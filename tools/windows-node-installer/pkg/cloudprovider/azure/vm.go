package azure

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"io/ioutil"
	"math/rand"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-03-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logger.Log.WithName("azure-vm")
var windowsWorker string = "winworker-"

// AzureProvider holds azure platform specific information required for creating/deleting
// the windows node.
type AzureProvider struct {
	// vnetClient to get Virtual Network related info.
	vnetClient network.VirtualNetworksClient
	// vmClient to query for instance related operations.
	vmClient compute.VirtualMachinesClient
	// ipClient to query for IP related operations.
	ipClient network.PublicIPAddressesClient
	// subnetsClient to query for subnet related operations.
	subnetsClient network.SubnetsClient
	// nicClient to query for nic related operations.
	nicClient network.InterfacesClient
	// nsgClient to query for security groups related operations.
	nsgClient network.SecurityGroupsClient
	// diskClient to query for disk related operations.
	diskClient compute.DisksClient
	// a request authorization token to supply for clients
	authorizer autorest.Authorizer
	// resourceGroupName of the existing openshift cluster.
	resourceGroupName string
	// subscriptionID of the corresponding azure service principal.
	subscriptionID string
	// infraID is the name of existing openshift infrastructure.
	infraID string
	// IPName is the resource name provided by the user if the installer doesn't want to create one.
	IpName string
	// NicName is the resource name provided by the user if the installer doesn't want to create one.
	NicName string
	// NsgName is the resource name provided by the user if the installer doesn't want to create one.
	NsgName string
	// imageID of the instance to be created.
	imageID string
	// instanceType aka instance flavor.
	instanceType string
	// workspace to store all the results.
	resourceTrackerDir string
}

// New returns azure interface for performing necessary functions related to creating or
// destroying an instance.
// It takes in kubeconfig of an existing OpenShift cluster and an azure specific credential file.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func New(openShiftClient *client.OpenShift, credentialPath, subscriptionID,
	resourceTrackerDir, imageID, instanceType string) (*AzureProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if errorCheck(err) {
		return nil, err
	}

	infraID, _ := openShiftClient.GetInfrastructureID()
	resourceAuthorizer, err := auth.NewAuthorizerFromFileWithResource(azure.PublicCloud.ResourceManagerEndpoint)
	if errorCheck(err) {
		return nil, err
	}
	resourceGroupName := provider.Azure.ResourceGroupName
	vnetClient := getVnetClient(resourceAuthorizer, subscriptionID)
	vmClient := getVMClient(resourceAuthorizer, subscriptionID)
	ipClient := getIPClient(resourceAuthorizer, subscriptionID)
	subnetClient := getSubnetsClient(resourceAuthorizer, subscriptionID)
	nicClient := getNicClient(resourceAuthorizer, subscriptionID)
	nsgClient := getNsgClient(resourceAuthorizer, subscriptionID)
	diskClient := getDiskClient(resourceAuthorizer, subscriptionID)
	var IpName, NicName, NsgName string

	return &AzureProvider{vnetClient, vmClient, ipClient,
		subnetClient, nicClient, nsgClient, diskClient, resourceAuthorizer,
		resourceGroupName, subscriptionID, infraID, IpName, NicName, NsgName,
		imageID, instanceType, resourceTrackerDir}, nil
}

// getVnetClient gets the Networking Client by passing the authorizer token.
func getVnetClient(authorizer autorest.Authorizer, subscriptionID string) network.VirtualNetworksClient {
	vnetClient := network.NewVirtualNetworksClient(subscriptionID)
	vnetClient.Authorizer = authorizer
	return vnetClient
}

// getVMClient gets the Virtual Machine Client by passing the authorizer token.
func getVMClient(authorizer autorest.Authorizer, subscriptionID string) compute.VirtualMachinesClient {
	vmClient := compute.NewVirtualMachinesClient(subscriptionID)
	vmClient.Authorizer = authorizer
	return vmClient
}

// getIPClient gets the IP Client by passing the authorizer token.
func getIPClient(authorizer autorest.Authorizer, subscriptionID string) network.PublicIPAddressesClient {
	ipClient := network.NewPublicIPAddressesClient(subscriptionID)
	ipClient.Authorizer = authorizer
	return ipClient
}

// getSubnetsClient gets the Subnet Client by passing the authorizer token.
func getSubnetsClient(authorizer autorest.Authorizer, subscriptionID string) network.SubnetsClient {
	subnetsClient := network.NewSubnetsClient(subscriptionID)
	subnetsClient.Authorizer = authorizer
	return subnetsClient
}

