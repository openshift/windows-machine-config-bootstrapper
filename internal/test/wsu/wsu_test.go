package wsu

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/internal/test"
	e2ef "github.com/openshift/windows-machine-config-bootstrapper/internal/test/framework"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/certificates/v1beta1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	// Path of the WSU playbook
	playbookPath = os.Getenv("WSU_PATH")
	// workerLabel is the worker label that needs to be applied to the Windows node
	workerLabel = "node-role.kubernetes.io/worker"
	// hybridOverlayMac is an annotation applied by the hybrid overlay
	hybridOverlayMac = "k8s.ovn.org/hybrid-overlay-distributed-router-gateway-mac"
	// windowsServerImage is the name/location of the Windows Server 2019 image we will use to test pod deployment
	windowsServerImage = "mcr.microsoft.com/windows/servercore:ltsc2019"
	// ubi8Image is the name/location of the linux image we will use for testing
	ubi8Image = "registry.access.redhat.com/ubi8/ubi:latest"
)

const hybridOverlayDir = "C:\\var\\log\\hybrid-overlay-node"
const kubeProxyDir = "C:\\var\\log\\kube-proxy"

type wsuFramework struct {
	// TestFramework holds the instantiation of test suite being executed
	*e2ef.TestFramework
	// Number of VMs which should use built version of WMCB
	// TODO Expose this option to the user along with vmCount -> https://issues.redhat.com/browse/WINC-240
	vmCountWithBuiltWMCB int
}

// Setup initializes the wsuFramework.
func (f *wsuFramework) Setup(vmCount int, credentials []*types.Credentials, skipVMsetup bool) error {
	f.TestFramework = &e2ef.TestFramework{}

	// If vmCount is 3 and vmCountWithBuiltWMCB is 2, 2 VMs will run WSU that will build WMCB and 1 VM will
	// auto-download the latest WMCB based on the cluster version
	f.vmCountWithBuiltWMCB = 1
	// vmCountWithBuiltWMCB should not be greater than vmCount
	if f.vmCountWithBuiltWMCB > vmCount {
		return fmt.Errorf("tried to run WSU against %d VMs but vmCount set to %d", f.vmCountWithBuiltWMCB, vmCount)
	}

	// Set up the framework
	err := f.TestFramework.Setup(vmCount, credentials, skipVMsetup)
	if err != nil {
		fmt.Errorf("framework setup failed: %v", err)
		return err
	}
	if err := f.GetLatestWMCBRelease(); err != nil {
		return fmt.Errorf("unable to get latest WMCB release: %v", err)
	}

	if err := f.GetClusterVersion(); err != nil {
		return fmt.Errorf("unable to get OpenShift cluster version: %v", err)
	}

	// Set 'buildWMCB' property of Windows VM
	// Not ideal to set a generic property in a specific implementation. This is a temporary workaround and will be
	// updated in https://issues.redhat.com/browse/WINC-249
	for i := 0; i < f.vmCountWithBuiltWMCB; i++ {
		f.WinVMs[i].SetBuildWMCB(true)
	}

	return nil
}

// createhostFile creates an ansible host file for the VMs we have spun up
func createHostFile(vmList []e2ef.TestWindowsVM) (string, error) {
	hostFile, err := ioutil.TempFile("", "testWSU")
	if err != nil {
		return "", fmt.Errorf("coud not make temporary file: %s", err)
	}
	defer hostFile.Close()

	// Give a loop back ip as internal ip, this would never show up as
	// private ip for any cloud provider. This is a dummy for testing purposes.
	// This is a hack to avoid changes to the Credentials struct or
	// making cloud provider API calls at this juncture and it would need to be fixed
	// if we ever want to add Azure e2e tests.
	loopbackIP := "127.0.0.1"

	// Add each host to the host file
	hostFileContents := "[win]\n"
	for i := 0; i < len(vmList); i++ {
		creds := vmList[i].GetCredentials()
		hostFileContents += creds.GetIPAddress() + " " + "ansible_password='" + creds.GetPassword() + "'" +
			" " + "private_ip='" + loopbackIP + "'" + "\n"
	}

	// Add the common variables
	hostFileContents += fmt.Sprintf(`[win:vars]
ansible_user=Administrator
cluster_address=%s
ansible_port=5986
ansible_connection=winrm
ansible_winrm_server_cert_validation=ignore
`, framework.ClusterAddress)
	_, err = hostFile.WriteString(hostFileContents)
	return hostFile.Name(), err
}

