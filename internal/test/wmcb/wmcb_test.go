package wmcb

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

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
	// wgetIgnoreCertCmd is the remote location of the wget-ignore-cert.ps1 script
	wgetIgnoreCertCmd = remoteDir + "wget-ignore-cert.ps1"
	// e2eExecutable is the remote location of the WMCB e2e test binary
	e2eExecutable = remoteDir + "wmcb_e2e_test.exe"
	// unitExecutable is the remote location of the WMCB unit test binary
	unitExecutable = remoteDir + "wmcb_unit_test.exe"
	// kubePackageURL is the kubernetes node package URL
	kubePackageURL = "https://dl.k8s.io/v1.16.2/kubernetes-node-windows-amd64.tar.gz"
	// kubePackageSHA is the SHA512 of node package from
	// https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG-1.16.md#node-binaries-3
	kubePackageSHA = "a88e7a1c6f72ea6073dbb4ddfe2e7c8bd37c9a56d94a33823f531e303a9915e7a844ac5880097724e44dfa7f4a9659d14b79cc46e2067f6b13e6df3f3f1b0f64"
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
)

// wmcbVM is a wrapper for the WindowsVM interface that associates it with WMCB specific testing
type wmcbVM struct {
	e2ef.WindowsVM
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

// initializeTestBootstrapperFiles initializes the files required for initialize-kubelet
func (vm *wmcbVM) initializeTestBootstrapperFiles() error {
	// Create the temp directory
	_, _, err := vm.Run("if not exist "+remoteDir+" mkdir "+remoteDir, false)
	if err != nil {
		return fmt.Errorf("unable to create remote directory %s: %v", remoteDir, err)
	}

	// Download and extract the kube package on the VM
	err = vm.remoteDownloadExtract(kubePackageURL, kubePackageSHA, remoteDir+"kube.tar.gz", remoteDir)
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

// remoteDownloadExtract downloads the tar file in url to the remoteDownloadFile location and checks if the SHA matches
func (vm *wmcbVM) remoteDownload(url, sha, remoteDownloadFile string) error {
	_, stderr, err := vm.Run("if (!(Test-Path "+remoteDownloadFile+")) { wget "+url+" -o "+remoteDownloadFile+" }", true)
	if err != nil {
		return fmt.Errorf("unable to download %s: %v\n%s", url, err, stderr)
	}

	if sha == "" {
		return nil
	}

	// Perform a checksum check
	stdout, _, err := vm.Run("certutil -hashfile "+remoteDownloadFile+" sha512", true)
	if err != nil {
		return fmt.Errorf("unable to check SHA of %s: %v", remoteDownloadFile, err)
	}
	if !strings.Contains(stdout, sha) {
		return fmt.Errorf("package %s SHA does not match: %v\n%s", remoteDownloadFile, err, stdout)
	}

	return nil
}

// remoteDownloadExtract downloads the tar file in url to the remoteDownloadFile location, checks if the SHA matches and
//  extracts the files to the remoteExtractDir directory
func (vm *wmcbVM) remoteDownloadExtract(url, sha, remoteDownloadFile, remoteExtractDir string) error {
	// Download the file from the URL
	err := vm.remoteDownload(url, sha, remoteDownloadFile)
	if err != nil {
		return fmt.Errorf("unable to download %s: %v", url, err)
	}

	// Extract files from the archive
	_, stderr, err := vm.Run("tar -xf "+remoteDownloadFile+" -C "+remoteExtractDir, true)
	if err != nil {
		return fmt.Errorf("unable to extract %s: %v\n%s", remoteDownloadFile, err, stderr)
	}
	return nil
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
