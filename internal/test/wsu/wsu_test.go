package wsu

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// clusterAddress is the address of the OpenShift cluster e.g. "foo.fah.com".
	// This should not include "https://api-" or a port
	clusterAddress = os.Getenv("CLUSTER_ADDR")

	// Temp directory ansible created on the windows host
	ansibleTempDir = ""
	// kubernetes-node-windows-amd64.tar.gz SHA512
	// Value from https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG-1.16.md#node-binaries-1
	// This value should be updated when we change the kubelet version in WSU
	expectedKubeTarSha = "a88e7a1c6f72ea6073dbb4ddfe2e7c8bd37c9a56d94a33823f531e303a9915e7a844ac5880097724e44dfa7f4" +
		"a9659d14b79cc46e2067f6b13e6df3f3f1b0f64"
	// workerLabel is the worker label that needs to be applied to the Windows node
	workerLabel = "node-role.kubernetes.io/worker"
	// windowsLabel represents the node label that need to be applied to the Windows node created
	windowsLabel = "node.openshift.io/os_id=Windows"
	// hybridOverlaySubnet is an annotation applied by the cluster network operator which is used by the hybrid overlay
	hybridOverlaySubnet = "k8s.ovn.org/hybrid-overlay-node-subnet"
	// hybridOverlayMac is an annotation applied by the hybrid overlay
	hybridOverlayMac = "k8s.ovn.org/hybrid-overlay-distributed-router-gateway-mac"

	// windowsServerImage is the name/location of the Windows Server 2019 image we will use to test pod deployment
	windowsServerImage = "mcr.microsoft.com/windows/servercore:ltsc2019"
	// ubi8Image is the name/location of the linux image we will use for testing
	ubi8Image = "registry.access.redhat.com/ubi8/ubi:latest"

	// retryCount is the amount of times we will retry an api operation
	retryCount = 20
	// retryInterval is the interval of time until we retry after a failure
	retryInterval = 5 * time.Second
	// remotePowerShellCmdPrefix holds the powershell prefix that needs to be prefixed to every command run on the
	// remote powershell session opened
	remotePowerShellCmdPrefix = "powershell.exe -NonInteractive -ExecutionPolicy Bypass "
)

// createhostFile creates an ansible host file and returns the path of it
func createHostFile(ip, password string) (string, error) {
	hostFile, err := ioutil.TempFile("", "testWSU")
	if err != nil {
		return "", fmt.Errorf("coud not make temporary file: %s", err)
	}
	defer hostFile.Close()

	_, err = hostFile.WriteString(fmt.Sprintf(`[win]
%s ansible_password='%s'
[win:vars]
ansible_user=Administrator
cluster_address=%s
ansible_port=5986
ansible_connection=winrm
ansible_winrm_server_cert_validation=ignore`, ip, password, clusterAddress))
	return hostFile.Name(), err
}