// TestWSU creates a Windows instance, runs the WSU, and then runs a series of tests to ensure all expected
// behavior was achieved. The following environment variables must be set for this test to run: KUBECONFIG,
// AWS_SHARED_CREDENTIALS_FILE, ARTIFACT_DIR, KUBE_SSH_KEY_PATH, WSU_PATH, CLUSTER_ADDR
func TestWSU(t *testing.T) {
	require.NotEmptyf(t, playbookPath, "WSU_PATH environment variable not set")

	t.Run("VM specific tests", testAllVMs)

	// Run cluster wide tests
	t.Run("Pending CSRs were approved", testNoPendingCSRs)

	t.Run("Tests across Windows nodes", testAcrossWindowsNodes)
}

// testAllVMs runs all VM specific tests
func testAllVMs(t *testing.T) {
	for i := range framework.WinVMs {
		t.Run("VM "+strconv.Itoa(i), func(t *testing.T) {
			runTests(t, framework.WinVMs[i])
		})
	}
}

// testAcrossWindowsNodes runs all the tests across Windows nodes
func testAcrossWindowsNodes(t *testing.T) {
	// Need at least two Windows VMs to run these tests, throwing error if this condition is not met
	require.GreaterOrEqualf(t, len(framework.WinVMs), 2, "Insufficient number of Windows VMs to run tests across"+
		" VMs, Minimum VM count: 2, Current VM count: %d", len(framework.WinVMs))

	// Selecting first 2 VMs from WinVms to run the tests
	firstVM := framework.WinVMs[0]
	secondVM := framework.WinVMs[1]

	t.Run("East-west networking across Windows nodes", func(t *testing.T) {
		testEastWestNetworkingAcrossWindowsNodes(t, firstVM, secondVM)
	})
}

// runTests runs all the tests required for a specific VM
func runTests(t *testing.T, vm e2ef.TestWindowsVM) {
	// Indicate that we can run the test suite on each node in parallel
	t.Parallel()

	runWSUAndTest(t, vm)
	//Run the test suite again, to ensure that the WSU can be run multiple times against the same VM
	t.Run("Run the WSU against the same VM again", func(t *testing.T) {
		runWSUAndTest(t, vm)
	})
}

// runWSUAndTest runs the WSU and runs tests against the setup node
func runWSUAndTest(t *testing.T, vm e2ef.TestWindowsVM) {
	// TODO: Think of a better way to refactor this function later to get just output that can be consumed by framework.
	// Run the WSU against the VM
	wsuOut, err := runWSU(vm)
	wsuStringOutput := string(wsuOut)
	require.NoError(t, err, "WSU playbook returned error: %s, with output: %s", err, wsuStringOutput)

	// Capture the WSU logs
	captureWSULogs(wsuOut, vm, t.Name()+"wsu.log")

	// Run our VM test suite
	runE2ETestSuite(t, vm, wsuStringOutput)
}

// captureWSULogs saves the WSU logs to the artifact directory
func captureWSULogs(wsuOut []byte, vm e2ef.TestWindowsVM, logFileName string) {
	externalIP := vm.GetCredentials().GetIPAddress()
	nodeName, err := framework.GetNodeName(externalIP)
	if err != nil {
		log.Printf("could not get the node name associated with the vm %s. "+
			"WSU logs for this vm will not be written to the artifact directory\n", vm.GetCredentials().GetInstanceId())
		return
	}
	localLogDirLocation := filepath.Join("nodes", nodeName, "logs")
	if err = framework.WriteToArtifactDir(wsuOut, localLogDirLocation, logFileName); err != nil {
		log.Printf("could not write %s to artifact dir: %s\n", logFileName, err)
	}
}

// runWSU runs the WSU playbook against a VM. Returns WSU stdout
func runWSU(vm e2ef.TestWindowsVM) ([]byte, error) {
	var ansibleCmd *exec.Cmd

	// In order to run the ansible playbook we create an inventory file:
	// https://docs.ansible.com/ansible/latest/user_guide/intro_inventory.html
	hostFilePath, err := createHostFile([]e2ef.TestWindowsVM{vm})
	if err != nil {
		return nil, err
	}

	// Run the WSU against the VM
	if vm.BuildWMCB() {
		// Build WMCB
		ansibleCmd = exec.Command("ansible-playbook", "-v", "-i", hostFilePath, playbookPath, "-e",
			"{build_wmcb: True}")
	} else {
		// Download latest released version of WMCB based on cluster version
		ansibleCmd = exec.Command("ansible-playbook", "-v", "-i", hostFilePath, playbookPath)
	}

	// Run the playbook
	wsuOut, err := ansibleCmd.CombinedOutput()
	return wsuOut, err
}

// getAnsibleTempDirPath returns the path of the ansible temp directory on the remote VM
func getAnsibleTempDirPath(ansibleOutput string) (string, error) {
	// Debug line looks like:
	// "msg": "Windows temporary directory: C:\\Users\\Administrator\\AppData\\Local\\Temp\\ansible.to4wamvh.yzk"
	debugStatementSplit := strings.Split(ansibleOutput, "Windows temporary directory: ")
	if len(debugStatementSplit) != 2 {
		return "", fmt.Errorf("expected one temporary directory debug statement, but found multiple: %s", ansibleOutput)
	}
	tempDirSplit := strings.Split(debugStatementSplit[1], "\"")
	if len(tempDirSplit) < 2 {
		return "", fmt.Errorf("unexpected split format %s", ansibleOutput)
	}

	return tempDirSplit[0], nil
}

