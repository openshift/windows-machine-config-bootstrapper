package wmcb

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/windows-machine-config-bootstrapper/internal/test"
	e2ef "github.com/openshift/windows-machine-config-bootstrapper/internal/test/framework"
)

const (
	// payloadDirectory is the directory in the operator image where are all the binaries live
	payloadDirectory = "/payload/"
	// cniDirectory is the directory for storing the CNI plugins
	cniDirectory = payloadDirectory + "/cni/"
	// remoteDir is the remote temporary directory that the e2e test uses
	remoteDir = "C:\\Temp\\"
	// winTemp is the default Windows temporary directory
	winTemp = "C:\\Windows\\Temp\\"
	// winCNIDir is the directory where the CNI files are placed
	winCNIDir = winTemp + "\\cni\\"
	// winCNIConfigPath is the CNI configuration file path on the Windows VM
	winCNIConfigPath = "C:\\Windows\\Temp\\cni\\config\\"
	// logDir is the remote kubernetes log director
	kLog = "C:\\k\\log\\"
	// cniConfigTemplate is the location of the cni.conf template file
	cniConfigTemplate = "templates/cni.template"
	// wgetIgnoreCertCmd is the remote location of the wget-ignore-cert.ps1 script
	wgetIgnoreCertCmd = remoteDir + "wget-ignore-cert.ps1"
	// e2eExecutable is the remote location of the WMCB e2e test binary
	e2eExecutable = remoteDir + "wmcb_e2e_test.exe"
	// unitExecutable is the remote location of the WMCB unit test binary
	unitExecutable = remoteDir + "wmcb_unit_test.exe"
	// hybridOverlayName is the name of the hybrid overlay executable
	hybridOverlayName = "hybrid-overlay-node.exe"
	// hybridOverExecutable is the remote location of the hybrid overlay binary
	hybridOverlayExecutable = remoteDir + hybridOverlayName
	// hybridOverlayConfigurationTime is the approximate time taken for the hybrid-overlay to complete reconfiguring
	// the Windows VM's network
	hybridOverlayConfigurationTime = 2 * time.Minute
)

var (
	// windowsTaint is the taint that needs to be applied to the Windows node
	windowsTaint = v1.Taint{
		Key:    "os",
		Value:  "Windows",
		Effect: v1.TaintEffectNoSchedule,
	}
)

// wmcbVM is a wrapper for the WindowsVM interface that associates it with WMCB specific testing
type wmcbVM struct {
	e2ef.TestWindowsVM
}

type wmcbFramework struct {
	// TestFramework holds the instantiation of test suite being executed
	*e2ef.TestFramework
}

// Setup initializes the wsuFramework.
func (f *wmcbFramework) Setup(vmCount int, skipVMSetup bool) error {
	f.TestFramework = &e2ef.TestFramework{}
	// Set up the framework
	err := f.TestFramework.Setup(vmCount, skipVMSetup)
	if err != nil {
		return fmt.Errorf("framework setup failed: %v", err)
	}
	return nil
}

// TestWMCB runs the unit and e2e tests for WMCB on the remote VMs
func TestWMCB(t *testing.T) {
	srcDestPairs := map[string]string{
		payloadDirectory: remoteDir,
		cniDirectory:     winCNIDir,
	}

	for _, vm := range framework.WinVMs {
		log.Printf("Testing VM: %s", vm.GetCredentials().InstanceId())
		wVM := &wmcbVM{vm}
		for src, dest := range srcDestPairs {
			err := wVM.CopyDirectory(src, dest)
			require.NoError(t, err, "error copying %s to the Windows VM", src)
		}
		t.Run("Unit", func(t *testing.T) {
			assert.NoError(t, wVM.runTest(unitExecutable+" --test.v"), "WMCB unit test failed")
		})
		t.Run("E2E", func(t *testing.T) {
			wVM.runE2ETestSuite(t)
		})
		t.Run("WMCB cluster tests", testWMCBCluster)
	}
}

// runE2ETestSuite runs the WmCB e2e tests suite on the VM
func (vm *wmcbVM) runE2ETestSuite(t *testing.T) {
	vm.runTestBootstrapper(t)

	vm.runTestConfigureCNI(t)

	// Run this test only after TestBoostrapper() to ensure kubelet service is present.
	vm.runTestKubeletUninstall(t)
}