// TestWSU creates a Windows instance, runs the WSU, and then runs a series of tests to ensure all expected
// behavior was achieved. The following environment variables must be set for this test to run: KUBECONFIG,
// AWS_SHARED_CREDENTIALS_FILE, ARTIFACT_DIR, KUBE_SSH_KEY_PATH, WSU_PATH, CLUSTER_ADDR
func TestWSU(t *testing.T) {
	require.NotEmptyf(t, playbookPath, "WSU_PATH environment variable not set")
	require.NotEmptyf(t, clusterAddress, "CLUSTER_ADDR environment variable not set")

	// In order to run the ansible playbook we create an inventory file:
	// https://docs.ansible.com/ansible/latest/user_guide/intro_inventory.html
	hostFilePath, err := createHostFile(framework.Credentials.GetIPAddress(), framework.Credentials.GetPassword())
	require.NoErrorf(t, err, "Could not write to host file: %s", err)
	cmd := exec.Command("ansible-playbook", "-vvv", "-i", hostFilePath, playbookPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "WSU playbook returned error: %s, with output: %s", err, string(out))

	// Ansible will copy files to a temporary directory with a path such as:
	// C:\\Users\\Administrator\\AppData\\Local\\Temp\\ansible.z5wa1pc5.vhn\\
	initialSplit := strings.Split(string(out), "C:\\\\Users\\\\Administrator\\\\AppData\\\\Local\\\\Temp\\\\ansible.")
	require.True(t, len(initialSplit) > 1, "Could not find Windows temp dir: %s", out)
	ansibleTempDir = "C:\\Users\\Administrator\\AppData\\Local\\Temp\\ansible." + strings.Split(initialSplit[1], "\"")[0]

	t.Run("Files copied to Windows node", testFilesCopied)
	t.Run("Pending CSRs were approved", testNoPendingCSRs)
	t.Run("Node is in ready state", testNodeReady)
	// test if the Windows node has proper worker label.
	t.Run("Check if worker label has been applied to the Windows node", testWorkerLabelsArePresent)
	t.Run("Network annotations were applied to node", testHybridOverlayAnnotations)
	t.Run("HNS Networks were created", testHNSNetworksCreated)
	t.Run("Check cni config generated on the Windows host", testCNIConfig)
	t.Run("East-west networking", testEastWestNetworking)
	t.Run("North-south networking", testNorthSouthNetworking)
}

// testCNIConfig tests if the CNI config has required hostsubnet and servicenetwork CIDR
// NOTE: split this into multiple tests when this grows
func testCNIConfig(t *testing.T) {
	// Read the CNI config present on the Windows host
	cniConfigFilePath := filepath.Join(ansibleTempDir, "cni", "config", "cni.conf")
	cniConfigFileContents, err := readRemoteFile(cniConfigFilePath)
	require.NoError(t, err, "Could not get CNI config contents")

	// Get the Windows node object
	windowsNodes, err := framework.K8sclientset.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: windowsLabel})
	require.NoError(t, err, "Could not get a list of Windows nodes")
	require.Equalf(t, len(windowsNodes.Items), 1, "Expected one Windows node to be present but found %v",
		len(windowsNodes.Items))

	// By the time, we reach here the annotation should be present, so need to validate again
	hostSubnet := windowsNodes.Items[0].Annotations[hybridOverlaySubnet]
	// Check if the host subnet matches our expected value
	assert.Contains(t, cniConfigFileContents, hostSubnet, "CNI config does not contain host subnet")

	// Check if the service CIDR matches our expected value
	networkCR, err := framework.OSConfigClient.ConfigV1().Networks().Get("cluster", metav1.GetOptions{})
	require.NoError(t, err, "Error querying network object")
	serviceNetworks := networkCR.Spec.ServiceNetwork
	// The serviceNetwork should be a singleton slice as of now, let's try accessing the first element in it.
	require.Equalf(t, len(serviceNetworks), 1, "Expected service network to be a singleton but got %v",
		len(serviceNetworks))
	requiredServiceNetwork := serviceNetworks[0]
	assert.Contains(t, cniConfigFileContents, requiredServiceNetwork, "CNI config does not contain service network")
}

// testFilesCopied tests that the files we attempted to copy to the Windows host, exist on the Windows host
func testFilesCopied(t *testing.T) {
	expectedFileList := []string{"kubelet.exe", "worker.ign", "wmcb.exe", "hybrid-overlay.exe", "kube.tar.gz"}

	// Check if each of the files we expect on the Windows host are there
	for _, filename := range expectedFileList {
		fullPath := ansibleTempDir + "\\" + filename
		// This command will write to stdout, only if the file we are looking for does not exist
		command := fmt.Sprintf("if not exist %s echo fail", fullPath)
		stdout := new(bytes.Buffer)
		_, err := framework.WinrmClient.Run(command, stdout, os.Stderr)
		assert.NoError(t, err, "Error looking for %s: %s", fullPath, err)
		assert.Emptyf(t, stdout.String(), "Missing file: %s", fullPath)
	}

	// Check the SHA of kube.tar.gz downloaded
	kubeTarPath := ansibleTempDir + "\\" + "kube.tar.gz"
	// certutil is part of default OS installation Windows 7+
	command := fmt.Sprintf("certutil -hashfile %s SHA512", kubeTarPath)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	_, err := framework.WinrmClient.Run(command, stdout, stderr)
	require.NoError(t, err, "Error generating SHA512 for %s", kubeTarPath)
	require.Equalf(t, stderr.Len(), 0, "Error generating SHA512 for %s", kubeTarPath)
	// CertUtil output example:
	// SHA512 hash of <filepath>:\r\n<SHA-output>\r\nCertUtil: -hashfile command completed successfully.
	// Extracting SHA value from the output
	actualKubeTarSha := strings.Split(stdout.String(), "\r\n")[1]
	assert.Equal(t, expectedKubeTarSha, actualKubeTarSha,
		"kube.tar.gz downloaded does not match expected checksum")
}