// getNicClient gets the NIC Client by passing the authorizer token.
func getNicClient(authorizer autorest.Authorizer, subscriptionID string) network.InterfacesClient {
	nicClient := network.NewInterfacesClient(subscriptionID)
	nicClient.Authorizer = authorizer
	return nicClient
}

// getNsgClient gets the network security group by passing the authorizer token.
func getNsgClient(authorizer autorest.Authorizer, subscriptionID string) network.SecurityGroupsClient {
	nsgClient := network.NewSecurityGroupsClient(subscriptionID)
	nsgClient.Authorizer = authorizer
	return nsgClient
}

// getDiskClient gets the disk client by passing the authorizer token.
func getDiskClient(authorizer autorest.Authorizer, subscriptionID string) compute.DisksClient {
	diskClient := compute.NewDisksClient(subscriptionID)
	diskClient.Authorizer = authorizer
	return diskClient
}

// errorCheck checks if there exists an error and returns a bool response
func errorCheck(err error) bool {
	if err != nil {
		return true
	} else {
		return false
	}
}

// checkForNil checks if the object is present or not
func checkForNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

// findIP checks for the IP address pattern.
func findIP(input string) string {
	numBlock := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])"
	regexPattern := numBlock + "\\." + numBlock + "\\." + numBlock + "\\." + numBlock
	regEx := regexp.MustCompile(regexPattern)
	return regEx.FindString(input)
}

// getMyIP returns the IP address string of the user
// by talking to https://checkip.azurewebsites.net/
func getMyIP() (ip *string, err error) {
	resp, err := http.Get("https://checkip.azurewebsites.net/")
	if errorCheck(err) {
		return nil, err
	}

	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	if errorCheck(err) {
		return nil, err
	}
	result := findIP(string(contents))
	return &result, nil
}

// getvnetProfile gets the vnet Profile of the existing openshift cluster.
// there exists a single vnet in the openshift cluster.
func (az *AzureProvider) getvnetProfile(ctx context.Context) (vnetProfile *network.VirtualNetwork, err error) {
	vnetList, err := az.vnetClient.List(ctx, az.resourceGroupName)
	if errorCheck(err) {
		return nil, err
	}
	vnetListValues := vnetList.Values()
	if len(vnetListValues) > 0 {
		vnetProfile = &vnetListValues[0]
	}
	return
}

// getvnetLocation returns the location of the vnet of the existing openshift cluster.
func (az *AzureProvider) getvnetLocation(ctx context.Context) (location *string) {
	vnetProfile, err := az.getvnetProfile(ctx)
	if errorCheck(err) {
		return nil
	}
	location = vnetProfile.Location
	return
}

// extractResourceName captures the resource name omitting the other details.
// for ex: /subscriptions/.../resourcegroups/ExampleResourceGroup?api-version=2016-02-01/vnetName/somesamplevnetName
// we need to extract the vnetName from the above input.
func extractResourceName(rawresource string) (name string) {
	resultList := strings.Split(rawresource, "/")
	arrayLength := len(resultList)
	name = resultList[arrayLength-1]
	return
}

// createPublicIP creates the public IP for the instance
func (az *AzureProvider) createPublicIP(ctx context.Context) (ip *network.PublicIPAddress, err error) {
	var nodeLocation string
	if !checkForNil(az.getvnetLocation(ctx)) {
		nodeLocation = *(az.getvnetLocation(ctx))
	} else {
		return nil, fmt.Errorf("cannot get location of the openshift cluster: %v", err)
	}
	future, err := az.ipClient.CreateOrUpdate(
		ctx,
		az.resourceGroupName,
		az.IpName,
		network.PublicIPAddress{
			Name:     to.StringPtr(az.IpName),
			Location: to.StringPtr(nodeLocation),
			PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
				PublicIPAddressVersion:   network.IPv4,
				PublicIPAllocationMethod: network.Static,
			},
		},
	)

	if errorCheck(err) {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.ipClient.Client)
	if errorCheck(err) {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	ip_info, err := future.Result(az.ipClient)
	return &ip_info, err
}

