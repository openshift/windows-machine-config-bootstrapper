package e2e

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-03-01/compute"
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
	winRMProtocol = "TCP"
	// winRmPriority value
	winRMPriority = 300
	// winRM action type
	winRmAction = "Allow"
)

// azureProvider stores Azure clients and resourceGroupName to access the windows node.
type azureProvider struct {
	// resourceGroupName of the Windows node
	resourceGroupName string
	// network security group client to check if winRmHttps port is opened or not.
	nsgClient network.SecurityGroupsClient
	// vm client to check for ansible ping on the windows node.
	vmClient compute.VirtualMachinesClient
}

var (
	// get the azure Auth Credentials specified from the env variable.
	azureCredentials = os.Getenv("AZURE_AUTH_LOCATION")
	// variable to be used for running the tests
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
	var vmClientPtr *compute.VirtualMachinesClient
	var nsgClientPtr *network.SecurityGroupsClient
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
		return fmt.Errorf("failed to get info from AZURE_AUTH_LOCATION with error: %v", err)
	}
	subscriptionId := getFileSettings.GetSubscriptionID()
	if subscriptionId == "" {
		return fmt.Errorf("failed to get the subscriptionId from the AZURE_AUTH_LOCATION")
	}
	vmClient := compute.NewVirtualMachinesClient(subscriptionId)
	vmClientPtr = &vmClient
	if vmClientPtr == nil {
		return fmt.Errorf("failed to initialize the vm client")
	}
	vmClient.Authorizer = resourceAuthorizer
	nsgClient := network.NewSecurityGroupsClient(subscriptionId)
	nsgClientPtr = &nsgClient
	if nsgClientPtr == nil {
		return fmt.Errorf("failed to initialize the network security group client")
	}
	nsgClient.Authorizer = resourceAuthorizer
	azureInfo.resourceGroupName = provider.Azure.ResourceGroupName
	azureInfo.vmClient = vmClient
	azureInfo.nsgClient = nsgClient
	return nil
}

// readInstallerInfo reads the instanceIDs and secGroupsIDs from the
// windows-node-installer.json file specified in "dir".
func readInstallerInfo() (err error) {
	wniFilePath := dir + "/windows-node-installer.json"
	installerInfo, err := resource.ReadInstallerInfo(wniFilePath)
	if err != nil {
		return fmt.Errorf("failed to read installer info %s from ARTIFACT_DIR", dir)
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
	var secRulePropFormat network.SecurityRulePropertiesFormat
	var destPortRange string
	var access network.SecurityRuleAccess
	var protocol network.SecurityRuleProtocol
	var priority int32
	var sourceAddressPrefix string
	for _, secGroupRule := range secGroupRules {
		if isNil(secGroupRule.SecurityRulePropertiesFormat) {
			continue
		}
		secRulePropFormat = *(secGroupRule.SecurityRulePropertiesFormat)
		if isNil(secRulePropFormat.DestinationPortRange) || isNil(secRulePropFormat.Priority) ||
			isNil(secRulePropFormat.SourceAddressPrefix) {
			continue
		}
		destPortRange = *(secRulePropFormat.DestinationPortRange)
		protocol = secRulePropFormat.Protocol
		access = secRulePropFormat.Access
		priority = *(secRulePropFormat.Priority)
		sourceAddressPrefix = *(secRulePropFormat.SourceAddressPrefix)
		if destPortRange == winRM && access == winRmAction && protocol == winRMProtocol &&
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
	var secGroupPropFormat network.SecurityGroupPropertiesFormat
	var secGroupRules []network.SecurityRule
	for _, nsgName := range secGroupsIDs {
		secGroupProfile, err := azureInfo.nsgClient.Get(ctx, azureInfo.resourceGroupName, nsgName, "")
		assert.NoError(t, err, "failed to get the network security group profile")
		assert.NotNil(t, secGroupProfile.SecurityGroupPropertiesFormat, "failed to get the security group properties format")
		secGroupPropFormat = *(secGroupProfile.SecurityGroupPropertiesFormat)
		assert.NotNil(t, secGroupProfile.SecurityRules, "failed to get the security rules list")
		secGroupRules = *(secGroupPropFormat.SecurityRules)
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
	for _, vmName := range instanceIDs {
		vmRDPFilePath := dir + "/" + vmName
		b, err := ioutil.ReadFile(vmRDPFilePath)
		assert.NoError(t, err, "failed to read file %s", vmName)
		// this regex looks for the IP address pattern from vmRDPFilePath.
		// the sample vmRDPFilePath looks like xfreerdp /u:xxxx /v:xx.xx.xx.xx /h:1080 /w:1920 /p:'password1234'
		ipAddressPattern := regexp.MustCompile(`\d+.\d+.\d+.\d+`)
		ipAddress := ipAddressPattern.FindString(string(b))
		// the passwordPattern extracts the characters present after the '/p:', but we are extracting
		// 13 characters even though the windows password length is of size 12 because we got a single quote
		// character included in the password.
		passwordPattern := regexp.MustCompile(`/p:.{13}`)
		password := passwordPattern.FindString(string(b))[3:]
		// we are trimming out the unnecessary single quotes.
		password = strings.Trim(password, `'`)
		hostFileName, err := createHostFile(ipAddress, password)
		assert.NoError(t, err, "failed to create a temp file")
		cmd := exec.Command("ansible", "win", "-i", hostFileName, "-m", "win_ping", "-vvvv")
		out, err := cmd.CombinedOutput()
		assert.NoError(t, err, "failed to execute the ansible ping check %s", string(out))

	}
}