// testNodeReady tests that the bootstrapped node was added to the cluster and is in the ready state
func testNodeReady(t *testing.T) {
	var createdNode *v1.Node
	nodes, err := framework.K8sclientset.CoreV1().Nodes().List(metav1.ListOptions{})
	require.NoError(t, err, "Could not get list of nodes")
	require.NotZero(t, len(nodes.Items), "No nodes found")

	// Find the node that we spun up
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == "ExternalIP" && address.Address == framework.Credentials.GetIPAddress() {
				createdNode = &node
				break
			}
		}
		if createdNode != nil {
			break
		}
	}
	require.NotNil(t, createdNode, "Created node not found through Kubernetes API")

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
	csrs, err := framework.K8sclientset.CertificatesV1beta1().CertificateSigningRequests().List(metav1.ListOptions{})
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
func testWorkerLabelsArePresent(t *testing.T) {
	// Check if the Windows node has the required label needed.
	windowsNodes, err := framework.K8sclientset.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: windowsLabel})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Equalf(t, len(windowsNodes.Items), 1, "expected 1 windows nodes to be present but found %v",
		len(windowsNodes.Items))
	assert.Contains(t, windowsNodes.Items[0].Labels, workerLabel,
		"expected worker label to be present on the Windows node but did not find any")
}

// readRemoteFile returns the contents of a remote file. Returns an error on winRM failure, or if it does not exist.
func readRemoteFile(fileName string) (string, error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	// Read the file from remote host and check if the hostSubnet is present
	command := remotePowerShellCmdPrefix + "cat " + fileName
	errorCode, err := framework.WinrmClient.Run(command, stdout, stderr)
	if err != nil {
		return "", fmt.Errorf("WinRM failure trying to run cat: %s", err)
	}
	stderrString := stderr.String()
	if errorCode != 0 {
		return "", fmt.Errorf("remote cat return code %d, stderr: %s", errorCode, stderrString)
	}
	return stdout.String(), nil
}

// testHybridOverlayAnnotations tests that the correct annotations have been added to the bootstrapped node
func testHybridOverlayAnnotations(t *testing.T) {
	windowsNodes, err := framework.K8sclientset.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: windowsLabel})
	require.NoError(t, err, "Could not get list of Windows nodes")
	assert.Equalf(t, len(windowsNodes.Items), 1, "expected one windows node to be present but found %v",
		len(windowsNodes.Items))
	assert.Contains(t, windowsNodes.Items[0].Annotations, hybridOverlaySubnet)
	assert.Contains(t, windowsNodes.Items[0].Annotations, hybridOverlayMac)
}

// testHNSNetworksCreated tests that the required HNS Networks have been created on the bootstrapped node
func testHNSNetworksCreated(t *testing.T) {
	stdout := new(bytes.Buffer)
	_, err := framework.WinrmClient.Run("powershell Get-HnsNetwork", stdout, os.Stderr)
	require.NoError(t, err, "Could not run Get-HnsNetwork command")
	stdoutString := stdout.String()
	assert.Contains(t, stdoutString, "Name                   : BaseOpenShiftNetwork",
		"Could not find BaseOpenShiftNetwork in list of HNS Networks")
	assert.Contains(t, stdoutString, "Name                   : OpenShiftNetwork",
		"Could not find OpenShiftNetwork in list of HNS Networks")
}