// createNIC creates the nic for the instance
func (az *AzureProvider) createNIC(ctx context.Context, vnetName, subnetName, nsgName,
	ipConfigName string) (nic *network.Interface, err error) {
	var nodeLocation string
	if !checkForNil(az.getvnetLocation(ctx)) {
		nodeLocation = *(az.getvnetLocation(ctx))
	} else {
		return nil, fmt.Errorf("cannot get location of the openshift cluster: %v", err)
	}
	subnet, err := az.subnetsClient.Get(ctx, az.resourceGroupName, vnetName, subnetName, "")
	if errorCheck(err) {
		fmt.Errorf("failed to get subnet: %v", err)
	}

	ip, err := az.ipClient.Get(ctx, az.resourceGroupName, az.IpName, "")

	if errorCheck(err) {
		fmt.Errorf("failed to get ip address: %v", err)
	}

	nicParams := network.Interface{
		Name:     to.StringPtr(az.NicName),
		Location: to.StringPtr(nodeLocation),
		InterfacePropertiesFormat: &network.InterfacePropertiesFormat{
			IPConfigurations: &[]network.InterfaceIPConfiguration{
				{
					Name: to.StringPtr(ipConfigName),
					InterfaceIPConfigurationPropertiesFormat: &network.InterfaceIPConfigurationPropertiesFormat{
						Subnet:                    &subnet,
						PrivateIPAllocationMethod: network.Dynamic,
						PublicIPAddress:           &ip,
					},
				},
			},
		},
	}

	if nsgName != "" {
		nsg, err := az.nsgClient.Get(ctx, az.resourceGroupName, nsgName, "")
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to get network security group rules: %v", err)
		}
		nicParams.NetworkSecurityGroup = &nsg
	}

	future, err := az.nicClient.CreateOrUpdate(ctx, az.resourceGroupName, az.NicName, nicParams)
	if errorCheck(err) {
		return nil, fmt.Errorf("cannot create or update nic: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.nicClient.Client)
	if err != nil {
		return nic, fmt.Errorf("cannot create or update nic: %v", err)
	}

	nic_info, err := future.Result(az.nicClient)
	return &nic_info, err
}

// createSecurityGroupRules tries to create new security group rules that allows to RDP to the windows node and allow for all the traffic
// coming from the nodes that belong to the same VNET.
func (az *AzureProvider) createSecurityGroupRules(ctx context.Context, nsgName string) (err error) {
	var nodeLocation string
	if !checkForNil(az.getvnetLocation(ctx)) {
		nodeLocation = *(az.getvnetLocation(ctx))
	} else {
		return fmt.Errorf("cannot get location of the openshift cluster: %v", err)
	}
	myIP, err := getMyIP()
	if errorCheck(err) {
		return err
	}
	future, err := az.nsgClient.CreateOrUpdate(
		ctx,
		az.resourceGroupName,
		nsgName,
		network.SecurityGroup{
			Location: to.StringPtr(nodeLocation),
			SecurityGroupPropertiesFormat: &network.SecurityGroupPropertiesFormat{
				SecurityRules: &[]network.SecurityRule{
					{
						Name: to.StringPtr("RDP"),
						SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
							Protocol:                 network.SecurityRuleProtocolTCP,
							SourceAddressPrefix:      to.StringPtr(*myIP + "/32"),
							SourcePortRange:          to.StringPtr("*"),
							DestinationAddressPrefix: to.StringPtr("*"),
							DestinationPortRange:     to.StringPtr("3389"),
							Access:                   network.SecurityRuleAccessAllow,
							Direction:                network.SecurityRuleDirectionInbound,
							Priority:                 to.Int32Ptr(100),
						},
					},
					{
						Name: to.StringPtr("vnet_traffic"),
						SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
							Protocol:                 network.SecurityRuleProtocolTCP,
							SourceAddressPrefix:      to.StringPtr("10.0.0.0/16"),
							SourcePortRange:          to.StringPtr("*"),
							DestinationAddressPrefix: to.StringPtr("*"),
							DestinationPortRanges:    &[]string{"1-65535"},
							Access:                   network.SecurityRuleAccessAllow,
							Direction:                network.SecurityRuleDirectionInbound,
							Priority:                 to.Int32Ptr(200),
						},
					},
					{
						Name: to.StringPtr("WINRM"),
						SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
							Protocol:                 network.SecurityRuleProtocolTCP,
							SourceAddressPrefix:      to.StringPtr(*myIP + "/32"),
							SourcePortRange:          to.StringPtr("*"),
							DestinationAddressPrefix: to.StringPtr("*"),
							DestinationPortRange:     to.StringPtr("5986"),
							Access:                   network.SecurityRuleAccessAllow,
							Direction:                network.SecurityRuleDirectionInbound,
							Priority:                 to.Int32Ptr(300),
						},
					},
				},
			},
		},
	)
	if err != nil {
		return fmt.Errorf("cannot create or update security group rules of worker subnet: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.nsgClient.Client)
	if err != nil {
		return fmt.Errorf("cannot create or update security group rules of worker subnet: %v", err)
	}

	return nil
}

// constructStorageProfile constructs the Storage Profile for the creation of windows instance.
// The Storage Profile consists of the image reference details such as which instance type, version etc.
// imageId format: Publisher:Offer:Sku:Version ex: MicrosoftWindowsServer:WindowsServer:2019-Datacenter:latest
func (az *AzureProvider) constructStorageProfile(imageId string) (storageProfile *compute.StorageProfile) {
	stringSplit := strings.Split(imageId, ":")

	storageProfile = &compute.StorageProfile{
		ImageReference: &compute.ImageReference{
			Publisher: to.StringPtr(stringSplit[0]),
			Offer:     to.StringPtr(stringSplit[1]),
			Sku:       to.StringPtr(stringSplit[2]),
			Version:   to.StringPtr(stringSplit[3]),
		},
	}
	return storageProfile
}

// randomPasswordString generates random string with restrictions of given length.
// ex: 3i[g0|)z for n of size 8
func randomPasswordString(n int) string {
	digits := "0123456789"
	specials := "~=+%^*/()[]{}/!@#$?|"
	var letter = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	b[0] = digits[rand.Intn(len(digits))]
	b[1] = specials[rand.Intn(len(specials))]
	for i := 2; i < n; i++ {
		b[i] = letter[rand.Intn(len(letter))]
	}
	rand.Shuffle(len(b), func(i, j int) {
		b[i], b[j] = b[j], b[i]
	})
	return string(b)
}

//randomString generates random string of given length.
// ex: for n = 8 it generates Excb1VQs
func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// getTimeZoneMap generates a map of different time zones and returns to the user.
// TODO: Covered the US TimeZones, need to fill up the other timezones.
func getTimeZoneMap() map[string]string {
	timeZoneMap := make(map[string]string)
	timeZoneMap["centralus"] = "Central Standard Time"
	timeZoneMap["eastus2"] = "Eastern Daylight Time"
	timeZoneMap["eastus"] = "Eastern Standard Time"
	timeZoneMap["westus"] = "Pacific Standard Time"
	timeZoneMap["westus2"] = "Pacific Daylight Time"
	timeZoneMap["northcentralus"] = "Central Daylight Time"
	timeZoneMap["southcentralus"] = "Central Daylight Time"
	return timeZoneMap
}

// generateResourceName generates the names for the individual resource components of an instance
// for ex: vkapalav-winc-47hkp-winworker--Pt8hW-ip
func (az *AzureProvider) generateResourceName(resource, randomStr string) (name string) {
	name = strings.Join([]string{az.infraID, windowsWorker, randomStr, resource}, "-")
	return name
}

// getIPAdress gets the IP Address by IP resource name as an argument.
func (az *AzureProvider) getIPAddress(ctx context.Context) (ipAddress *string, err error) {
	result, err := az.ipClient.Get(ctx, az.resourceGroupName, az.IpName, "")
	if errorCheck(err) {
		return nil, err
	}
	ipAddressProperties := *(result.PublicIPAddressPropertiesFormat)
	ipAddress = &(*(ipAddressProperties.IPAddress))
	return
}

// constructWinRMConfig constructs the winrm Listeners for the instance for now it will configure only one listener, but this may
// scale to multiple listeners depending on the use case. By default they listen via Http protocol.
// TODO: Provide the CertificateURL for the winRMListeners, the url need to be grabbed from the Key Vault.
func constructWinRMConfig() (winRMConfig *compute.WinRMConfiguration) {

	winRMListeners := &[]compute.WinRMListener{
		{
			Protocol: "Http",
		},
	}
	winRMConfig = &compute.WinRMConfiguration{
		Listeners: winRMListeners,
	}
	return winRMConfig
}

// constructAdditionalContent constructs the commands need to executed upon windows node set up.
// "AutoLogon" setting is responsible to logging into the windows instance and "FirstLogonCommands" is responsible
// for executing the commands on start i.e in this case it will set up WinRM for Ansible to execute remote commands.
func constructAdditionalContent(instanceName, adminUserName, adminPassword string) *[]compute.AdditionalUnattendContent {
	firstLogonData :=
		"<FirstLogonCommands> " +
			"<SynchronousCommand> " +
			"<CommandLine>cmd /c mkdir \"%TEMP%\"\\script</CommandLine> " +
			"<Description>Create the script directory</Description> " +
			"<Order>11</Order> " +
			"</SynchronousCommand> " +
			"<SynchronousCommand> " +
			"<CommandLine>cmd /c copy C:\\AzureData\\CustomData.bin " +
			"\"%TEMP%\"\\script\\winrm.ps1\"</CommandLine> " +
			"<Description>Move the CustomData file to the working directory</Description> " +
			"<Order>12</Order>" +
			"</SynchronousCommand> " +
			"<SynchronousCommand> " +
			"<CommandLine>cmd /c powershell.exe -sta -ExecutionPolicy Unrestricted -file " +
			"\"%TEMP%\"\\script\\winrm.ps1</CommandLine> " +
			"<Description>Execute the WinRM enabling script</Description> " +
			"<Order>13</Order> " +
			"</SynchronousCommand> " +
			"</FirstLogonCommands>"

	autoLogonData := fmt.Sprintf("<AutoLogon><Domain>%s</Domain><Username>%s</Username><Password><Value>%s</Value></Password>"+
		"<LogonCount>1</LogonCount><Enabled>true</Enabled></AutoLogon>", instanceName, adminUserName, adminPassword)
	additionalContent := &[]compute.AdditionalUnattendContent{
		{
			PassName:      "OobeSystem",
			ComponentName: "Microsoft-Windows-Shell-Setup",
			SettingName:   "AutoLogon",
			Content:       to.StringPtr(autoLogonData),
		},
		{
			PassName:      "OobeSystem",
			ComponentName: "Microsoft-Windows-Shell-Setup",
			SettingName:   "FirstLogonCommands",
			Content:       to.StringPtr(firstLogonData),
		},
	}
	return additionalContent
}

// constructOSProfile constructs the OS Profile for the creation of windows instance. The OS Profile consists of information
// such as configuring remote management listeners, instance access setup.
func (az *AzureProvider) constructOSProfile(ctx context.Context) (osProfile *compute.OSProfile, vmName, password string) {
	instanceName := windowsWorker + randomString(5)
	adminUserName := "core"
	adminPassword := randomPasswordString(12)
	computeWinRMConfig := constructWinRMConfig()
	additionalContent := constructAdditionalContent(instanceName, adminUserName, adminPassword)
	data := `$url = "https://raw.githubusercontent.com/ansible/ansible/devel/examples/scripts/ConfigureRemotingForAnsible.ps1"
        $file = "$env:temp\ConfigureRemotingForAnsible.ps1"
        (New-Object -TypeName System.Net.WebClient).DownloadFile($url,  $file)
        powershell.exe -ExecutionPolicy ByPass -File $file`

	var nodeLocation string
	if !checkForNil(az.getvnetLocation(ctx)) {
		nodeLocation = *(az.getvnetLocation(ctx))
	}
	timeZoneMap := getTimeZoneMap()
	osProfile = &compute.OSProfile{
		ComputerName:  to.StringPtr(instanceName),
		AdminUsername: to.StringPtr(adminUserName),
		AdminPassword: to.StringPtr(adminPassword),
		CustomData:    to.StringPtr(base64.StdEncoding.EncodeToString([]byte(data))),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:          to.BoolPtr(true),
			EnableAutomaticUpdates:    to.BoolPtr(false),
			TimeZone:                  to.StringPtr(timeZoneMap[nodeLocation]),
			AdditionalUnattendContent: additionalContent,
			WinRM:                     computeWinRMConfig,
		},
	}
	return osProfile, instanceName, adminPassword
}

