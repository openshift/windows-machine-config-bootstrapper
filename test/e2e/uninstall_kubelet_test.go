package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
)

// TestKubeletUninstall tests if WMCB returns an error if the kubelet is uninstalled
func TestKubeletUninstall(t *testing.T) {
	wmcb, err := bootstrapper.NewWinNodeBootstrapper("", "", "", "", "", "", "", "")
	require.NoError(t, err, "could not create wmcb")

	err = wmcb.UninstallKubelet()
	assert.NoError(t, err, "unable to uninstall kubelet service")

	err = wmcb.Disconnect()
	assert.NoError(t, err, "could not disconnect from windows svc API")

	assert.False(t, svcExists(t, bootstrapper.KubeletServiceName))
}

// testUninstallWithoutKubeletSvc tests if WMCB returns an error if the kubelet is uninstalled with no kubelet service present
func testUninstallWithoutKubeletSvc(t *testing.T) {
	if svcExists(t, bootstrapper.KubeletServiceName) {
		t.Skip("Skipping as kubelet service already exists")
	}

	wmcb, err := bootstrapper.NewWinNodeBootstrapper("", "", "", "", "", "", "", "")
	require.NoError(t, err, "could not create wmcb")

	err = wmcb.UninstallKubelet()
	assert.Error(t, err, "no error when attempting to uninstall kubelet without kubelet svc")
	assert.Contains(t, err.Error(), "kubelet service is not present", "incorrect error thrown")
}