// testEastWestNetworking deploys Windows and Linux pods, and tests that the pods can communicate
func testEastWestNetworking(t *testing.T) {
	// Deploy a webserver pod on the new node
	winServerDeployment, err := deployWindowsWebServer()
	require.NoError(t, err, "Could not create Windows Server deployment")
	defer deleteDeployment(winServerDeployment.Name)

	// Get the pod so we can use its IP
	winServerIP, err := getPodIP(*winServerDeployment.Spec.Selector)
	require.NoError(t, err, "Could not retrieve pod with selector %v", *winServerDeployment.Spec.Selector)

	// test Windows <-> Linux
	// This will install curl and then curl the windows server.
	linuxCurlerCommand := []string{"bash", "-c", "yum update; yum install curl -y; curl " + winServerIP}
	linuxCurlerJob, err := createLinuxJob("linux-curler", linuxCurlerCommand)
	require.NoError(t, err, "Could not create Linux job")
	defer deleteJob(linuxCurlerJob.Name)
	err = waitUntilJobSucceeds(linuxCurlerJob.Name)
	assert.NoError(t, err, "Could not curl the Windows server from a linux container")

	// test Windows <-> Windows on same node
	// This will continually try to read from the Windows Server. We have to try multiple times as the Windows container
	// takes some time to finish initial network setup.
	winCurlerCommand := []string{"powershell.exe", "-command", "for (($i =0), ($j = 0); $i -lt 10; $i++) { " +
		"$response = Invoke-Webrequest -UseBasicParsing -Uri " + winServerIP +
		"; $code = $response.StatusCode; echo \"GET returned code $code\";" +
		"If ($code -eq 200) {exit 0}; Start-Sleep -s 10;}; exit 1" + winServerIP}
	winCurlerJob, err := createWindowsServerJob("win-curler", winCurlerCommand)
	require.NoError(t, err, "Could not create Windows job")
	defer deleteJob(winCurlerJob.Name)
	err = waitUntilJobSucceeds(winCurlerJob.Name)
	assert.NoError(t, err, "Could not curl the Windows webserver pod from a separate Windows container")

	// TODO: test Windows <-> Windows on different node
}

