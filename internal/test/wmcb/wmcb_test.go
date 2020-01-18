package wmcb

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/openshift/windows-machine-config-operator/internal/test"
	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificates "k8s.io/api/certificates/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const (
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
	hybridOverlayName = "hybrid-overlay.exe"
	// hybridOverExecutable is the remote location of the hybrid overlay binary
	hybridOverlayExecutable = remoteDir + hybridOverlayName
)

var (
	// windowsTaint is the taint that needs to be applied to the Windows node
	windowsTaint = v1.Taint{
		Key:    "os",
		Value:  "Windows",
		Effect: v1.TaintEffectNoSchedule,
	}
	// filesToBeTransferred holds the list of files that needs to be transferred to the Windows VM
	filesToBeTransferred = flag.String("filesToBeTransferred", "",
		"Comma separated list of files to be transferred")
	// kubeNode contains the information about  the kubernetes node package for Windows
	kubeNode = pkgInfo{
		url:     "https://dl.k8s.io/v1.16.2/kubernetes-node-windows-amd64.tar.gz",
		sha:     "a88e7a1c6f72ea6073dbb4ddfe2e7c8bd37c9a56d94a33823f531e303a9915e7a844ac5880097724e44dfa7f4a9659d14b79cc46e2067f6b13e6df3f3f1b0f64",
		shaType: "sha512",
	}
	// cniPlugins contains information about the CNI plugin package
	cniPlugins = pkgInfo{
		url:     "https://github.com/containernetworking/plugins/releases/download/v0.8.2/cni-plugins-windows-amd64-v0.8.2.tgz",
		sha:     "705a760673fd9e2164ac38f0df7d739ca6c3ec4f4204b0c439227ec6da7cb153859013c917e7f8f1a9456365dd9193f627a7e9e4e1981725cab89bb5ab881ec0",
		shaType: "sha512",
	}
)

// wmcbVM is a wrapper for the WindowsVM interface that associates it with WMCB specific testing
type wmcbVM struct {
	e2ef.WindowsVM
}

// pkg encapsulates information about a package
type pkgInfo struct {
	// url is the URL of the package
	url string
	// sha is the SHA hash of the package
	sha string
	// shaType is the type of SHA used, example: 256 or 512
	shaType string
}