// constructNetworkProfile constructs the Network Profile for the instance to be created.
// The network profile consists of information such as nic ID that to be attached to the instance
// and if multiple ID's need to be configured select which one need to be primary.
func (az *AzureProvider) constructNetworkProfile(ctx context.Context,
	vmName string) (networkProfile *compute.NetworkProfile, err error) {
	var index int
	var vnetName, subnetName string
	var subnetList []network.Subnet
	var subnetProperties network.SubnetPropertiesFormat
	var nsgStruct network.SecurityGroup
	var vnetProfile *network.VirtualNetwork

	vnetProfile, err = az.getvnetProfile(ctx)
	if errorCheck(err) {
		return nil, fmt.Errorf("cannot get vnet profile")
	}

	if !checkForNil(vnetProfile.Name) {
		vnetName = *(vnetProfile.Name)
	} else {
		return nil, fmt.Errorf("cannot obtain vnet name of openshift cluster")
	}
	if !checkForNil(vnetProfile.VirtualNetworkPropertiesFormat.Subnets) {
		subnetList = *(vnetProfile.VirtualNetworkPropertiesFormat.Subnets)
	} else {
		return nil, fmt.Errorf("cannot obtain subnet list from existing vnet")
	}
	for i, subnet := range subnetList {
		if strings.Contains(*(subnet.Name), "worker") {
			index = i
			break
		}
	}
	if !checkForNil(subnetList[index].Name) {
		subnetName = *(subnetList[index].Name)
	} else {
		return nil, fmt.Errorf("cannot obtain worker nodes subnet name")
	}
	if !checkForNil(subnetList[index].SubnetPropertiesFormat) {
		subnetProperties = *(subnetList[index].SubnetPropertiesFormat)
	} else {
		return nil, fmt.Errorf("cannot obtain subnet properties")
	}
	if !checkForNil(subnetProperties.NetworkSecurityGroup) {
		nsgStruct = *(subnetProperties.NetworkSecurityGroup)
	} else {
		return nil, fmt.Errorf("cannot obtain security group rules of worker subnet")
	}

	vmRandString := strings.Split(vmName, "-")[1]
	var nsgName string
	if !checkForNil(nsgStruct.ID) {
		nsgID := *(nsgStruct.ID)
		nsgName = extractResourceName(nsgID)
		err := az.createSecurityGroupRules(ctx, nsgName)
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to update security group rules: %v", err)
		}
	} else {
		nsgName = az.generateResourceName("nsg", vmRandString)
		err := az.createSecurityGroupRules(ctx, nsgName)
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to update security group rules: %v", err)
		}
	}
	az.NsgName = nsgName

	if len(az.IpName) == 0 {
		ipName := az.generateResourceName("ip", vmRandString)
		az.IpName = ipName
		_, err := az.createPublicIP(ctx)
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to create IP for node: %v", err)
		}
	}

	var vmNic network.Interface
	if len(az.NicName) > 0 {
		vmNic, err = az.nicClient.Get(ctx, az.resourceGroupName, az.NicName, "")
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to attach user provided nic for the instance: %v", err)
		}
	} else {
		nicName := az.generateResourceName("nic", vmRandString)
		ipConfigName := az.generateResourceName("ipConfig", vmRandString)
		az.NicName = nicName
		ptrvmNic, err := az.createNIC(ctx, vnetName, subnetName, nsgName, ipConfigName)
		vmNic = *(ptrvmNic)
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to create nic for the instance: %v", err)
		}
	}
	nicID := vmNic.ID

	networkProfile = &compute.NetworkProfile{
		NetworkInterfaces: &[]compute.NetworkInterfaceReference{
			{
				ID: nicID,
				NetworkInterfaceReferenceProperties: &compute.NetworkInterfaceReferenceProperties{
					Primary: to.BoolPtr(true),
				},
			},
		},
	}
	return networkProfile, nil
}

