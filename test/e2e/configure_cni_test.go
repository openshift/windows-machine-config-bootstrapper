package e2e

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cniDir string
var cniConfig string

func init() {
	pflag.StringVar(&cniDir, "cni-dir", "C:\\Windows\\Temp\\cni", "CNI binary location")
	pflag.StringVar(&cniConfig, "cni-config", "C:\\Windows\\Temp\\cni\\config\\cni.conf", "CNI config location")
}

func TestConfigureCNI(t *testing.T) {
	t.Run("Configure CNI", testConfigureCNI)
}

// testConfigureCNIWithoutKubeletSvc tests if WMCB returns an error if CNI configuration is attempted without a kubelet
// service
func testConfigureCNIWithoutKubeletSvc(t *testing.T) {
	if svcExists(t, bootstrapper.KubeletServiceName) {
		t.Skip("Skipping as kubelet service already exists")
	}

	// Create a temp directory and cni.conf for use here as cni-dir and cni-config needn't be created at this point as
	// this test is called from TestBootstrapper
	tempDir, err := ioutil.TempDir("", "wmcb")
	require.NoError(t, err, "could not create temp directory")
	cniConfig, err := ioutil.TempFile(tempDir, "cni.conf")
	require.NoError(t, err, "could not create temp CNI config")
	// Ignore the return error as there is not much we can do if the temporary directory is not deleted
	defer os.RemoveAll(tempDir)

	// Instantiate the bootstrapper
	wmcb, err := bootstrapper.NewWinNodeBootstrapper(tempDir, "", "", "", "",
		tempDir, cniConfig.Name(), platformType)
	require.NoError(t, err, "could not instantiate wmcb")

	err = wmcb.Configure()
	assert.Error(t, err, "no error when attempting to configure CNI without kubelet svc")
	assert.Contains(t, err.Error(), "kubelet service is not present", "incorrect error thrown")
}

// testConfigureCNI tests if ConfigureCNI() runs successfully by checking if the kubelet service comes up after
// configuring CNI
func testConfigureCNI(t *testing.T) {
	wmcb, err := bootstrapper.NewWinNodeBootstrapper(installDir, "", "", "", "",
		cniDir, cniConfig, platformType)
	require.NoError(t, err, "could not create wmcb")

	err = wmcb.Configure()
	assert.NoError(t, err, "error running wmcb.ConfigureCNI")

	err = wmcb.Disconnect()
	assert.NoError(t, err, "could not disconnect from windows svc API")

	// Wait for the service to start
	time.Sleep(2 * time.Second)
	assert.Truef(t, svcRunning(t, bootstrapper.KubeletServiceName),
		"kubelet service is not running after configuring CNI")

	// Wait for kubelet log to be populated
	time.Sleep(10 * time.Second)

	assert.True(t, isKubeletRunning(t, kubeletLogPath))

	// Kubelet arguments with paths that are set by configure-cni
	// Does not include arguments with paths that do not depend on underlying OS
	checkPathsFor := []string{"--cni-bin-dir", "--cni-conf-dir"}
	_, path, _, err := getSvcInfo(bootstrapper.KubeletServiceName)
	require.NoError(t, err, "Could not get kubelet arguments")

	t.Run("Test the paths in Kubelet arguments", func(t *testing.T) {
		testPathInKubeletArgs(t, checkPathsFor, path)
	})
}
