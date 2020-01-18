package e2e

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"

	"github.com/masterzen/winrm"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"github.com/stretchr/testify/assert"
)

const (
	// winRMPort is the secure WinRM port
	winRMPort = "5986"
)

var (
	// credentials store the windows instance credential information
	credentials *types.Credentials
	// retryCount is the number of times we hit a request.
	retryCount = 30
	// retryInterval is interval of time until we retry after a failure.
	retryInterval = 5 * time.Second
)

// setupWinRMClient initializes the winRM client and executes commands on the windows node
func setupWinRMClient(host, password, user string) (*winrm.Client, error) {
	winRMPortNo, err := strconv.Atoi(winRMPort)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to an integer with error: %v", err)
	}
	endpoint := winrm.NewEndpoint(host, winRMPortNo, true, true,
		nil, nil, nil, time.Minute*1)
	winRMClient, err := winrm.NewClient(endpoint, user, password)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize winRM client with error: %v", err)
	}
	return winRMClient, nil
}

// runWinRMCmd executes a winRM command on the windows node.
func runWinRMCmd(winRMClient *winrm.Client, command string) (string, string, error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exitCode, err := winRMClient.Run(command, stdout, stderr)
	if err != nil {
		return "", "", fmt.Errorf("failed while executing the %s remotely with error: %v", command, err)
	}
	if exitCode != 0 {
		return stdout.String(), stderr.String(), fmt.Errorf("%s returned %d exit code", command, exitCode)
	}
	return stdout.String(), stderr.String(), nil
}

// checkForFirewallRule waits until it is able to locate firewall rule on windows node
// for retryCount attempts
func checkForFirewallRule(winRMClient *winrm.Client) (stderr string, err error) {
	// winFirewallCmd will verify if firewall rule is present on the windows node
	winFirewallCmd := "powershell.exe Get-NetFirewallRule -DisplayName ContainerLogsPort"
	for i := 0; i < retryCount; i++ {
		_, stderr, err = runWinRMCmd(winRMClient, winFirewallCmd)
		time.Sleep(retryInterval)
		if stderr == "" {
			return
		}
	}
	return
}

// testInstanceFirewallRule checks if the created instance has opened container logs port via firewall
func testInstanceFirewallRule(t *testing.T) {
	// TODO: Instantiating winRM client should be done in the setup function and client should become
	//  part of test framework.
	winRMClient, err := setupWinRMClient(credentials.GetIPAddress(), credentials.GetPassword(),
		credentials.GetUserName())
	require.NoErrorf(t, err, "winRM client setup failed with error: %v", err)
	stderr, err := checkForFirewallRule(winRMClient)
	assert.Equalf(t, "", stderr, "firewall rule command failed with error: %s", stderr)
	assert.NoError(t, err, "failed to test the firewall rule on the windows node")
}