// runTest runs the testCmd in the given VM
func (vm *wmcbVM) runTest(testCmd string) error {
	output, err := vm.Run(testCmd, true)

	// Logging the output so that it is visible on the CI page
	log.Printf("\n%s\n", output)

	if err != nil {
		return fmt.Errorf("error running test: %v: %s", err, output)
	}
	if strings.Contains(output, "FAIL") {
		return fmt.Errorf("test output showed a failure")
	}
	if strings.Contains(output, "panic") {
		return fmt.Errorf("test output showed panic")
	}
	return nil
}

// runTestBootstrapper runs the initialize-kubelet tests
func (vm *wmcbVM) runTestBootstrapper(t *testing.T) {
	err := vm.initializeTestBootstrapperFiles()
	require.NoError(t, err, "error initializing files required for TestBootstrapper")

	err = vm.runTest(e2eExecutable + " --test.run TestBootstrapper --test.v")
	require.NoError(t, err, "TestBootstrapper failed")
}

// runTestConfigureCNI performs the required setup and runs the configure-cni tests
func (vm *wmcbVM) runTestConfigureCNI(t *testing.T) {
	node, err := framework.GetNode(vm.GetCredentials().IPAddress())
	require.NoError(t, err, "unable to get node object for VM")

	err = vm.handleHybridOverlay(node.GetName())
	require.NoError(t, err, "unable to handle hybrid-overlay")

	// It is guaranteed that the hybrid overlay annotations are present as we have already checked for it
	hybridOverlayAnnotation := node.GetAnnotations()[test.HybridOverlaySubnet]
	err = vm.initializeTestConfigureCNIFiles(hybridOverlayAnnotation)
	require.NoError(t, err, "error initializing files required for TestConfigureCNI")

	err = vm.runTest(e2eExecutable + " --test.run TestConfigureCNI --test.v")
	require.NoError(t, err, "TestConfigureCNI failed")
}

// initializeTestBootstrapperFiles initializes the files required for initialize-kubelet
func (vm *wmcbVM) initializeTestBootstrapperFiles() error {
	// Create the temp directory
	_, err := vm.Run(mkdirCmd(remoteDir), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v", remoteDir, err)
	}

	// Copy kubelet.exe to C:\Windows\Temp\
	_, err = vm.Run("cp "+remoteDir+"kubelet.exe "+winTemp, true)
	if err != nil {
		return fmt.Errorf("unable to copy kubelet.exe to %s: %v", winTemp, err)
	}

	// Ignition v2.3.0 maps to Ignition config spec v3.1.0.
	ignitionAcceptHeaderSpec := "application/vnd.coreos.ignition+json`;version=3.1.0"
	// Download the worker ignition to C:\Windows\Tenp\ using the script that ignores the server cert
	output, err := vm.Run(wgetIgnoreCertCmd+" -server https://"+framework.ClusterAddress+":22623/config/worker"+
		" -output "+winTemp+"worker.ign"+" -acceptHeader "+ignitionAcceptHeaderSpec, true)
	if err != nil {
		return fmt.Errorf("unable to download worker.ign: %v\nOutput: %s", err, output)
	}

	return nil
}

// initializeTestConfigureCNIFiles initializes the files required for configure-cni
func (vm *wmcbVM) initializeTestConfigureCNIFiles(ovnHostSubnet string) error {
	// Create the CNI directory C:\Windows\Temp\cni on the Windows VM
	output, err := vm.Run(mkdirCmd(winCNIDir), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v\n%s", remoteDir, err, output)
	}

	// Create the CNI config file locally
	cniConfigPath, err := createCNIConf(ovnHostSubnet)
	if err != nil {
		return fmt.Errorf("error creating local cni.conf: %v", err)
	}

	// Copy the created config to C:\Window\Temp\cni\config\cni.conf on the Windows VM
	err = vm.CopyFile(cniConfigPath, winCNIConfigPath)
	if err != nil {
		return fmt.Errorf("error copying %s --> VM %s: %v", cniConfigPath, winCNIConfigPath, err)
	}
	return nil
}

