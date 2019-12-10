package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	wniAzure "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/azure"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
)

const (
	// winRMPort is the secure WinRM port
	winRMPort = "5986"
	// winRMPortPriority is the priority for the WinRM rule
	winRMPortPriority = 600
	// winRMRuleName security group rule name for the WinRM rule
	winRMRuleName = "WinRM"
	// rdpPort is the RDP port
	rdpPort = "3389"
	// rdpRulePriority is the priority for the RDP rule
	rdpRulePriority = 601
	// rdpRuleName is the security group rule name for the RDP rule
	rdpRuleName = "RDP"
	// vnetPorts is the port range for vnet rule
	vnetPorts = "1-65535"
	// vnetRulePriority is the priority for the vnet traffic rule
	vnetRulePriority = 602
	// vnetRuleName is the security group rule name for vnet traffic within the cluster
	vnetRuleName = "vnet_traffic"
	// ruleProtocol is the default protocol for all rules
	ruleProtocol = "Tcp"
	// ruleAction is the default actions for all rules
	ruleAction = "Allow"
)

type requiredRule struct {
	// name is the required name of the security rule
	name string
	// sourceAddress is the required source address in the security rule
	sourceAddress *string
	// destinationPortRange are the required destination ports of the rule
	destinationPortRange string
	// priority is the rules required priority in the NSG
	priority int32
	// present indicates that the rule was present as expected in a security group
	present bool
}

// azureProvider stores Azure clients and resourceGroupName to access the windows node.
type azureProvider struct {
	// resourceGroupName of the Windows node
	resourceGroupName string
	// nsgClient to check if winRmHttps port is opened or not.
	nsgClient network.SecurityGroupsClient
	// requiredRules is the set of SG rules that need to be created or deleted
	requiredRules map[string]*requiredRule
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

// TestCreateVM is used to the test the following after a successful run of "wni azure create"
// 1. check if required rules are present
// 2. ansible ping check to confirm that windows node is correctly
//    configured to execute the remote ansible commands.
func TestCreateVM(t *testing.T) {
	t.Run("check if required security rules are present", testRequiredRules)
	t.Run("check if ansible is able to ping on the WinRmHttps port", testAnsiblePing)
}

// isNil is a helper functions which checks if the object is a nil pointer or not.
func isNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr &&
		reflect.ValueOf(v).IsNil())
}

// constructRequiredRules populates the required rules map
func constructRequiredRules() (map[string]*requiredRule,
	error) {
	myIP, err := wniAzure.GetMyIP()
	if err != nil {
		return nil, fmt.Errorf("unable to get public IP address: %v", err)
	}

	requiredRules := make(map[string]*requiredRule)
	requiredRules[rdpRuleName] = &requiredRule{rdpRuleName, myIP, rdpPort, rdpRulePriority, false}
	requiredRules[winRMRuleName] = &requiredRule{winRMRuleName, myIP, winRMPort, winRMPortPriority, false}
	requiredRules[vnetRuleName] = &requiredRule{vnetRuleName, to.StringPtr("10.0.0.0/16"), vnetPorts,
		vnetRulePriority, false}
	return requiredRules, nil
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

	requiredRules, err := constructRequiredRules()
	if err != nil {
		return fmt.Errorf("unable to construct required rules: %v", err)
	}
	azureInfo.requiredRules = requiredRules

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

// areRequiredRulesPresent returns true if all the required rules are present in the SecurityRule slice
func areRequiredRulesPresent(secGroupRules []network.SecurityRule) bool {
	for _, secGroupRule := range secGroupRules {
		if isNil(secGroupRule.Name) {
			continue
		}
		reqRule, found := azureInfo.requiredRules[*secGroupRule.Name]
		if !found {
			continue
		}

		if isNil(secGroupRule.SecurityRulePropertiesFormat) {
			continue
		}
		secRulePropFormat := *(secGroupRule.SecurityRulePropertiesFormat)
		if isNil(secRulePropFormat.DestinationPortRange) || isNil(secRulePropFormat.Priority) ||
			isNil(secRulePropFormat.SourceAddressPrefixes) {
			continue
		}
		destPortRange := *(secRulePropFormat.DestinationPortRange)
		protocol := secRulePropFormat.Protocol
		access := secRulePropFormat.Access
		priority := *(secRulePropFormat.Priority)
		if destPortRange == reqRule.destinationPortRange && access == ruleAction && protocol == ruleProtocol &&
			priority == reqRule.priority {
			sourceAddressIsPresent := false
			for _, sourceAddress := range *secRulePropFormat.SourceAddressPrefixes {
				if sourceAddress == *reqRule.sourceAddress {
					sourceAddressIsPresent = true
				}
			}
			if sourceAddressIsPresent {
				reqRule.present = true
			}
		}
	}

	// Check if all the required rules are present. Return false on the first instance a rule is not present
	for _, reqRule := range azureInfo.requiredRules {
		if !reqRule.present {
			return false
		}
	}
	return true
}

// testRequiredRules checks if all the required rules are present in all the NSGs in the win
func testRequiredRules(t *testing.T) {
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
		assert.True(t, areRequiredRulesPresent(secGroupRules), "required rules are not present")

		// reset the presence of all the required rules
		for _, reqRule := range azureInfo.requiredRules {
			reqRule.present = false
		}
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
ansible_winrm_server_cert_validation=ignore`, ip, password, winRMPort))
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
		require.NoErrorf(t, err, "failed to read file %s", vmName)
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
		assert.NoErrorf(t, err, "ansible ping check failed with error: %s", string(out))
	}
}