// CreateWindowsVM takes in imageId, instanceType and sshKey name to create Windows instance under the same
// resourceGroupName as the existing OpenShift
// TODO: If it fails during the instance creation process it has to delete the resources created
// untill that step.
func (az *AzureProvider) CreateWindowsVM() (*types.Credentials, error) {
	// Construct the VirtualMachine properties
	rand.Seed(time.Now().UnixNano())
	ctx := context.Background()
	vmHardwareProfile := &compute.HardwareProfile{
		VMSize: compute.VirtualMachineSizeTypes(az.instanceType)}
	log.Info(fmt.Sprintf("constructed the HardwareProfile for node"))

	vmStorageProfile := az.constructStorageProfile(az.imageID)
	log.Info(fmt.Sprintf("constructed the Storage Profile for node"))

	vmOSProfile, instanceName, adminPassword := az.constructOSProfile(ctx)
	log.Info(fmt.Sprintf("constructed the OSProfile for the node"))

	vmNetworkProfile, err := az.constructNetworkProfile(ctx, instanceName)
	if errorCheck(err) {
		return nil, err
	}
	log.Info(fmt.Sprintf("constructed the network profile for the node"))

	log.Info(fmt.Sprintf("constructed all the profiles, about to create instance."))
	future, err := az.vmClient.CreateOrUpdate(
		ctx,
		az.resourceGroupName,
		instanceName,
		compute.VirtualMachine{
			Location: az.getvnetLocation(ctx),
			VirtualMachineProperties: &compute.VirtualMachineProperties{
				HardwareProfile: vmHardwareProfile,
				StorageProfile:  vmStorageProfile,
				OsProfile:       vmOSProfile,
				NetworkProfile:  vmNetworkProfile,
			},
		},
	)
	if errorCheck(err) {
		return nil, fmt.Errorf("instance failed to create: %v", err)
	}
	err = future.WaitForCompletionRef(ctx, az.vmClient.Client)
	if errorCheck(err) {
		return nil, fmt.Errorf("instance failed to create: %v", err)
	}
	vmInfo, err := future.Result(az.vmClient)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to obtain instance info"))
	}

	resourceTrackerFilePath, err := resource.MakeFilePath(az.resourceTrackerDir)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("unable to create a file"))
	}

	err = resource.AppendInstallerInfo([]string{*(vmInfo.Name)}, []string{az.NsgName}, resourceTrackerFilePath)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("unable to append the resources"))
	}

	log.Info(fmt.Sprintf("Successfully created windows instance: %s", instanceName))

	ipAddress, ipErr := az.getIPAddress(ctx)
	if errorCheck(ipErr) {
		log.Error(err, fmt.Sprintf("couldn't get the IP Address for corresponding resource: %s", az.IpName))
		*ipAddress = az.IpName
	}
	resultData := fmt.Sprintf("xfreerdp /u:core /v:%s /h:1080 /w:1920 /p:'%s' \n", *ipAddress, adminPassword)
	resultPath := az.resourceTrackerDir + instanceName
	err = resource.StoreCredentialData(resultPath, resultData)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("unable to write data into file"))
		log.Info(fmt.Sprintf("xfreerdp /u:core /v:%s /h:1080 /w:1920 /p:%s", *ipAddress, adminPassword))
	} else {
		log.Info(fmt.Sprintf("Please Check for file %s in %s directory on how to access the node",
			instanceName, az.resourceTrackerDir))
	}
	credentials := types.NewCredentials(instanceName, *ipAddress, adminPassword)
	return credentials, nil
}

