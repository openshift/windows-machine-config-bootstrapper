package e2e

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
)

const (
	// winRm Https port for the Windows node.
	winRM = "5986"
	// winRm protocol type.
	winRMProtocol = "Tcp"
	// winRmPriority value
	winRMPriority = 300
	// winRM action type
	winRMAction = "Allow"
)

// azureProvider stores Azure clients and resourceGroupName to access the windows node.
type azureProvider struct {
	// resourceGroupName of the Windows node
	resourceGroupName string
	// nsgClient to check if winRmHttps port is opened or not.
	nsgClient network.SecurityGroupsClient
}

var (
	// azureCredentials is the location of the env variable "AZURE_AUTH_LOCATION".
	azureCredentials = os.Getenv("AZURE_AUTH_LOCATION")
	// azureInfo initializes the azureProvider type, holds the info that will be used in the tests.
	azureInfo = azureProvider{}
	// instanceIDs that are obtained from the windows-node-installer.json
	instanceIDs []string
	// secGroupIDs that are obtained from the windows-node-installer.json
	secGroupsIDs []string
)

// TestWinRMSetup runs two tests i.e
// 1. checks if the winRMHttps port is opened or not.
// 2. ansible ping check to confirm that windows node is correctly
//    configured to execute the remote ansible commands.
func TestWinRMSetup(t *testing.T) {
	t.Run("check if WinRmHttps port is opened in the inbound security group rules list", testWinRmPort)
	t.Run("check if ansible is able to ping on the WinRmHttps port", testAnsiblePing)
}

// isNil is a helper functions which checks if the object is a nil pointer or not.
func isNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr &&
		reflect.ValueOf(v).IsNil())
}

// setup initializes the azureProvider to be used for running the tests.
func setup() (err error) {
	oc, err := client.GetOpenShift(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to initialize OpenShift client with error: %v", err)
	}
	provider, err := oc.GetCloudProvider()
	if err != nil {
		return fmt.Errorf("failed to get cloud provider information with error: %v", err)
	}
	resourceAuthorizer, err := auth.NewAuthorizerFromFileWithResource(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to get azure authorization token with error: %v", err)
	}
	getFileSettings, err := auth.GetSettingsFromFile()
	if err != nil {
		return fmt.Errorf("failed to get info from %s with error: %v", azureCredentials, err)
	}
	subscriptionId := getFileSettings.GetSubscriptionID()
	if subscriptionId == "" {
		return fmt.Errorf("failed to get the subscriptionId from AZURE_AUTH_LOCATION: %s", azureCredentials)
	}
	nsgClient := network.NewSecurityGroupsClient(subscriptionId)
	nsgClient.Authorizer = resourceAuthorizer
	azureInfo.resourceGroupName = provider.Azure.ResourceGroupName
	azureInfo.nsgClient = nsgClient
	return nil
}

// readInstallerInfo reads the instanceIDs and secGroupsIDs from the
// windows-node-installer.json file specified in "dir".
func readInstallerInfo() (err error) {
	wniFilePath := filepath.Join(dir, "/windows-node-installer.json")
	installerInfo, err := resource.ReadInstallerInfo(wniFilePath)
	if err != nil {
		return fmt.Errorf("failed to read installer info from %s with error: %v", dir, err)
	}
	if len(installerInfo.SecurityGroupIDs) == 0 {
		return fmt.Errorf("failed to obtain the sec group Ids")
	}
	secGroupsIDs = installerInfo.SecurityGroupIDs
	if len(installerInfo.InstanceIDs) == 0 {
		return fmt.Errorf("failed to obtain the instance Ids")
	}
	instanceIDs = installerInfo.InstanceIDs
	return nil
}