// runE2ETestSuite runs the WSU test suite against a VM
func runE2ETestSuite(t *testing.T, vm e2ef.TestWindowsVM, ansibleOutput string) {
	tempDirPath, err := getAnsibleTempDirPath(ansibleOutput)
	require.NoError(t, err, "Could not get path of Ansible temp directory")

	binaryFileList := []string{"kubelet.exe", "worker.ign", "wmcb.exe", "hybrid-overlay-node.exe", "kube.tar.gz"}
	hybridOverlaylogList := []string{"hybridOverlayStdout.log", "hybridOverlayStderr.log"}
	kubeProxylogList := []string{"kube-proxy.exe.INFO", "kube-proxy.exe.WARNING"}

	node, err := framework.GetNode(vm.GetCredentials().GetIPAddress())
	require.NoError(t, err, "Could not get Windows node object")

	t.Run("Files copied to Windows node", func(t *testing.T) {
		testRemoteFilesExist(t, vm, binaryFileList, tempDirPath)
	})
	if vm.BuildWMCB() {
		t.Run("Check if wmcb was built", func(t *testing.T) {
			testBuiltWMCB(t, ansibleOutput)
		})
	} else {
		t.Run("Check if wmcb was downloaded", func(t *testing.T) {
			testDownloadedWMCB(t, ansibleOutput)
		})
	}
	t.Run("Node is in ready state", func(t *testing.T) {
		testNodeReady(t, node)
	})
	t.Run("check if hybrid-overlay and kube-proxy log exists", func(t *testing.T) {
		testRemoteFilesExist(t, vm, kubeProxylogList, kubeProxyDir)
		testRemoteFilesExist(t, vm, hybridOverlaylogList, hybridOverlayDir)
	})
	t.Run("Check if worker label has been applied to the Windows node", func(t *testing.T) {
		testWorkerLabelsArePresent(t, node)
	})
	t.Run("Network annotations were applied to node", func(t *testing.T) {
		testHybridOverlayAnnotations(t, node)
	})
	t.Run("HNS Networks were created", func(t *testing.T) {
		testHNSNetworksCreated(t, vm)
	})
	t.Run("Check cni config generated on the Windows host", func(t *testing.T) {
		testCNIConfig(t, node, vm, tempDirPath)
	})
	t.Run("Kubelet is running with the latest 0.8.x version of CNI Plugins", func(t *testing.T) {
		testCNIPluginsVersion(t, vm)
	})
	t.Run("East-west networking", func(t *testing.T) {
		testEastWestNetworking(t, node, vm)
	})
	t.Run("North-south networking", func(t *testing.T) {
		testNorthSouthNetworking(t, node, vm)
	})
}

// testDownloadedWMCB checks if the task 'Download WMCB' in the Ansible output was executed, not skipped
func testDownloadedWMCB(t *testing.T, ansibleOutput string) {
	// split the output for each task
	tasks := strings.Split(ansibleOutput, "TASK")
	// grab the output for the task "Download WMCB"
	downloadWmcbTaskOutput := ""
	for i := 0; i < len(tasks); i++ {
		if strings.Contains(tasks[i], "Download WMCB") {
			downloadWmcbTaskOutput = tasks[i]
		}
	}
	// check if the outcome was changed i.e. the task was executed ("changed": true)
	require.Contains(t, downloadWmcbTaskOutput, "\"changed\": true")
}

// testBuiltWMCB checks the Ansible output to ensure WMCB was built
func testBuiltWMCB(t *testing.T, ansibleOutput string) {
	require.Contains(t, ansibleOutput,
		"CGO_ENABLED=0 GO111MODULE=on GOOS=windows go build -o wmcb.exe  "+
			"github.com/openshift/windows-machine-config-bootstrapper/cmd/bootstrapper")
}