// handleHybridOverlay ensures that the hybrid overlay is running on the node
func (vm *wmcbVM) handleHybridOverlay(nodeName string) error {
	// Check if the hybrid-overlay-node is running
	output, err := vm.Run("Get-Process -Name \"hybrid-overlay-node\"", true)

	// stderr being empty implies that an hybrid-overlay-node was running. This is to help with local development.
	if err == nil || output == "" {
		return nil
	}

	// Wait until the node object has the hybrid overlay subnet annotation. Otherwise the hybrid-overlay-node will fail to
	// start
	if err = waitForHybridOverlayAnnotation(nodeName); err != nil {
		return fmt.Errorf("error waiting for hybrid overlay node annotation: %v", err)
	}

	output, err = vm.Run(mkdirCmd(kLog), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v\n%s", kLog, err, output)
	}

	// Start the hybrid-overlay-node in the background over ssh.
	go vm.Run(hybridOverlayExecutable+" --node "+nodeName+
		" --k8s-kubeconfig c:\\k\\kubeconfig > "+kLog+"hybrid-overlay.log 2>&1", false)

	err = vm.waitForHybridOverlayToRun()
	if err != nil {
		return fmt.Errorf("error running %s: %v", hybridOverlayName, err)
	}

	// Wait for the hybrid-overlay to complete reconfiguring the network. The only way to detect that it has completed
	// the reconfiguration is to check for the HNS networks but doing that results in 5+ minutes wait times for the
	// vm.Run() call to complete. So the only alternative is to wait before proceeding.
	time.Sleep(hybridOverlayConfigurationTime)

	// Running the hybrid-overlay causes network reconfiguration in the Windows VM which results in the ssh connection
	// being closed and the client is not smart enough to reconnect.
	if err = vm.Reinitialize(); err != nil {
		return errors.Wrap(err, "error reinitializing VM after running hybrid-overlay")
	}

	err = vm.waitForOpenShiftHNSNetworks()
	if err != nil {
		return fmt.Errorf("error waiting for OpenShift HNS networks to be created: %v", err)
	}

	return nil
}

// waitForOpenShiftHSNNetworks waits for the OpenShift HNS networks to be created until the timeout is reached
func (vm *wmcbVM) waitForOpenShiftHNSNetworks() error {
	var output string
	var err error
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		output, err = vm.Run("Get-HnsNetwork", true)
		if err != nil {
			// retry
			continue
		}

		if strings.Contains(output, "BaseOVNKubernetesHybridOverlayNetwork") &&
			strings.Contains(output, "OVNKubernetesHybridOverlayNetwork") {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}

	// OpenShift HNS networks were not found
	log.Printf("Get-HnsNetwork:\n%s", output)
	return fmt.Errorf("timeout waiting for OpenShift HNS networks: %v", err)
}

// waitForHybridOverlayToRun waits for the hybrid-overlay-node.exe to run until the timeout is reached
func (vm *wmcbVM) waitForHybridOverlayToRun() error {
	var err error
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		_, err = vm.Run("Get-Process -Name \"hybrid-overlay-node\"", true)
		if err == nil {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}

	// hybrid-overlay-node never started running
	return fmt.Errorf("timeout waiting for hybrid-overlay-node: %v", err)
}

func (vm *wmcbVM) runTestKubeletUninstall(t *testing.T) {
	err := vm.runTest(e2eExecutable + " --test.run TestKubeletUninstall --test.v")
	require.NoError(t, err, "TestKubeletUninstall failed")
}

// mkdirCmd returns the Windows command to create a directory if it does not exists
func mkdirCmd(dirName string) string {
	return "if not exist " + dirName + " mkdir " + dirName
}

// createCNIConf create the local cni.conf and returns its path
func createCNIConf(ovnHostSubnet string) (string, error) {
	serviceNetworkCIDR, err := getServiceNetworkCIDR()
	if err != nil {
		return "", fmt.Errorf("unable to get service network CIDR: %v", err)
	}

	cniConfigPath, err := generateCNIConf(ovnHostSubnet, serviceNetworkCIDR)
	if err != nil {
		return "", fmt.Errorf("unable to generate CNI configuration: %v", err)
	}

	return cniConfigPath, nil
}

