package wmcb

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"testing"

	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// remotePowerShellCmdPrefix holds the powershell prefix that needs to be prefixed to every command run on the
	// remote powershell session opened
	remotePowerShellCmdPrefix = "powershell.exe -NonInteractive -ExecutionPolicy Bypass "
	// nodeLabels represents the node label that need to be applied to the Windows node created
	nodeLabel = "node.openshift.io/os_id=Windows"
)

var (
	// windowsTaint is the taint that needs to be applied to the Windows node
	windowsTaint = v1.Taint{
		Key:    "os",
		Value:  "Windows",
		Effect: v1.TaintEffectNoSchedule,
	}
)

// TestWMCBUnit runs the unit tests for WMCB
func TestWMCBUnit(t *testing.T) {
	for _, vm := range framework.WinVMs {
		runWMCBUnitTestSuiteOnVM(t, vm)
	}
}

func runWMCBUnitTestSuiteOnVM(t *testing.T, vm *e2ef.WindowsVM) {
	// Transfer the binary to the windows using scp
	defer vm.SSHClient.Close()
	sftp, err := sftp.NewClient(vm.SSHClient)
	require.NoError(t, err, "sftp client initialization failed")
	defer sftp.Close()
	f, err := os.Open(*binaryToBeTransferred)
	require.NoErrorf(t, err, "error opening binary file to be transferred: %s", *binaryToBeTransferred)
	dstFile, err := sftp.Create(vm.RemoteDir + "\\" + "wmcb_unit_test.exe")
	require.NoError(t, err, "error opening binary file to be transferred")
	_, err = io.Copy(dstFile, f)
	require.NoError(t, err, "error copying binary to the Windows VM")

	// Forcefully close it so that we can execute the binary later
	dstFile.Close()

	stdout := os.Stdout
	r, w, err := os.Pipe()
	assert.NoError(t, err, "error opening pipe to read stdout")
	os.Stdout = w

	// Remotely execute the test binary.
	exitCode, err := vm.WinrmClient.Run(remotePowerShellCmdPrefix+vm.RemoteDir+"\\"+
		"wmcb_unit_test.exe --test.v",
		os.Stdout, os.Stderr)
	assert.NoError(t, err, "error while executing the test binary remotely")
	assert.Equal(t, 0, exitCode, "remote binary returned non-zero exit code")
	w.Close()
	out, err := ioutil.ReadAll(r)
	assert.NoError(t, err, "error reading stdout from the remote Windows VM")
	os.Stdout = stdout
	log.Printf("%s", out)
	assert.NotContains(t, string(out), "FAIL")
	assert.NotContains(t, string(out), "panic")
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

// TestWMCBCluster runs the cluster tests for the nodes
func TestWMCBCluster(t *testing.T) {
	//TODO: Transfer the WMCB binary to the Windows node and approve CSR for the Windows node.
	// I want this to be moved to another test. We've another card for this, so let's come back
	// to that later(WINC-82). As of now, this test is limited to check if the taint has been
	// applied to the Windows node and skipped for now.
	client := framework.K8sclientset
	winNodes, err := client.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: "kubernetes.io/os=windows"})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Equal(t, hasWindowsTaint(winNodes.Items), true, "expected Windows Taint to be present on the Windows Node")
	winNodes, err = client.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: nodeLabel})
	require.NoErrorf(t, err, "error while getting Windows node: %v", err)
	assert.Lenf(t, winNodes.Items, 1, "expected one node to have node label but found: %v", len(winNodes.Items))
}