// testCNIConfig tests if the CNI config has required hostsubnet and servicenetwork CIDR
// NOTE: split this into multiple tests when this grows
func testCNIConfig(t *testing.T, node *v1.Node, vm e2ef.TestWindowsVM, ansibleTempDir string) {
	// Read the CNI config present on the Windows host
	cniConfigFilePath := filepath.Join(ansibleTempDir, "cni", "config", "cni.conf")
	cniConfigFileContents, err := readRemoteFile(cniConfigFilePath, vm)
	require.NoError(t, err, "Could not get CNI config contents")

	// By the time, we reach here the annotation should be present, so need to validate again
	hostSubnet := node.Annotations[test.HybridOverlaySubnet]
	// Check if the host subnet matches our expected value
	assert.Contains(t, cniConfigFileContents, hostSubnet, "CNI config does not contain host subnet")

	// Check if the service CIDR matches our expected value
	networkCR, err := framework.OSConfigClient.ConfigV1().Networks().Get(context.TODO(), "cluster", metav1.GetOptions{})
	require.NoError(t, err, "Error querying network object")
	serviceNetworks := networkCR.Spec.ServiceNetwork
	// The serviceNetwork should be a singleton slice as of now, let's try accessing the first element in it.
	require.Equalf(t, len(serviceNetworks), 1, "Expected service network to be a singleton but got %v",
		len(serviceNetworks))
	requiredServiceNetwork := serviceNetworks[0]
	assert.Contains(t, cniConfigFileContents, requiredServiceNetwork, "CNI config does not contain service network")
}

// testCNIPluginsVersion tests if the kubelet on the Windows VM is running with latest 0.8.x version of CNI Plugins
func testCNIPluginsVersion(t *testing.T, vm e2ef.TestWindowsVM) {
	// Fetching kubelet service image path which includes the  directory path of the CNI Plugin binaries
	// Example kubelet service image path: c:\k\kubelet.exe --windows-service --bootstrap-kubeconfig=c:\k\bootstrap-kubeconfig
	// --kubeconfig=c:\k\kubeconfig --network-plugin=cni --cni-bin-dir=c:\k\cni --log-file=c:\k\log\kubelet.log
	// --node-labels=node.openshift.io/os_id=Windows --cloud-provider=aws --resolv-conf="" --config=c:\k\kubelet.conf
	// --pod-infra-container-image=mcr.microsoft.com/k8s/core/pause:1.2.0 --logtostderr=false --v=3
	// --cert-dir=c:\var\lib\kubelet\pki\ --register-with-taints=os=Windows:NoSchedule --cni-conf-dir=c:\k\cni\config
	kubeletImagePath, _, err := vm.Run("$service=get-wmiobject -query \\\"select * from win32_service "+
		"where name='kubelet'\\\"; echo $service.pathname", true)
	require.NoError(t, err, "Could not fetch image path of the kubelet service")
	// Extracting directory path of the CNI Plugin binaries from kubelet service image path
	var cniBinaryDir string
	kubeletArgs := strings.Split(kubeletImagePath, " ")
	for _, kubeletArg := range kubeletArgs {
		kubeletArgOptionAndValue := strings.Split(strings.TrimSpace(kubeletArg), "=")
		// if kubelet arg is of the form --<option>=<value> and option='--network-plugin' does not have value='cni' then
		// we throw error since CNI Plugins are not enabled
		if len(kubeletArgOptionAndValue) == 2 && kubeletArgOptionAndValue[0] == "--network-plugin" &&
			kubeletArgOptionAndValue[1] != "cni" {
			err := fmt.Errorf("kubelet arg '--network-plugin' should have value 'cni', current value is '%s'",
				kubeletArgOptionAndValue[1])
			require.NoError(t, err, "CNI Plugins are not enabled")
		}
		// if kubelet arg is of the form --<option>=<value> and option='--cni-bin-dir' then its value will be the required
		// directory path of the CNI Plugin binaries
		if len(kubeletArgOptionAndValue) == 2 && kubeletArgOptionAndValue[0] == "--cni-bin-dir" {
			cniBinaryDir = kubeletArgOptionAndValue[1]
		}
	}
	// Throwing error if directory of CNI Plugin binaries could not be fetched from kubelet service image path
	require.NotEmpty(t, cniBinaryDir, "Could not fetch directory path of CNI Plugin binaries")
	// Fetching names of CNI Plugins as a comma separated string
	// Example: flannel,host-local,win-bridge,win-overlay
	pluginNamesCommaSeparated, _, err := vm.Run("(gci -FILE "+cniBinaryDir+").basename -join ','", true)
	require.NoError(t, err, "Could not fetch comma separated names of CNI Plugins from directory %s", cniBinaryDir)
	pluginNames := strings.Split(pluginNamesCommaSeparated, ",")
	// Executing each CNI Plugin to check if it is of latest 0.8.x version
	for _, pluginName := range pluginNames {
		// Example stderr after executing plugin executable: CNI win-overlay plugin v0.8.2
		_, pluginExecutionStderr, err := vm.Run(cniBinaryDir+"\\"+pluginName, true)
		require.NoError(t, err, "Could not execute CNI Plugin "+pluginName)
		assert.Contains(t, pluginExecutionStderr, framework.LatestCniPluginsVersion, "CNI Plugin "+
			pluginName+" is not of latest 0.8.x version")
	}
}