// getNICname returns nicName by taking instance name as an argument.
func (az *AzureProvider) getNICname(ctx context.Context, vmName string) (err error, nicName string) {
	vmStruct, err := az.vmClient.Get(ctx, az.resourceGroupName, vmName, "instanceView")
	if err != nil {
		log.Error(err, fmt.Sprintf("cannot fetch the instance data of %s", vmName))
		return
	}
	networkProfile := vmStruct.VirtualMachineProperties.NetworkProfile
	networkInterface := (*networkProfile.NetworkInterfaces)[0]
	nicID := *networkInterface.ID
	nicName = extractResourceName(nicID)
	return nil, nicName
}

// getIPname returns ipName by taking instance name as an argument.
func (az *AzureProvider) getIPname(ctx context.Context, vmName string) (err error, ipName string) {
	vmStruct, err := az.vmClient.Get(ctx, az.resourceGroupName, vmName, "instanceView")
	if err != nil {
		log.Error(err, fmt.Sprintf("cannot fetch the instance data of %s", vmName))
		return
	}
	networkProfile := vmStruct.VirtualMachineProperties.NetworkProfile
	networkInterface := (*networkProfile.NetworkInterfaces)[0]
	nicID := *networkInterface.ID
	nicName := extractResourceName(nicID)
	interfaceStruct, err := az.nicClient.Get(ctx, az.resourceGroupName, nicName, "")
	if err != nil {
		log.Error(err, fmt.Sprintf("cannot fetch the network interface data of %s", vmName))
	}
	interfacePropFormat := *(interfaceStruct.InterfacePropertiesFormat)
	interfaceIPConfigs := *(interfacePropFormat.IPConfigurations)
	ipConfigProp := *(interfaceIPConfigs[0].InterfaceIPConfigurationPropertiesFormat)
	ipID := *(ipConfigProp.PublicIPAddress.ID)
	ipName = extractResourceName(ipID)
	return nil, ipName
}