// isWinRMPortOpen returns a bool response if winRmHttps port is opened on
// windows instance or not.
func isWinRMPortOpen(secGroupRules []network.SecurityRule) bool {
	for _, secGroupRule := range secGroupRules {
		if isNil(secGroupRule.SecurityRulePropertiesFormat) {
			continue
		}
		secRulePropFormat := *(secGroupRule.SecurityRulePropertiesFormat)
		if isNil(secRulePropFormat.DestinationPortRange) || isNil(secRulePropFormat.Priority) ||
			isNil(secRulePropFormat.SourceAddressPrefix) {
			continue
		}
		destPortRange := *(secRulePropFormat.DestinationPortRange)
		protocol := secRulePropFormat.Protocol
		access := secRulePropFormat.Access
		priority := *(secRulePropFormat.Priority)
		sourceAddressPrefix := *(secRulePropFormat.SourceAddressPrefix)
		if destPortRange == winRM && access == winRMAction && protocol == winRMProtocol &&
			priority == winRMPriority && len(sourceAddressPrefix) != 0 {
			return true
		}
	}
	return false
}

// testWinRmPort checks if winRmHttps port is mentioned in the inbound security
// group rules in the worker subnet.
func testWinRmPort(t *testing.T) {
	ctx := context.Background()
	err := setup()
	require.NoError(t, err, "failed to initialize azureProvider")
	err = readInstallerInfo()
	require.NoError(t, err, "failed to get info from wni file")
	for _, nsgName := range secGroupsIDs {
		secGroupProfile, err := azureInfo.nsgClient.Get(ctx, azureInfo.resourceGroupName, nsgName, "")
		require.NoError(t, err, "failed to get the network security group profile")
		require.NotEmpty(t, secGroupProfile.SecurityGroupPropertiesFormat, "failed to get the security group properties format")
		secGroupPropFormat := *(secGroupProfile.SecurityGroupPropertiesFormat)
		require.NotEmpty(t, secGroupProfile.SecurityRules, "failed to get the security rules list")
		secGroupRules := *(secGroupPropFormat.SecurityRules)
		assert.True(t, isWinRMPortOpen(secGroupRules), "winRmHttps port is not opened")
	}
}

// createHostFile creates an ansible host file and returns the path of it
func createHostFile(ip, password string) (string, error) {
	hostFile, err := ioutil.TempFile("", "test")
	if err != nil {
		return "", fmt.Errorf("coud not make temporary file: %s", err)
	}
	defer hostFile.Close()

	_, err = hostFile.WriteString(fmt.Sprintf(`[win]
%s ansible_password=%s
[win:vars]
ansible_user=core
ansible_port=%s
ansible_connection=winrm
ansible_winrm_server_cert_validation=ignore`, ip, password, winRM))
	return hostFile.Name(), err
}

// testAnsiblePing checks if ansible is able to ping on opened winRmHttps port
func testAnsiblePing(t *testing.T) {
	// this regex looks for the IP address pattern from vmCredentialPath.
	// the sample vmCredentialPath looks like xfreerdp /u:xxxx /v:xx.xx.xx.xx /h:1080 /w:1920 /p:'password1234'
	ipAddressPattern := regexp.MustCompile(`\d+.\d+.\d+.\d+`)
	// the passwordPattern extracts the characters present after the '/p:', but we are extracting
	// 13 characters even though the windows password length is of size 12 because we got a single quote
	// character included in the password.
	passwordPattern := regexp.MustCompile(`/p:.{13}`)
	for _, vmName := range instanceIDs {
		vmCredentialPath := filepath.Join(dir, "/", vmName)
		rdpCmd, err := ioutil.ReadFile(vmCredentialPath)
		require.NoError(t, err, "failed to read file %s", vmName)
		ipAddress := ipAddressPattern.FindString(string(rdpCmd))
		assert.NotEmpty(t, ipAddress, "the IP address can't be empty")
		password := passwordPattern.FindString(string(rdpCmd))[3:]
		assert.NotEmpty(t, password, "the password can't be empty")
		// we are trimming out the unnecessary single quotes.
		password = strings.Trim(password, `'`)
		hostFileName, err := createHostFile(ipAddress, password)
		require.NoError(t, err, "failed to create a temp file")
		pingCmd := exec.Command("ansible", "win", "-i", hostFileName, "-m", "win_ping")
		out, err := pingCmd.CombinedOutput()
		assert.NoError(t, err, "ansible ping check failed with error: %s", string(out))
	}
}