// testRemoteFilesExist tests that the files we expect, exist on the Windows host
func testRemoteFilesExist(t *testing.T, vm e2ef.TestWindowsVM, expectedFileList []string, directoryPath string) {
	// Check if each of the files we expect on the Windows host are there
	for _, filename := range expectedFileList {
		fullPath := directoryPath + "\\" + filename
		// This command will write to stdout, only if the file we are looking for does not exist
		command := fmt.Sprintf("if not exist %s echo fail", fullPath)
		stdout, _, err := vm.Run(command, false)
		assert.NoError(t, err, "Error looking for %s: %s", fullPath, err)
		assert.Emptyf(t, stdout, "Missing file: %s", fullPath)
	}
}

// testNodeReady tests that the bootstrapped node was added to the cluster and is in the ready state
func testNodeReady(t *testing.T, createdNode *v1.Node) {
	// Make sure the node is in a ready state
	foundReady := false
	for _, condition := range createdNode.Status.Conditions {
		if condition.Type != v1.NodeReady {
			continue
		}
		foundReady = true
		assert.Equalf(t, v1.ConditionTrue, condition.Status, "Node not in ready state: %s", condition.Reason)
		break
	}
	// Just in case node is missing the ready condition, for whatever reason
	assert.True(t, foundReady, "Node did not have ready condition")
}

// testNoPendingCSRs tests that there are no pending CSRs on the cluster
func testNoPendingCSRs(t *testing.T) {
	csrs, err := framework.K8sclientset.CertificatesV1beta1().CertificateSigningRequests().List(context.TODO(),
		metav1.ListOptions{})
	assert.NoError(t, err, "could not get CSR list")
	for _, csr := range csrs.Items {
		// CSR's with an empty condition list are pending
		assert.NotEmptyf(t, csr.Status.Conditions, "csr %v is pending", csr)
		// If not pending, make sure the CSR is approved
		for _, condition := range csr.Status.Conditions {
			assert.Equalf(t, v1beta1.CertificateApproved, condition.Type, "csr %v has non-approved condition", csr)
		}
	}
}

// testWorkerLabelsArePresent tests if the worker labels are present on the Windows Node.
func testWorkerLabelsArePresent(t *testing.T, node *v1.Node) {
	assert.Contains(t, node.Labels, workerLabel,
		"expected worker label to be present on the Windows node but did not find any")
}

// readRemoteFile returns the contents of a remote file. Returns an error on winRM failure, or if it does not exist.
func readRemoteFile(fileName string, vm e2ef.TestWindowsVM) (string, error) {
	stdout, _, err := vm.Run("cat "+fileName, true)
	if err != nil {
		return "", fmt.Errorf("WinRM failure trying to run cat: %s", err)
	}
	return stdout, nil
}

// testHybridOverlayAnnotations tests that the correct annotations have been added to the bootstrapped node
func testHybridOverlayAnnotations(t *testing.T, node *v1.Node) {
	assert.Contains(t, node.Annotations, test.HybridOverlaySubnet)
	assert.Contains(t, node.Annotations, hybridOverlayMac)
}

// testHNSNetworksCreated tests that the required HNS Networks have been created on the bootstrapped node
func testHNSNetworksCreated(t *testing.T, vm e2ef.TestWindowsVM) {
	stdout, _, err := vm.Run("Get-HnsNetwork", true)
	require.NoError(t, err, "Could not run Get-HnsNetwork command")
	assert.Contains(t, stdout, "Name                   : BaseOVNKubernetesHybridOverlayNetwork",
		"Could not find BaseOVNKubernetesHybridOverlayNetwork in list of HNS Networks")
	assert.Contains(t, stdout, "Name                   : OVNKubernetesHybridOverlayNetwork",
		"Could not find OVNKubernetesHybridOverlayNetwork in list of HNS Networks")
}

// getAffinityForNode returns an affinity which matches the associated node's name
func getAffinityForNode(node *v1.Node) (*v1.Affinity, error) {
	return &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchFields: []v1.NodeSelectorRequirement{
							{
								Key:      "metadata.name",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{node.Name},
							},
						},
					},
				},
			},
		},
	}, nil
}

// getPodEvents gets all events for any pod with the input in its name. Used for debugging purposes
func getPodEvents(name string) ([]v1.Event, error) {
	eventList, err := framework.K8sclientset.CoreV1().Events(v1.NamespaceDefault).List(context.TODO(),
		metav1.ListOptions{FieldSelector: "involvedObject.kind=Pod"})
	if err != nil {
		return []v1.Event{}, err
	}
	podEvents := []v1.Event{}
	for _, event := range eventList.Items {
		if strings.Contains(event.InvolvedObject.Name, name) {
			podEvents = append(podEvents, event)
		}
	}
	return podEvents, nil

}

