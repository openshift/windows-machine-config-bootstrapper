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
	"testing"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/client"
	wniAzure "github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/cloudprovider/azure"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/resource"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
)

const (
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
	// azureUser used to access the windows instance
	azureUser = "core"
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
	// ipAddressPattern looks for the IP address from vm rdp command file.
	ipAddressPattern = regexp.MustCompile(`\d+.\d+.\d+.\d+`)
	// passwordPattern looks for the password from vm rdp command file.
	passwordPattern = regexp.MustCompile(`/p:'.{12}'`)
	// credentials holds the credentials associated with the Windows VM.
	credentials *types.Credentials
)

// TestCreateVM is used to the test the following after a successful run of "wni azure create"
// 1. check if required rules are present
// 2. ansible ping check to confirm that windows node is correctly
//    configured to execute the remote ansible commands.
// TODO: This is not actually testing the Windows VM creation. Change this function.
func TestCreateVM(t *testing.T) {
	err := setup()
	require.NoErrorf(t, err, "failed at the setup with error: %v", err)
	t.Run("check if required security rules are present", testRequiredRules)
	t.Run("check if ansible is able to ping on the WinRmHttps port", testAnsiblePing)
	t.Run("check if container logs port is open in Windows firewall", testAzureInstancesFirewallRule)
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

// setup does these prerequisite tests before running the tests.
// 1. populate fields into the azureInfo
// 2. populate global variables instanceIDs and secGroupsIDs
// 3. get credential object for the instance created.
func setup() error {
	err := populateAzureInfo()
	if err != nil {
		return fmt.Errorf("failed to populate Azure Info with error: %v", err)
	}
	err = populateInstAndSgIds()
	if err != nil {
		return fmt.Errorf("failed to populate the Instance and security group Id's: %v", err)
	}
	err = getCredentials()
	if err != nil {
		return fmt.Errorf("failed to get credential object with error: %v", err)
	}
	return nil
}

// populateAzureInfo populates the fields present in the azureInfo.
func populateAzureInfo() error {
	oc, err := client.GetOpenShift(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to initialize OpenShift client with error: %v", err)
	}

	provider, err := oc.GetCloudProvider()
	if err != nil {
		return fmt.Errorf("failed to get cloud provider information with error: %v", err)
	}
	azureInfo.resourceGroupName = provider.Azure.ResourceGroupName

	// credentialFileData contains mapping of data present in the azure credential file.
	credentialFileData, err := auth.GetSettingsFromFile()
	if err != nil {
		return fmt.Errorf("failed to get info from %s with error: %v", azureCredentials, err)
	}

	subscriptionId := credentialFileData.GetSubscriptionID()
	if subscriptionId == "" {
		return fmt.Errorf("failed to get the subscriptionId from AZURE_AUTH_LOCATION: %s", azureCredentials)
	}

	// instantiate network security group client.
	azureInfo.nsgClient = network.NewSecurityGroupsClient(subscriptionId)

	// set authorisation token for network security group client.
	resourceAuthorizer, err := auth.NewAuthorizerFromFileWithResource(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to get azure authorization token with error: %v", err)
	}
	azureInfo.nsgClient.Authorizer = resourceAuthorizer

	requiredRules, err := constructRequiredRules()
	if err != nil {
		return fmt.Errorf("failed to construct required rules with error: %v", err)
	}
	azureInfo.requiredRules = requiredRules
	return nil
}

// populateInstAndSgIds populates instanceIDs and secGroupsIDs for the instance created.
func populateInstAndSgIds() error {
	err := readInstallerInfo()
	if err != nil {
		return fmt.Errorf("failed to get info from windows-node-installer.json with error: %v", err)
	}
	return nil
}

// getCredentials gets the credential object for the created instance.
func getCredentials() error {
	// currently we are testing properties of a single instance, which is going to be
	// zeroth entry in it.
	instanceId := instanceIDs[0]
	ipAddress, password, err := getIpPass(instanceId)
	if err != nil {
		return fmt.Errorf("failed to get the Ip address and password for the instance with error: %v", err)
	}
	if ipAddress == "" {
		return fmt.Errorf("failed to capture Ip address")
	}
	if password == "" {
		return fmt.Errorf("failed to capture the password")
	}
	credentials = types.NewCredentials(instanceId, ipAddress, password, azureUser)
	return nil
}

// readInstallerInfo reads the instanceIDs and secGroupsIDs from the
// windows-node-installer.json file specified in "artifactDir".
func readInstallerInfo() (err error) {
	wniFilePath := filepath.Join(artifactDir, "/windows-node-installer.json")
	installerInfo, err := resource.ReadInstallerInfo(wniFilePath)
	if err != nil {
		return fmt.Errorf("failed to read installer info from %s with error: %v", artifactDir, err)
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

// getIpPass extracts instance IPAddress and password from instance rdp file.
func getIpPass(vmName string) (string, string, error) {
	vmCredentialPath := filepath.Join(artifactDir, "/", vmName)
	rdpCmd, err := ioutil.ReadFile(vmCredentialPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read %s credentials with error: %v", vmName, err)
	}

	// this regex looks for the IP address pattern from vmCredentialPath.
	// the sample vmCredentialPath looks like xfreerdp /u:xxxx /v:12.23.34.45 /h:1080 /w:1920 /p:'password1234'
	// ipAddress will capture IPaddress 12.23.34.45
	ipAddress := ipAddressPattern.FindString(string(rdpCmd))

	// passwordPattern will extract /p:'password1234'. In that we require password1234
	password := passwordPattern.FindString(string(rdpCmd))[4:16]

	return ipAddress, password, nil
}

// testRequiredRules checks if all the required rules are present in all the NSGs in the win
func testRequiredRules(t *testing.T) {
	ctx := context.Background()
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
	// TODO: Do not iterate on the instances but instead pass the credentials object
	//  for test function
	for _, vmName := range instanceIDs {
		ipAddress, password, err := getIpPass(vmName)
		require.NoErrorf(t, err, "failed to read file %s", vmName)
		assert.NotEmpty(t, ipAddress, "the IP address can't be empty")
		assert.NotEmpty(t, password, "the password can't be empty")
		hostFileName, err := createHostFile(ipAddress, password)
		require.NoError(t, err, "failed to create a temp file")
		pingCmd := exec.Command("ansible", "win", "-i", hostFileName, "-m", "win_ping")
		out, err := pingCmd.CombinedOutput()
		assert.NoErrorf(t, err, "ansible ping check failed with error: %s", string(out))
	}
}

// testAzureFirewallRule asserts if the created instance has firewall rule that opens container logs port.
func testAzureInstancesFirewallRule(t *testing.T) {
	ipAddress, password, err := getIpPass(credentials.GetInstanceId())
	require.NoErrorf(t, err, "failed to obtain credentials for %s", credentials.GetInstanceId())
	credentials = types.NewCredentials(credentials.GetInstanceId(), ipAddress, password, azureUser)
	testInstanceFirewallRule(t)
}