// destroyIP deletes the IP address by taking it's name as argument.
func (az *AzureProvider) destroyIP(ctx context.Context, ipName string) (err error) {
	_, err = az.ipClient.Delete(ctx, az.resourceGroupName, ipName)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to delete the public IP: %s", ipName))
		return
	}
	return
}

// destroyInstance deletes the instance by taking it's name as argument.
func (az *AzureProvider) destroyInstance(ctx context.Context, vmName string) (err error) {
	future, err := az.vmClient.Delete(ctx, az.resourceGroupName, vmName)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to delete the instance: %s", vmName))
		return
	}
	err = future.WaitForCompletionRef(ctx, az.vmClient.Client)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to delete the instance: %s", vmName))
		return
	}
	return nil
}

// destroyNIC deletes the nic by taking it's name as argument.
func (az *AzureProvider) destroyNIC(ctx context.Context, nicName string) (err error) {
	_, err = az.nicClient.Delete(ctx, az.resourceGroupName, nicName)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to delete the nic: %s", nicName))
		return
	}
	return
}

// deleteSpecificRule deletes particular rule set by name from the list of available rules set.
func (az *AzureProvider) deleteSpecificRule(s []network.SecurityRule, name string) (updatedList []network.SecurityRule) {
	for i, rule := range s {
		if !checkForNil(rule.Name) {
			if strings.Contains(*(rule.Name), name) {
				updatedList = append(s[:i], s[i+1:]...)
			}
		}
	}
	return
}