// testEastWestNetworking deploys Windows and Linux pods, and tests that the pods can communicate
func testEastWestNetworking(t *testing.T, node *v1.Node, vm e2ef.TestWindowsVM) {
	affinity, err := getAffinityForNode(node)
	require.NoError(t, err, "Could not get affinity for node")

	// Deploy a webserver pod on the new node
	winServerDeployment, err := deployWindowsWebServer("win-webserver-"+vm.GetCredentials().GetInstanceId(), vm, affinity)
	require.NoError(t, err, "Could not create Windows Server deployment")
	defer deleteDeployment(winServerDeployment.Name)

	// Get the pod so we can use its IP
	winServerIP, err := getPodIP(*winServerDeployment.Spec.Selector)
	require.NoError(t, err, "Could not retrieve pod with selector %v", *winServerDeployment.Spec.Selector)

	// test Windows <-> Linux
	// This will install curl and then curl the windows server.
	linuxCurlerCommand := []string{"bash", "-c", "yum update; yum install curl -y; curl " + winServerIP}
	linuxCurlerJob, err := createLinuxJob("linux-curler-"+vm.GetCredentials().GetInstanceId(), linuxCurlerCommand)
	require.NoError(t, err, "Could not create Linux job")
	defer deleteJob(linuxCurlerJob.Name)
	err = waitUntilJobSucceeds(linuxCurlerJob.Name)
	assert.NoError(t, err, "Could not curl the Windows server from a linux container")

	// test Windows <-> Windows on same node
	winCurlerJob, err := createWinCurlerJob(vm, winServerIP)
	require.NoError(t, err, "Could not create Windows job")
	defer deleteJob(winCurlerJob.Name)
	err = waitUntilJobSucceeds(winCurlerJob.Name)
	assert.NoError(t, err, "Could not curl the Windows webserver pod from a separate Windows container")
}

//  testEastWestNetworkingAcrossWindowsNodes deploys Windows pods on two different Nodes, and tests that the pods can communicate
func testEastWestNetworkingAcrossWindowsNodes(t *testing.T, firstVM e2ef.TestWindowsVM, secondVM e2ef.TestWindowsVM) {
	firstNode, err := framework.GetNode(firstVM.GetCredentials().GetIPAddress())
	require.NoError(t, err, "Could not get Windows node object from first VM")

	affinityForFirstNode, err := getAffinityForNode(firstNode)
	require.NoError(t, err, "Could not get affinity for first node")

	// Deploy a webserver pod on the first node
	winServerDeploymentOnFirstNode, err := deployWindowsWebServer("win-webserver-"+firstVM.GetCredentials().GetInstanceId(),
		firstVM, affinityForFirstNode)
	require.NoError(t, err, "Could not create Windows Server deployment on first Node")
	defer deleteDeployment(winServerDeploymentOnFirstNode.Name)

	// Get the pod so we can use its IP
	winServerIP, err := getPodIP(*winServerDeploymentOnFirstNode.Spec.Selector)
	require.NoError(t, err, "Could not retrieve pod with selector %v", *winServerDeploymentOnFirstNode.Spec.Selector)

	// test Windows <-> Windows across nodes
	winCurlerJobOnSecondNode, err := createWinCurlerJob(secondVM, winServerIP)
	require.NoError(t, err, "Could not create Windows job on second Node")
	defer deleteJob(winCurlerJobOnSecondNode.Name)
	err = waitUntilJobSucceeds(winCurlerJobOnSecondNode.Name)
	assert.NoError(t, err, "Could not curl the Windows webserver pod on the first node from Windows container "+
		"on the second node")
}

//  createWinCurlerJob creates a Job to curl Windows server at given IP address
func createWinCurlerJob(vm e2ef.TestWindowsVM, winServerIP string) (*batchv1.Job, error) {
	winCurlerCommand := getWinCurlerCommand(winServerIP)
	winCurlerJob, err := createWindowsServerJob("win-curler-"+vm.GetCredentials().GetInstanceId(), winCurlerCommand)
	return winCurlerJob, err
}

// getWinCurlerCommand generates a command to curl a Windows server from the given IP address
func getWinCurlerCommand(winServerIP string) []string {
	// This will continually try to read from the Windows Server. We have to try multiple times as the Windows container
	// takes some time to finish initial network setup.
	winCurlerCommand := []string{"powershell.exe", "-command", "for (($i =0), ($j = 0); $i -lt 10; $i++) { " +
		"$response = Invoke-Webrequest -UseBasicParsing -Uri " + winServerIP +
		"; $code = $response.StatusCode; echo \"GET returned code $code\";" +
		"If ($code -eq 200) {exit 0}; Start-Sleep -s 10;}; exit 1"}
	return winCurlerCommand
}