// deployWindowsWebServer creates a deployment with a single Windows Server pod, listening on port 80
func deployWindowsWebServer() (*appsv1.Deployment, error) {
	// Preload the image that will be used on the Windows node, to prevent download timeouts
	// and separate possible failure conditions into multiple operations
	err := pullDockerImage(windowsServerImage)
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
	winServerDeployment, err := createWindowsServerDeployment("win-server", winServerCommand)
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
	for i := 0; i < retryCount; i++ {
		job, err = framework.K8sclientset.BatchV1().Jobs(v1.NamespaceDefault).Get(name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if job.Status.Succeeded > 0 {
			return nil
		}
		if job.Status.Failed > 0 {
			return fmt.Errorf("job %v failed", job)
		}
		time.Sleep(retryInterval)
	}
	return fmt.Errorf("job %v timed out", job)
}

// waitUntilDeploymentScaled will return nil if the deployment reaches the amount of replicas specified in its spec
func waitUntilDeploymentScaled(name string) error {
	var deployment *appsv1.Deployment
	var err error
	for i := 0; i < retryCount; i++ {
		deployment, err = framework.K8sclientset.AppsV1().Deployments(v1.NamespaceDefault).Get(name,
			metav1.GetOptions{})
		if err != nil {
			return err
		}
		if *deployment.Spec.Replicas == deployment.Status.AvailableReplicas {
			return nil
		}
		time.Sleep(retryInterval)
	}
	return fmt.Errorf("timed out waiting for deployment %v to scale", deployment)
}

// getPodIP returns the IP of the pod that matches the label selector. If more than one pod match the
// selector, the function will return an error
func getPodIP(selector metav1.LabelSelector) (string, error) {
	selectorString := labels.Set(selector.MatchLabels).String()
	podList, err := framework.K8sclientset.CoreV1().Pods(v1.NamespaceDefault).List(metav1.ListOptions{
		LabelSelector: selectorString})
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
	job, err := jobsClient.Create(job)
	if err != nil {
		return nil, err
	}
	return job, err
}

// deleteJob deletes the job with the given name
func deleteJob(name string) error {
	jobsClient := framework.K8sclientset.BatchV1().Jobs(v1.NamespaceDefault)
	return jobsClient.Delete(name, &metav1.DeleteOptions{})
}

// createWindowsServerDeployment creates a deployment with a Windows Server 2019 container
func createWindowsServerDeployment(name string, command []string) (*appsv1.Deployment, error) {
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
	deploy, err := deploymentsClient.Create(deployment)
	if err != nil {
		return nil, err
	}
	return deploy, err
}

// deleteDeployment deletes the deployment with the given name
func deleteDeployment(name string) error {
	deploymentsClient := framework.K8sclientset.AppsV1().Deployments(v1.NamespaceDefault)
	return deploymentsClient.Delete(name, &metav1.DeleteOptions{})
}

// pullDockerImage pulls the designated image on the remote host
func pullDockerImage(name string) error {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	command := "docker pull " + name
	errorCode, err := framework.WinrmClient.Run(command, stdout, stderr)
	if err != nil {
		return fmt.Errorf("failed to remotely run docker pull: %s", err)
	}
	// Any failures will be captured when theres a non-zero return value
	stderrString := stderr.String()
	if errorCode != 0 {
		return fmt.Errorf("return code %d, stderr: %s", errorCode, stderrString)
	}
	return nil
}

// testNorthSouthNetworking deploys a Windows Server pod, and tests that we can network with it from outside the cluster
func testNorthSouthNetworking(t *testing.T) {
	// Deploy a webserver pod on the new node
	winServerDeployment, err := deployWindowsWebServer()
	require.NoError(t, err, "Could not create Windows Server deployment")
	defer deleteDeployment(winServerDeployment.Name)

	// Create a load balancer svc to expose the webserver
	loadBalancer, err := createLoadBalancer("win-server", *winServerDeployment.Spec.Selector)
	require.NoError(t, err, "Could not create load balancer for Windows Server")
	defer deleteService(loadBalancer.Name)
	loadBalancer, err = waitForLoadBalancerIngress(loadBalancer.Name)
	require.NoError(t, err, "Error waiting for load balancer ingress")

	// Try and read from the webserver through the load balancer. The load balancer takes a fair amount of time, ~3 min,
	// to start properly routing connections.
	resp, err := retryGET("http://"+loadBalancer.Status.LoadBalancer.Ingress[0].Hostname, retryInterval*3)
	require.NoError(t, err, "Could not GET from load balancer: %v", loadBalancer)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Non 200 response from webserver")
}

// retryGET will repeatedly try to GET from the provided URL
func retryGET(url string, retryInterval time.Duration) (*http.Response, error) {
	var resp *http.Response
	var err error
	for i := 0; i < retryCount; i++ {
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
	return framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault).Create(svcSpec)
}

// waitForLoadBalancerIngress waits until the load balancer has an external hostname ready
func waitForLoadBalancerIngress(name string) (*v1.Service, error) {
	var svc *v1.Service
	var err error
	for i := 0; i < retryCount; i++ {
		svc, err = framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault).Get(name,
			metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if len(svc.Status.LoadBalancer.Ingress) == 1 {
			return svc, nil
		}
		time.Sleep(retryInterval)
	}
	return nil, fmt.Errorf("timed out waiting for single ingress: %v", svc)
}

// deleteService deletes the service with the given name
func deleteService(name string) error {
	svcClient := framework.K8sclientset.CoreV1().Services(v1.NamespaceDefault)
	return svcClient.Delete(name, &metav1.DeleteOptions{})
}