// TestWMCB runs the unit and e2e tests for WMCB on the remote VMs
func TestWMCB(t *testing.T) {
	remoteDir := "C:\\Temp"
	for _, vm := range framework.WinVMs {
		wVM := &wmcbVM{vm}
		files := strings.Split(*filesToBeTransferred, ",")
		for _, file := range files {
			err := wVM.CopyFile(file, remoteDir)
			require.NoError(t, err, "error copying %s to the Windows VM", file)
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

	// Handle the bootstrap and node CSRs
	err := handleCSRs()
	require.NoError(t, err, "error handling CSRs")

	vm.runTestConfigureCNI(t)
}

// runTest runs the testCmd in the given VM
func (vm *wmcbVM) runTest(testCmd string) error {
	stdout, stderr, err := vm.Run(testCmd, true)

	// Logging the output so that it is visible on the CI page
	log.Printf("\n%s\n", stdout)
	log.Printf("\n%s\n", stderr)

	if err != nil {
		return fmt.Errorf("error running test: %v", err)
	}
	if stderr != "" {
		return fmt.Errorf("test returned stderr output")
	}
	if strings.Contains(stdout, "FAIL") {
		return fmt.Errorf("test output showed a failure")
	}
	if strings.Contains(stdout, "panic") {
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
	err := vm.initializeHybridOverlayBinary()
	require.NoError(t, err, "error initializing files required for TestConfigureCNI")

	node, err := framework.GetNode(vm.GetCredentials().GetIPAddress())
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
	_, _, err := vm.Run(mkdirCmd(remoteDir), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v", remoteDir, err)
	}

	// Download and extract the kube package on the VM
	err = vm.remoteDownloadExtract(kubeNode, remoteDir+"kube.tar.gz", remoteDir)
	if err != nil {
		return fmt.Errorf("unable to download kube package: %v", err)
	}

	// Copy kubelet.exe to C:\Windows\Temp\
	_, _, err = vm.Run("cp "+remoteDir+"kubernetes\\node\\bin\\kubelet.exe "+winTemp, true)
	if err != nil {
		return fmt.Errorf("unable to copy kubelet.exe to %s", winTemp)
	}

	// Download the worker ignition to C:\Windows\Tenp\ using the script that ignores the server cert
	_, _, err = vm.Run(wgetIgnoreCertCmd+" -server https://api-int."+e2ef.ClusterAddress+":22623/config/worker"+" -output "+winTemp+"worker.ign", true)
	if err != nil {
		return fmt.Errorf("unable to download worker.ign: %v", err)
	}

	return nil
}

// remoteDownload downloads the tar file in url to the remoteDownloadFile location and checks if the SHA matches
func (vm *wmcbVM) remoteDownload(pkg pkgInfo, remoteDownloadFile string) error {
	_, stderr, err := vm.Run("if (!(Test-Path "+remoteDownloadFile+")) { wget "+pkg.url+" -o "+remoteDownloadFile+" }",
		true)
	if err != nil {
		return fmt.Errorf("unable to download %s: %v\n%s", pkg.url, err, stderr)
	}

	if pkg.sha == "" {
		return nil
	}

	// Perform a checksum check
	stdout, _, err := vm.Run("certutil -hashfile "+remoteDownloadFile+" "+pkg.shaType, true)
	if err != nil {
		return fmt.Errorf("unable to check SHA of %s: %v", remoteDownloadFile, err)
	}
	if !strings.Contains(stdout, pkg.sha) {
		return fmt.Errorf("package %s SHA does not match: %v\n%s", remoteDownloadFile, err, stdout)
	}

	return nil
}

// remoteDownloadExtract downloads the tar file in url to the remoteDownloadFile location, checks if the SHA matches and
//  extracts the files to the remoteExtractDir directory
func (vm *wmcbVM) remoteDownloadExtract(pkg pkgInfo, remoteDownloadFile, remoteExtractDir string) error {
	// Download the file from the URL
	err := vm.remoteDownload(pkg, remoteDownloadFile)
	if err != nil {
		return fmt.Errorf("unable to download %s: %v", pkg.url, err)
	}

	// Extract files from the archive
	_, stderr, err := vm.Run("tar -xf "+remoteDownloadFile+" -C "+remoteExtractDir, true)
	if err != nil {
		return fmt.Errorf("unable to extract %s: %v\n%s", remoteDownloadFile, err, stderr)
	}
	return nil
}

// initializeTestConfigureCNIFiles initializes the files required for configure-cni
func (vm *wmcbVM) initializeTestConfigureCNIFiles(ovnHostSubnet string) error {
	// Create the CNI directory C:\Windows\Temp\cni on the Windows VM
	_, stderr, err := vm.Run(mkdirCmd(winCNIDir), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v\n%s", remoteDir, err, stderr)
	}

	cniUrl, err := url.Parse(cniPlugins.url)
	if err != nil {
		return fmt.Errorf("error parsing %s: %v", cniPlugins.url, err)
	}

	// Download and extract the CNI binaries on the Windows VM
	err = vm.remoteDownloadExtract(cniPlugins, remoteDir+path.Base(cniUrl.Path), winCNIDir)
	if err != nil {
		return fmt.Errorf("unable to download CNI package: %v", err)
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

// initializeHybridOverlayBinary creates the files on the Windows node needed for running "configure-cni"
func (vm *wmcbVM) initializeHybridOverlayBinary() error {
	hybridOverlayURL, err := framework.GetReleaseArtifactURL(hybridOverlayName)
	if err != nil {
		return fmt.Errorf("unable to get %s URL: %v", hybridOverlayName, err)
	}

	hybridOverlaySHA, err := framework.GetReleaseArtifactSHA(hybridOverlayName)
	if err != nil {
		return fmt.Errorf("unable to get %s SHA: %v", hybridOverlayName, err)
	}

	hybridOverlay := pkgInfo{
		url:     hybridOverlayURL,
		sha:     hybridOverlaySHA,
		shaType: "sha256",
	}

	err = vm.remoteDownload(hybridOverlay, hybridOverlayExecutable)
	if err != nil {
		return fmt.Errorf("unable to download %s on VM: %s", hybridOverlayURL, err)
	}
	return nil
}

// handleHybridOverlay ensures that the hybrid overlay is running on the node
func (vm *wmcbVM) handleHybridOverlay(nodeName string) error {
	// Check if the hybrid-overlay is running
	_, stderr, err := vm.Run("Get-Process -Name \"hybrid-overlay\"", true)

	// stderr being empty implies that an hybrid-overlay was running. This is to help with local development.
	if err == nil || stderr == "" {
		return nil
	}

	// Wait until the node object has the hybrid overlay subnet annotation. Otherwise the hybrid-overlay will fail to
	// start
	if err = waitForHybridOverlayAnnotation(nodeName); err != nil {
		return fmt.Errorf("error waiting for hybrid overlay node annotation: %v", err)
	}

	_, stderr, err = vm.Run(mkdirCmd(kLog), false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v\n%s", kLog, err, stderr)
	}

	// Start the hybrid-overlay in the background over ssh. We cannot use vm.Run() and by extension WinRM.Run() here as
	// we observed WinRM.Run() returning before the commands completes execution. The reason for that is unclear and
	// requires further investigation.
	go vm.RunOverSSH(hybridOverlayExecutable+" --node "+nodeName+
		" --k8s-kubeconfig c:\\k\\kubeconfig > "+kLog+"hybrid-overlay.log 2>&1", false)

	err = vm.waitForHybridOverlayToRun()
	if err != nil {
		return fmt.Errorf("error running %s: %v", hybridOverlayName, err)
	}

	err = vm.waitForOpenShiftHNSNetworks()
	if err != nil {
		return fmt.Errorf("error waiting for OpenShift HNS networks to be created: %v", err)
	}

	// Running the hybrid-overlay causes network reconfiguration in the Windows VM which results in the ssh connection
	// being closed and the client is not smart enough to reconnect. We have observed that the WinRM connection does not
	// get closed and does not need reinitialization.
	err = vm.Reinitialize()

	return nil
}

// waitForOpenShiftHSNNetworks waits for the OpenShift HNS networks to be created until the timeout is reached
func (vm *wmcbVM) waitForOpenShiftHNSNetworks() error {
	var stdout string
	var err error
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		stdout, _, err = vm.Run("Get-HnsNetwork", true)
		if err != nil {
			// retry
			continue
		}

		if strings.Contains(stdout, "BaseOpenShiftNetwork") &&
			strings.Contains(stdout, "OpenShiftNetwork") {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}

	// OpenShift HNS networks were not found
	log.Printf("Get-HnsNetwork:\n%s", stdout)
	return fmt.Errorf("timeout waiting for OpenShift HNS networks: %v", err)
}

// waitForHybridOverlayToRun waits for the hybrid-overlay.exe to run until the timeout is reached
func (vm *wmcbVM) waitForHybridOverlayToRun() error {
	var err error
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		_, _, err = vm.Run("Get-Process -Name \"hybrid-overlay\"", true)
		if err == nil {
			return nil
		}
		time.Sleep(e2ef.RetryInterval)
	}

	// hybrid-overlay never started running
	return fmt.Errorf("timeout waiting for hybrid-overlay: %v", err)
}

// approve approves the given CSR if it has not already been approved
// Based on https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/certificates/certificates.go#L237
func approve(csr *certificates.CertificateSigningRequest) error {
	// Check if the certificate has already been approved
	for _, c := range csr.Status.Conditions {
		if c.Type == certificates.CertificateApproved {
			return nil
		}
	}

	// Approve the CSR
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Ensure we get the current version
		csr, err := framework.K8sclientset.CertificatesV1beta1().CertificateSigningRequests().Get(
			csr.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Add the approval status condition
		csr.Status.Conditions = append(csr.Status.Conditions, certificates.CertificateSigningRequestCondition{
			Type:           certificates.CertificateApproved,
			Reason:         "WMCBe2eTestRunnerApprove",
			Message:        "This CSR was approved by WMCB e2e test runner",
			LastUpdateTime: metav1.Now(),
		})

		_, err = framework.K8sclientset.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(csr)
		return err
	})
}

//findCSR finds the CSR that matches the requestor filter
func findCSR(requestor string) (*certificates.CertificateSigningRequest, error) {
	var foundCSR *certificates.CertificateSigningRequest
	// Find the CSR
	for retries := 0; retries < e2ef.RetryCount; retries++ {
		csrs, err := framework.K8sclientset.CertificatesV1beta1().CertificateSigningRequests().List(metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to get CSR list: %v", err)
		}
		if csrs == nil {
			time.Sleep(e2ef.RetryInterval)
			continue
		}

		for _, csr := range csrs.Items {
			if !strings.Contains(csr.Spec.Username, requestor) {
				continue
			}
			var handledCSR bool
			for _, c := range csr.Status.Conditions {
				if c.Type == certificates.CertificateApproved || c.Type == certificates.CertificateDenied {
					handledCSR = true
					break
				}
			}
			if handledCSR {
				continue
			}
			foundCSR = &csr
			break
		}

		if foundCSR != nil {
			break
		}
		time.Sleep(e2ef.RetryInterval)
	}

	if foundCSR == nil {
		return nil, fmt.Errorf("unable to find CSR with requestor %s", requestor)
	}
	return foundCSR, nil
}

// handleCSR finds the CSR based on the requestor filter and approves it
func handleCSR(requestorFilter string) error {
	csr, err := findCSR(requestorFilter)
	if err != nil {
		return fmt.Errorf("error finding CSR for %s: %v", requestorFilter, err)
	}

	if err = approve(csr); err != nil {
		return fmt.Errorf("error approving CSR for %s: %v", requestorFilter, err)
	}

	return nil
}

// handleCSRs handles the approval of bootstrap and node CSRs
func handleCSRs() error {
	// Handle the bootstrap CSR
	err := handleCSR("system:serviceaccount:openshift-machine-config-operator:node-bootstrapper")
	if err != nil {
		return fmt.Errorf("unable to handle bootstrap CSR: %v", err)
	}

	// Handle the node CSR
	// Note: for the product we want to get the node name from the instance information
	err = handleCSR("system:node:")
	if err != nil {
		return fmt.Errorf("unable to handle node CSR: %v", err)
	}

	return nil
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
	networkCR, err := framework.OSConfigClient.ConfigV1().Networks().Get("cluster", metav1.GetOptions{})
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
		node, err := framework.K8sclientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
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
	winNodes, err := client.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: "kubernetes.io/os=windows"})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Equal(t, hasWindowsTaint(winNodes.Items), true, "expected Windows Taint to be present on the Windows Node")
	winNodes, err = client.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: e2ef.WindowsLabel})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Lenf(t, winNodes.Items, 1, "expected one node to have node label but found: %v", len(winNodes.Items))
}