// deployWindowsWebServer creates a deployment with a single Windows Server pod, listening on port 80
func deployWindowsWebServer(name string, vm e2ef.TestWindowsVM, affinity *v1.Affinity) (*appsv1.Deployment, error) {
	// Preload the image that will be used on the Windows node, to prevent download timeouts
	// and separate possible failure conditions into multiple operations
	err := pullDockerImage(windowsServerImage, vm)
	if err != nil {
		return nil, fmt.Errorf("could not pull Windows Server image: %s", err)
	}
	// This will run a Server on the container, which can be reached with a GET request
	winServerCommand := []string{"powershell.exe", "-command",
		"$listener = New-Object System.Net.HttpListener; $listener.Prefixes.Add('http://*:80/'); $listener.Start(); " +
			"Write-Host('Listening at http://*:80/'); while ($listener.IsListening) { " +
			"$context = $listener.GetContext(); $response = $context.Response; " +
			"$content='<html><body><H1>Windows Container Web Server</H1></body></html>'; " +
			"$buffer = [System.Text.Encoding]::UTF8.GetBytes($content); $response.ContentLength64 = $buffer.Length; " +
			"$response.OutputStream.Write($buffer, 0, $buffer.Length); $response.Close(); };"}
	winServerDeployment, err := createWindowsServerDeployment(name, winServerCommand, affinity)
	if err != nil {
		return nil, fmt.Errorf("could not create Windows deployment: %s", err)
	}
	// Wait until the server is ready to be queried
	err = waitUntilDeploymentScaled(winServerDeployment.Name)
	if err != nil {
		deleteDeployment(winServerDeployment.Name)
		return nil, fmt.Errorf("deployment was unable to scale: %s", err)
	}
	return winServerDeployment, nil
}

// waitUntilJobSucceeds will return an error if the job fails or reaches a timeout
func waitUntilJobSucceeds(name string) error {
	var job *batchv1.Job
	var err error
	for i := 0; i < e2ef.RetryCount; i++ {
		job, err = framework.K8sclientset.BatchV1().Jobs(v1.NamespaceDefault).Get(context.TODO(), name,
			metav1.GetOptions{})
		if err != nil {
			return err
		}
		if job.Status.Succeeded > 0 {
			return nil
		}
		if job.Status.Failed > 0 {
			events, _ := getPodEvents(name)
			return fmt.Errorf("job %v failed: %v", job, events)
		}
		time.Sleep(e2ef.RetryInterval)
	}
	events, _ := getPodEvents(name)
	return fmt.Errorf("job %v timed out: %v", job, events)
}

// waitUntilDeploymentScaled will return nil if the deployment reaches the amount of replicas specified in its spec
func waitUntilDeploymentScaled(name string) error {
	var deployment *appsv1.Deployment
	var err error
	for i := 0; i < e2ef.RetryCount; i++ {
		deployment, err = framework.K8sclientset.AppsV1().Deployments(v1.NamespaceDefault).Get(context.TODO(), name,
			metav1.GetOptions{})
		if err != nil {
			return err
		}
		if *deployment.Spec.Replicas == deployment.Status.AvailableReplicas {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}
	events, _ := getPodEvents(name)
	return fmt.Errorf("timed out waiting for deployment %v to scale: %v", deployment, events)
}

// getPodIP returns the IP of the pod that matches the label selector. If more than one pod match the
// selector, the function will return an error
func getPodIP(selector metav1.LabelSelector) (string, error) {
	selectorString := labels.Set(selector.MatchLabels).String()
	podList, err := framework.K8sclientset.CoreV1().Pods(v1.NamespaceDefault).List(context.TODO(),
		metav1.ListOptions{LabelSelector: selectorString})
	if err != nil {
		return "", err
	}
	if len(podList.Items) != 1 {
		return "", fmt.Errorf("expected one pod matching %s, but found %d", selectorString, len(podList.Items))
	}

	return podList.Items[0].Status.PodIP, nil
}

// createWindowsServerJob creates a job which will run the provided command with a Windows Server image
func createWindowsServerJob(name string, command []string) (*batchv1.Job, error) {
	windowsNodeSelector := map[string]string{"beta.kubernetes.io/os": "windows"}
	windowsTolerations := []v1.Toleration{{Key: "os", Value: "Windows", Effect: v1.TaintEffectNoSchedule}}
	return createJob(name, windowsServerImage, command, windowsNodeSelector, windowsTolerations)
}

// createLinuxJob creates a job which will run the provided command with a ubi8 image
func createLinuxJob(name string, command []string) (*batchv1.Job, error) {
	linuxNodeSelector := map[string]string{"beta.kubernetes.io/os": "linux"}
	return createJob(name, ubi8Image, command, linuxNodeSelector, []v1.Toleration{})
}

func createJob(name, image string, command []string, selector map[string]string,
	tolerations []v1.Toleration) (*batchv1.Job, error) {
	jobsClient := framework.K8sclientset.BatchV1().Jobs(v1.NamespaceDefault)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "-job",
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Tolerations:   tolerations,
					Containers: []v1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: v1.PullIfNotPresent,
							Command:         command,
						},
					},
					NodeSelector: selector,
				},
			},
		},
	}

	// Create job
	job, err := jobsClient.Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return job, err
}

