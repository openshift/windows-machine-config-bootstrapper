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
	winRmPortHttps = "5986"
	// winRm Protocol type
	winRmProtocol = "TCP"
	// winRM action type
	winRmAction = "Allow"
	// winRmPriority value
	winRmPriority = 300
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

// TestWinRMSetup runs two tests i.e checks if the winRMHttps port is opened or not and
// other test does an ansible ping check to confirm that windows node is correctly
// configured to execute the remote ansible commands.
func TestWinRMSetup(t *testing.T) {
	setupAzureClients(t)
	readInstallerInfo(t)
	t.Run("check if WinRmHttps port is opened in the inbound security group rules list", testWinRmPort)
	t.Run("check if ansible is able to ping on the WinRmHttps port", testAnsiblePing)
}

// checkForNil is a helper functions which checks if the object is a nil pointer or not.
func checkForNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr &&
		reflect.ValueOf(v).IsNil())
}

// setupAzureClients basically initializes the azure clients to be used in the tests.
func setupAzureClients(t *testing.T) {
	oc, err := client.GetOpenShift(kubeconfig)
	require.NoError(t, err, "failed to initialize OpenShift client")
	provider, err := oc.GetCloudProvider()
	require.NoError(t, err, "failed to get cloud provider information")
	resourceAuthorizer, err := auth.NewAuthorizerFromFileWithResource(azure.PublicCloud.ResourceManagerEndpoint)
	require.NoError(t, err, "failed to get azure authorization token")
	getFileSettings, err := auth.GetSettingsFromFile()
	require.NoError(t, err, "failed to get info from AZURE_AUTH_LOCATION")
	subscriptionId := getFileSettings.GetSubscriptionID()
	vmClient := compute.NewVirtualMachinesClient(subscriptionId)
	vmClient.Authorizer = resourceAuthorizer
	nsgClient := network.NewSecurityGroupsClient(subscriptionId)
	nsgClient.Authorizer = resourceAuthorizer
	azureInfo.resourceGroupName = provider.Azure.ResourceGroupName
	azureInfo.vmClient = vmClient
	azureInfo.nsgClient = nsgClient
}

// readInstallerInfo reads the instanceIDs and secGroupsIDs from the
// windows-node-installer.json file specified in "dir".
func readInstallerInfo(t *testing.T) {
	filePath := dir + "/windows-node-installer.json"
	installerInfo, err := resource.ReadInstallerInfo(filePath)
	require.NoError(t, err, "failed to read from ARTIFACT_DIR")
	secGroupsIDs = installerInfo.SecurityGroupIDs
	instanceIDs = installerInfo.InstanceIDs
}

// checkForWinRmPort returns a bool response if winRmHttps port is opened on
// windows instance or not.
func checkForWinRmPort(t *testing.T, secGroupRules []network.SecurityRule) bool {
	var secRulePropFormat network.SecurityRulePropertiesFormat
	var destPortRange string
	var access network.SecurityRuleAccess
	var protocol network.SecurityRuleProtocol
	var priority int32
	var sourceAddressPrefix string
	for _, secGroupRule := range secGroupRules {
		if checkForNil(secGroupRule.SecurityRulePropertiesFormat) {
			continue
		}
		secRulePropFormat = *(secGroupRule.SecurityRulePropertiesFormat)
		if checkForNil(secRulePropFormat.DestinationPortRange) && checkForNil(secRulePropFormat.Priority) &&
			checkForNil(secRulePropFormat.SourceAddressPrefix) {
			continue
		}
		destPortRange = *(secRulePropFormat.DestinationPortRange)
		protocol = secRulePropFormat.Protocol
		access = secRulePropFormat.Access
		priority = *(secRulePropFormat.Priority)
		sourceAddressPrefix = *(secRulePropFormat.SourceAddressPrefix)
		if destPortRange != winRmPortHttps && access != winRmAction && protocol != winRmProtocol &&
			priority != winRmPriority && len(sourceAddressPrefix) == 0 {
			continue
		}
		return true
	}
	return false
}

// testWinRmPort checks if winRmHttps port is mentioned in the inbound security
// group rules in the worker subnet.
func testWinRmPort(t *testing.T) {
	ctx := context.Background()
	var secGroupPropFormat network.SecurityGroupPropertiesFormat
	var secGroupRules []network.SecurityRule
	for _, nsgName := range secGroupsIDs {
		secGroupProfile, err := azureInfo.nsgClient.Get(ctx, azureInfo.resourceGroupName, nsgName, "")

		assert.NoError(t, err, "failed to get the network security group profile")
		if !checkForNil(secGroupProfile.SecurityGroupPropertiesFormat) {
			secGroupPropFormat = *(secGroupProfile.SecurityGroupPropertiesFormat)
		} else {
			assert.FailNow(t, "failed to get the security group properties format")
		}
		if !checkForNil(secGroupPropFormat.SecurityRules) {
			secGroupRules = *(secGroupPropFormat.SecurityRules)
		} else {
			assert.FailNow(t, "failed to get the security rules list")
		}
		assert.True(t, checkForWinRmPort(t, secGroupRules), "winRmHttps port is not opened")
	}
}

// createhostFile creates an ansible host file and returns the path of it
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
ansible_port=5986
ansible_connection=winrm
ansible_winrm_server_cert_validation=ignore`, ip, password))
	return hostFile.Name(), err
}

// testAnsiblePing checks if ansible is able to ping on opened winRmHttps port
func testAnsiblePing(t *testing.T) {
	for _, vmName := range instanceIDs {
		filePath := dir + "/" + vmName
		b, err := ioutil.ReadFile(filePath)
		assert.NoError(t, err, "failed to read file %s", vmName)
		re := regexp.MustCompile(`\d+.\d+.\d+.\d+`)
		ipAddress := re.FindString(string(b))
		re = regexp.MustCompile(`/p:.{13}`)
		password := re.FindString(string(b))[3:]
		password = strings.Trim(password, `'`)
		t.Logf("password is %s, the ip is %s", password, ipAddress)

		hostFileName, err := createHostFile(ipAddress, password)
		t.Logf("password is %s,", hostFileName)
		assert.NoError(t, err, "failed to create a temp file")
		cmd := exec.Command("ansible", "win", "-i", hostFileName, "-m", "win_ping", "-vvvv")
		out, err := cmd.CombinedOutput()
		assert.NoError(t, err, "failed to execute the ansible ping check %s", string(out))

	}
}