// getServiceNetworkCIDR returns the service network CIDR from the cluster network object
func getServiceNetworkCIDR() (string, error) {
	// Get the cluster network object so that we can find the service network CIDR
	networkCR, err := framework.OSConfigClient.ConfigV1().Networks().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting cluster network object: %v", err)
	}

	if len(networkCR.Spec.ServiceNetwork) != 1 {
		return "", fmt.Errorf("expected one service network but got %d", len(networkCR.Spec.ServiceNetwork))
	}

	return networkCR.Spec.ServiceNetwork[0], nil
}

// generateCNIConf generates the cni.conf file, based on the input OVN host subnet and service network CIDR, and
// returns the its path
func generateCNIConf(ovnHostSubnet, serviceNetworkCIDR string) (string, error) {
	// cniConf is used in replacing the template values in templates/cni.template
	type cniConf struct {
		OvnHostSubnet      string
		ServiceNetworkCIDR string
	}
	confData := cniConf{ovnHostSubnet, serviceNetworkCIDR}

	// Read the contents of the template file
	content, err := ioutil.ReadFile(cniConfigTemplate)
	if err != nil {
		return "", fmt.Errorf("error reading CNI config template: %v", err)
	}

	cniConfTmpl := template.New("CNI")

	// Parse the template
	cniConfTmpl, err = cniConfTmpl.Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("error parsing CNI config template: %v", err)
	}

	// Create a temp file to hold the config
	tmpCniDir, err := ioutil.TempDir("", "cni")
	if err != nil {
		return "", fmt.Errorf("error creating local temp CNI directory: %v", err)
	}

	cniConfigPath, err := os.Create(filepath.Join(tmpCniDir, "cni.conf"))
	if err != nil {
		return "", fmt.Errorf("error creating local cni.conf: %v", err)
	}

	// Take the data values, replace it in the template and write the result out to a file
	if err = cniConfTmpl.Execute(cniConfigPath, confData); err != nil {
		return "", fmt.Errorf("error applying data to CNI config template: %v", err)
	}

	if err = cniConfigPath.Close(); err != nil {
		return "", fmt.Errorf("error closing %s: %v", cniConfigPath.Name(), err)
	}

	return cniConfigPath.Name(), nil
}

// waitForHybridOverlayAnnotation waits for the hybrid overlay subnet annotation to be present on the node until the
// timeout is reached
func waitForHybridOverlayAnnotation(nodeName string) error {
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		node, err := framework.K8sclientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error getting node %s: %v", nodeName, err)
		}
		_, found := node.Annotations[test.HybridOverlaySubnet]
		if found {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}
	return fmt.Errorf("timeout waiting for %s node annotation", test.HybridOverlaySubnet)
}

// hasWindowsTaint returns true if the given Windows node has the Windows taint
func hasWindowsTaint(winNodes []v1.Node) bool {
	// We've just created one Windows node as part of our CI suite. So, it's ok to return instead of checking for all
	// the items in the node
	for _, node := range winNodes {
		for _, taint := range node.Spec.Taints {
			if taint.Key == windowsTaint.Key && taint.Value == windowsTaint.Value && taint.Effect == windowsTaint.Effect {
				return true
			}
		}
	}
	return false
}

// testWMCBCluster runs the cluster tests for the nodes
func testWMCBCluster(t *testing.T) {
	// TODO: Fix this test for multiple VMs
	client := framework.K8sclientset
	winNodes, err := client.CoreV1().Nodes().List(context.TODO(),
		metav1.ListOptions{LabelSelector: "kubernetes.io/os=windows"})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Equal(t, hasWindowsTaint(winNodes.Items), true, "expected Windows Taint to be present on the Windows Node")
	winNodes, err = client.CoreV1().Nodes().List(context.TODO(),
		metav1.ListOptions{LabelSelector: e2ef.WindowsLabel})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Lenf(t, winNodes.Items, 1, "expected one node to have node label but found: %v", len(winNodes.Items))
	// Test Windows Nodes for Ready status
	for _, node := range winNodes.Items {
		readyCondition := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == v1.NodeReady {
				readyCondition = true
				assert.Equalf(t, v1.ConditionTrue, condition.Status, "expected Windows node %v should be in %v State",
					node.Name, condition.Status)
			}
		}
		assert.Truef(t, readyCondition, "expected node Status to have condition type Ready for node %v", node.Name)
	}
}