// deleteJob deletes the job with the given name
func deleteJob(name string) error {
	jobsClient := framework.K8sclientset.BatchV1().Jobs(v1.NamespaceDefault)
	return jobsClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// createWindowsServerDeployment creates a deployment with a Windows Server 2019 container
func createWindowsServerDeployment(name string, command []string, affinity *v1.Affinity) (*appsv1.Deployment, error) {
	deploymentsClient := framework.K8sclientset.AppsV1().Deployments(v1.NamespaceDefault)
	replicaCount := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: v1.PodSpec{
					Affinity: affinity,
					Tolerations: []v1.Toleration{
						{
							Key:    "os",
							Value:  "Windows",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
					Containers: []v1.Container{
						// Windows web server
						{
							Name:            name,
							Image:           windowsServerImage,
							ImagePullPolicy: v1.PullIfNotPresent,
							Command:         command,
							Ports: []v1.ContainerPort{
								{
									Protocol:      v1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
					NodeSelector: map[string]string{"beta.kubernetes.io/os": "windows"},
				},
			},
		},
	}

	// Create Deployment
	deploy, err := deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return deploy, err
}

// deleteDeployment deletes the deployment with the given name
func deleteDeployment(name string) error {
	deploymentsClient := framework.K8sclientset.AppsV1().Deployments(v1.NamespaceDefault)
	return deploymentsClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// pullDockerImage pulls the designated image on the remote host
func pullDockerImage(name string, vm e2ef.TestWindowsVM) error {
	command := "docker pull " + name
	_, _, err := vm.Run(command, false)
	if err != nil {
		return fmt.Errorf("failed to remotely run docker pull: %s", err)
	}
	return nil
}

// testNorthSouthNetworking deploys a Windows Server pod, and tests that we can network with it from outside the cluster
func testNorthSouthNetworking(t *testing.T, node *v1.Node, vm e2ef.TestWindowsVM) {
	affinity, err := getAffinityForNode(node)
	require.NoError(t, err, "Could not get affinity for node")

	// Deploy a webserver pod on the new node
	winServerDeployment, err := deployWindowsWebServer("win-webserver-"+vm.GetCredentials().GetInstanceId(), vm, affinity)
	require.NoError(t, err, "Could not create Windows Server deployment")
	defer deleteDeployment(winServerDeployment.Name)

	// Create a load balancer svc to expose the webserver
	loadBalancer, err := createLoadBalancer("win-webserver-"+vm.GetCredentials().GetInstanceId(), *winServerDeployment.Spec.Selector)
	require.NoError(t, err, "Could not create load balancer for Windows Server")
	defer deleteService(loadBalancer.Name)
	loadBalancer, err = waitForLoadBalancerIngress(loadBalancer.Name)
	require.NoError(t, err, "Error waiting for load balancer ingress")

	// Try and read from the webserver through the load balancer. The load balancer takes a fair amount of time, ~3 min,
	// to start properly routing connections.
	resp, err := retryGET("http://"+loadBalancer.Status.LoadBalancer.Ingress[0].Hostname, e2ef.RetryInterval*3)
	require.NoError(t, err, "Could not GET from load balancer: %v", loadBalancer)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Non 200 response from webserver")
}

// retryGET will repeatedly try to GET from the provided URL
func retryGET(url string, retryInterval time.Duration) (*http.Response, error) {
	var resp *http.Response
	var err error
	for i := 0; i < e2ef.RetryCount; i++ {
		resp, err = http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		time.Sleep(retryInterval)
	}
	return resp, fmt.Errorf("timed out trying to GET %s: %s", url, err)
}

// createLoadBalancer creates a new load balancer for pods matching the label selector
func createLoadBalancer(name string, selector metav1.LabelSelector) (*v1.Service, error) {
	svcSpec := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Protocol: v1.ProtocolTCP,
					Port:     80,
				},
			},
			Selector: selector.MatchLabels,
		}}
	return framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault).Create(context.TODO(), svcSpec,
		metav1.CreateOptions{})
}

// waitForLoadBalancerIngress waits until the load balancer has an external hostname ready
func waitForLoadBalancerIngress(name string) (*v1.Service, error) {
	var svc *v1.Service
	var err error
	for i := 0; i < e2ef.RetryCount; i++ {
		svc, err = framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault).Get(context.TODO(), name,
			metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if len(svc.Status.LoadBalancer.Ingress) == 1 {
			return svc, nil
		}
		time.Sleep(e2ef.RetryInterval)
	}
	return nil, fmt.Errorf("timed out waiting for single ingress: %v", svc)
}

// deleteService deletes the service with the given name
func deleteService(name string) error {
	svcClient := framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault)
	return svcClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
}
