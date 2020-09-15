package e2e

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var (
	ignitionURL string
	kubeletPath string
	installDir  string
)

const kubeletLogPath = "C:\\var\\log\\kubelet\\kubelet.log"

func init() {
	pflag.StringVar(&ignitionURL, "ignition-url", "", "machine config server URL")
	pflag.StringVar(&kubeletPath, "kubelet-path", "C:\\Windows\\Temp\\kubelet.exe", "kubelet location")
	pflag.StringVar(&installDir, "install-dir", "C:\\k", "Installation directory")
	pflag.Parse()
}

// TestBootstrapper tests that the bootstrapper was able to start the required services
// TODO: Consider adding functionality to this test to check if the underlying processes are running properly,
//  	 otherwise keep that functionality contained within other future tests
func TestBootstrapper(t *testing.T) {
	var kubeletRunningBeforeTest bool
	// If the kubelet is not running yet, we can run disruptive tests
	if svcExists(t, bootstrapper.KubeletServiceName) && svcRunning(t, bootstrapper.KubeletServiceName) {
		kubeletRunningBeforeTest = true
	}
	if !kubeletRunningBeforeTest {
		// Remove the kubelet logfile, so that when we parse it, we are looking at the current run only
		removeFileIfExists(t, kubeletLogPath)
	}

	t.Run("Configure CNI without kubelet service present", testConfigureCNIWithoutKubeletSvc)

	// Run the bootstrapper, which will start the kubelet service
	wmcb, err := bootstrapper.NewWinNodeBootstrapper(installDir, "", ignitionURL, kubeletPath, "", "")
	require.NoErrorf(t, err, "Could not create WinNodeBootstrapper: %s", err)
	err = wmcb.InitializeKubelet()
	assert.NoErrorf(t, err, "Could not run bootstrapper: %s", err)
	err = wmcb.Disconnect()
	assert.NoErrorf(t, err, "Could not disconnect from windows svc API: %s", err)

	t.Run("Kubelet Windows service starts", func(t *testing.T) {
		// Wait for the service to start
		time.Sleep(2 * time.Second)
		assert.Truef(t, svcRunning(t, bootstrapper.KubeletServiceName), "The kubelet service is not running")
	})

	t.Run("Kubelet enters running state", func(t *testing.T) {
		if kubeletRunningBeforeTest {
			t.Skip("Skipping as kubelet was already running before the test")
		}
		// Wait for kubelet log to be populated
		time.Sleep(30 * time.Second)
		assert.True(t, isKubeletRunning(t, kubeletLogPath))
	})

	// Kubelet arguments with paths that are set by bootstrapper
	// Does not include node-labels and container-image since their paths do not depend on underlying OS
	checkPathsFor := []string{"--bootstrap-kubeconfig", "--cloud-config", "--config", "--kubeconfig", "--log-file",
		"--cert-dir"}
	t.Run("Test the paths in Kubelet arguments", func(t *testing.T) {
		testPathInKubeletArgs(t, checkPathsFor)
	})
}

// svcRunning returns true if the service with the name svcName is running
func svcRunning(t *testing.T, svcName string) bool {
	state, _, err := getSvcInfo(svcName)
	assert.NoError(t, err)
	return svc.Running == state
}

// svcExists returns true with the service with the name svcName is installed, the state of the service does not matter
func svcExists(t *testing.T, svcName string) bool {
	svcMgr, err := mgr.Connect()
	require.NoError(t, err)
	defer svcMgr.Disconnect()
	mySvc, err := svcMgr.OpenService(svcName)
	if err != nil {
		return false
	} else {
		mySvc.Close()
		return true
	}
}

// getSvcInfo gets the current state and the fully qualified path of the specified service.
// Requires administrator privileges
func getSvcInfo(svcName string) (svc.State, string, error) {
	// State(0) is equivalent to "Stopped"
	state := svc.State(0)
	svcMgr, err := mgr.Connect()
	if err != nil {
		return state, "", fmt.Errorf("could not connect to Windows SCM: %s", err)
	}
	defer svcMgr.Disconnect()
	mySvc, err := svcMgr.OpenService(svcName)
	if err != nil {
		// Could not find the service, so it was never created
		return state, "", err
	}
	defer mySvc.Close()
	// Get state of Service
	status, err := mySvc.Query()
	if err != nil {
		return state, "", err
	}
	// Get fully qualified path of Service
	config, err := mySvc.Config()
	if err != nil {
		return state, "", err
	}
	if config.BinaryPathName != "" {
		return status.State, config.BinaryPathName, nil
	} else {
		return status.State, "", fmt.Errorf("could not fetch %s path: %s", svcName, err)
	}
}

// removeFileIfExists removes the file given by 'path', and will not throw an error if it does not exist
func removeFileIfExists(t *testing.T, path string) {
	err := os.Remove(path)
	if err != nil {
		require.Truef(t, os.IsNotExist(err), "could not remove file %s: %s", path, err)
	}
}

// isKubeletRunning checks if the kubelet was able to start sucessfully
func isKubeletRunning(t *testing.T, logPath string) bool {
	buf, err := ioutil.ReadFile(logPath)
	assert.NoError(t, err)
	return strings.Contains(string(buf), "Started kubelet")
}

// testPathInKubeletArgs checks if the paths given as arguments to kubelet service are correct
// Only checks for paths that are dependent on the underlying OS
func testPathInKubeletArgs(t *testing.T, checkPathsFor []string) {
	// Get fully qualified path for kubelet
	_, path, err := getSvcInfo(bootstrapper.KubeletServiceName)
	require.NoError(t, err, "Could not get kubelet arguments")
	// Split the arguments from kubelet path
	kubeletArg := strings.Split(path, " ")
	for _, arg := range kubeletArg {
		// Split the key and value of arg
		argSplit := strings.SplitN(arg, "=", 2)
		// Ignore single valued arguments
		if len(argSplit) > 1 {
			for _, key := range checkPathsFor {
				if key == argSplit[0] {
					assert.Containsf(t, argSplit[1], string(os.PathSeparator), "Path not correctly set for %s", key)
				}
			}
		}
	}
}