// deleteNSGRules deletes the security group rules by taking it's name as argument.
// it deletes the rdp and vnet_traffic rules from the worker subnet security group rules.
func (az *AzureProvider) deleteNSGRules(ctx context.Context, nsgName string) (err error) {
	var secGroupPropFormat network.SecurityGroupPropertiesFormat
	var secGroupRules []network.SecurityRule
	secGroupProfile, err := az.nsgClient.Get(ctx, az.resourceGroupName, nsgName, "")
	if errorCheck(err) {
		return fmt.Errorf("cannot obtain the worker subnet security group rules: %v", err)
	}
	if !checkForNil(secGroupProfile.SecurityGroupPropertiesFormat) {
		secGroupPropFormat = *(secGroupProfile.SecurityGroupPropertiesFormat)
	} else {
		return fmt.Errorf("cannot obtain the security group properties format: %v", err)
	}
	if !checkForNil(secGroupPropFormat.SecurityRules) {
		secGroupRules = *(secGroupPropFormat.SecurityRules)
	} else {
		fmt.Errorf("cannot obtain the security group rules: %v", err)
	}
	secGroupRules = az.deleteSpecificRule(secGroupRules, "RDP")
	secGroupRules = az.deleteSpecificRule(secGroupRules, "vnet_traffic")
	future, err := az.nsgClient.CreateOrUpdate(
		ctx,
		az.resourceGroupName,
		nsgName,
		network.SecurityGroup{
			Location: az.getvnetLocation(ctx),
			SecurityGroupPropertiesFormat: &network.SecurityGroupPropertiesFormat{
				SecurityRules: &secGroupRules,
			},
		},
	)
	if errorCheck(err) {
		return fmt.Errorf("cannot delete the security group rules of the worker subnet: %v", err)
	}
	err = future.WaitForCompletionRef(ctx, az.nsgClient.Client)
	if errorCheck(err) {
		return fmt.Errorf("cannot delete the security group rules of the worker subnet: %v", err)
	}
	_, err = future.Result(az.nsgClient)
	return
}

// destroyDisk deletes the disk attached with the instance by taking its name
// as an argument.
func (az *AzureProvider) destroyDisk(ctx context.Context, vmInfo compute.VirtualMachine) (err error) {
	vmStorageProfile := *(vmInfo.VirtualMachineProperties.StorageProfile)
	vmOSdiskProperties := *(vmStorageProfile.OsDisk)
	diskName := *(vmOSdiskProperties.Name)
	_, err = az.diskClient.Delete(ctx, az.resourceGroupName, diskName)
	if errorCheck(err) {
		log.Error(err, fmt.Sprintf("failed to delete the root disk: %s", diskName))
		return
	}
	return
}

// DestroyWindowsVMs destroys all the resources created by the CreateWindows method.
func (az *AzureProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file
	log.Info(fmt.Sprintf("processing file '%s'", az.resourceTrackerDir))
	ctx := context.Background()
	resourceTrackerFilePath, err := resource.MakeFilePath(az.resourceTrackerDir)
	if errorCheck(err) {
		return err
	}
	installerInfo, err := resource.ReadInstallerInfo(resourceTrackerFilePath)
	if errorCheck(err) {
		return fmt.Errorf("unable to get saved info from json file")
	}
	var terminatedInstances, deletedSg []string
	var rdpFilePath string

	for _, vmName := range installerInfo.InstanceIDs {

		_, nicName := az.getNICname(ctx, vmName)
		_, ipName := az.getIPname(ctx, vmName)

		vmInfo, _ := az.vmClient.Get(ctx, az.resourceGroupName, vmName, compute.InstanceView)

		log.Info(fmt.Sprintf("deleting the resources associated with the instance: %s", vmName))

		err = az.destroyInstance(ctx, vmName)
		if !errorCheck(err) {
			log.Info(fmt.Sprintf("deleted the instance '%s'", vmName))
		}

		err = az.destroyNIC(ctx, nicName)
		if !errorCheck(err) {
			log.Info(fmt.Sprintf("deleted the NIC of instance"))
		}

		err = az.destroyIP(ctx, ipName)
		if !errorCheck(err) {
			log.Info(fmt.Sprintf("deleted the IP of instance"))
		}

		err = az.destroyDisk(ctx, vmInfo)
		if !errorCheck(err) {
			log.Info(fmt.Sprintf("deleted the disk attached to the instance"))
		}

		rdpFilePath = az.resourceTrackerDir + vmName
		err = resource.DeleteCredentialData(rdpFilePath)
		if errorCheck(err) {
			log.Error(err, fmt.Sprintf("unable to remove the file: %s", rdpFilePath))
		}

		terminatedInstances = append(terminatedInstances, vmName)
	}

	for _, nsgName := range installerInfo.SecurityGroupIDs {
		err = az.deleteNSGRules(ctx, nsgName)
		if !errorCheck(err) {
			log.Info(fmt.Sprintf("deleted the created security group rules in worker subnet"))
		}

		deletedSg = append(deletedSg, nsgName)
	}

	// Update the 'windows-node-installer.json' file
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, resourceTrackerFilePath)
	if errorCheck(err) {
		log.Info(fmt.Sprintf("%s file was not updated, %v", resourceTrackerFilePath, err))
	}
	return nil
}
