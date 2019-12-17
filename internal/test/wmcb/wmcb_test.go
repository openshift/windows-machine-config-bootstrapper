package wmcb

import (
	"flag"
	"log"
	"path/filepath"
	"testing"

	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
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
	// binaryToBeTransferred holds the binary that needs to be transferred to the Windows VM
	// TODO: Make this an array later with a comma separated values for more binaries to be transferred
	binaryToBeTransferred = flag.String("binaryToBeTransferred", "",
		"Absolute path of the binary to be transferred")
)

// TestWMCBUnit runs the unit tests for WMCB
func TestWMCBUnit(t *testing.T) {
	for _, vm := range framework.WinVMs {
		runWMCBUnitTestSuite(t, vm)
	}
}

// runWMCBUnitTestSuite runs the unit test suite on the VM
func runWMCBUnitTestSuite(t *testing.T, vm e2ef.WindowsVM) {
	remoteDir := "C:\\Temp"
	err := vm.CopyFile(*binaryToBeTransferred, remoteDir)
	require.NoError(t, err, "error copying binary to the Windows VM")

	stdout, stderr, err := vm.Run(remoteDir+"\\"+filepath.Base(*binaryToBeTransferred)+" --test.v", true)
	assert.NoError(t, err, "error running WMCB unit test suite")
	log.Printf("\n%s\n", stdout)
	assert.Equal(t, "", stderr, "WMCB unit test returned error output")
	assert.NotContains(t, stdout, "FAIL", "WMCB unit test failed")
	assert.NotContains(t, stdout, "panic", "WMCB unit test panic")
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
