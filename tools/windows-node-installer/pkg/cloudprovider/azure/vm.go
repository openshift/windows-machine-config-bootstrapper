package azure

import (
	"context"
	"fmt"
	"math/rand"
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
var ctx = context.Background()

// AzureProvider holds azure platform specific information required for creating/deleting
// the windows node.
type AzureProvider struct {
	vnetClient         network.VirtualNetworksClient
	vmClient           compute.VirtualMachinesClient
	ipClient           network.PublicIPAddressesClient
	subnetsClient      network.SubnetsClient
	nicClient          network.InterfacesClient
	nsgClient          network.SecurityGroupsClient
	diskClient         compute.DisksClient
	authorizer         autorest.Authorizer
	resourceGroupName  string
	subscriptionID     string
	infraID            string
	IpName             string
	NicName            string
	NsgName            string
	resourceTrackerDir string
}

// New returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func New(openShiftClient *client.OpenShift, credentialPath, subscriptionID,
	resourceTrackerDir string) (*AzureProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if err != nil {
		return nil, err
	}

	infraID, _ := openShiftClient.GetInfrastructureID()
	resourceAuthorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
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
		resourceGroupName, subscriptionID, infraID, IpName, NicName, NsgName, resourceTrackerDir}, nil
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

// verifyForResource verifies if the resource exists or not.
func verifyForResource(resource string) bool {
	if resource != "" {
		return true
	} else {
		return false
	}
}

// getvnetProfile gets the vnet Profile of the existing openshift cluster.
func (az *AzureProvider) getvnetProfile() (vnetProfile network.VirtualNetwork) {
	vnetList, err := az.vnetClient.List(ctx, az.resourceGroupName)
	if err != nil {
		fmt.Errorf("cannot get the vnetProfile info: %v", err)
		return
	}
	vnetListValues := vnetList.Values()
	vnetProfile = vnetListValues[0]
	return vnetProfile
}

// extractResourceName captures the resource name omitting the other details.
// document the function.
func extractResourceName(rawresource string) (name string) {
	resultList := strings.Split(rawresource, "/")
	arrayLength := len(resultList)
	name = resultList[arrayLength-1]
	return
}

// createPublicIP creates the public IP for the instance
func (az *AzureProvider) createPublicIP(ctx context.Context) (ip network.PublicIPAddress, err error) {
	nodeLocation := *(az.getvnetProfile().Location)
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

	if err != nil {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.ipClient.Client)
	if err != nil {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	return future.Result(az.ipClient)
}

// createNIC creates the nic for the instance
func (az *AzureProvider) createNIC(ctx context.Context, vnetName, subnetName, nsgName string) (nic network.Interface, err error) {
	nodeLocation := *(az.getvnetProfile().Location)
	subnet, err := az.subnetsClient.Get(ctx, az.resourceGroupName, vnetName, subnetName, "")
	if err != nil {
		fmt.Errorf("failed to get subnet: %v", err)
	}

	ip, err := az.ipClient.Get(ctx, az.resourceGroupName, az.IpName, "")

	if err != nil {
		fmt.Errorf("failed to get ip address: %v", err)
	}

	nicParams := network.Interface{
		Name:     to.StringPtr(az.NicName),
		Location: to.StringPtr(nodeLocation),
		InterfacePropertiesFormat: &network.InterfacePropertiesFormat{
			IPConfigurations: &[]network.InterfaceIPConfiguration{
				{
					Name: to.StringPtr("ipConfig1"),
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
		if err != nil {
			fmt.Errorf("failed to get nsg: %v", err)
		}
		nicParams.NetworkSecurityGroup = &nsg
	}

	future, err := az.nicClient.CreateOrUpdate(ctx, az.resourceGroupName, az.NicName, nicParams)
	if err != nil {
		return nic, fmt.Errorf("cannot create nic: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.nicClient.Client)
	if err != nil {
		return nic, fmt.Errorf("cannot create nic: %v", err)
	}

	return future.Result(az.nicClient)
}

// createSecurityGroup tries to create a security group that allows to RDP to the windows node and allow for all the traffic
// coming from the nodes that belong to the same VNET.
func (az *AzureProvider) createSecurityGroup(ctx context.Context, nsgName string) (err error) {
	nodeLocation := *(az.getvnetProfile().Location)
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
							SourceAddressPrefix:      to.StringPtr("*"),
							SourcePortRange:          to.StringPtr("1-65535"),
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
							DestinationAddressPrefix: to.StringPtr("0.0.0.0/0"),
							DestinationPortRanges:    &[]string{"1-65535"},
							Access:                   network.SecurityRuleAccessAllow,
							Direction:                network.SecurityRuleDirectionInbound,
							Priority:                 to.Int32Ptr(200),
						},
					},
				},
			},
		},
	)
	if err != nil {
		return fmt.Errorf("cannot create security group: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, az.nsgClient.Client)
	if err != nil {
		return fmt.Errorf("cannot create security group: %v", err)
	}

	return nil
}

// constructStorageProfile constructs the Storage Profile for the creation of windows instance.
// The Storage Profile consists of the image reference details such as which instance type, version etc.
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

// randonString generates random string of given length.
func randomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// generateResourceName generates the names for the individual resource components of an instance
func (az *AzureProvider) generateResourceName(resource string) (name string) {
	randomStr := randomString(4)
	name = strings.Join([]string{az.infraID, "windows", "worker", randomStr, resource}, "-")
	return name
}

// getIPAdress gets the IP Address by IP resource name as an argument.
func (az *AzureProvider) getIPAddress() (ipAddress string, err error) {
	result, err := az.ipClient.Get(ctx, az.resourceGroupName, az.IpName, "")
	if err != nil {
		log.Error(err, fmt.Sprintf("cannot get the Ip Address: %s", az.IpName))
		return "", err
	}
	ipAddressProperties := *(result.PublicIPAddressPropertiesFormat)
	ipAddress = *(ipAddressProperties.IPAddress)
	return
}

// constructWinRMConfig constructs the winrm Listeners for the instance for now it will configure only one listener, but this may
// scale to multiple listeners depending on the use case. By default they listen via Http protocol.
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

// constructOSProfile constructs the OS Profile for the creation of windows instance. The OS Profile consists of information
// such as configuring remote management listeners, instance access setup.
func (az *AzureProvider) constructOSProfile() (osProfile *compute.OSProfile, vmName, password string) {
	instanceName := randomString(5)
	adminUserName := "windows"
	adminPassword := randomString(12)
	computeWinRMConfig := constructWinRMConfig()
	nodeLocation := *(az.getvnetProfile().Location)
	var TimeZone string
	if nodeLocation == "centralus" {
		TimeZone = "Central Standard Time"
	}
	osProfile = &compute.OSProfile{
		ComputerName:  to.StringPtr(instanceName),
		AdminUsername: to.StringPtr(adminUserName),
		AdminPassword: to.StringPtr(adminPassword),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:       to.BoolPtr(true),
			EnableAutomaticUpdates: to.BoolPtr(false),
			TimeZone:               to.StringPtr(TimeZone),
			WinRM:                  computeWinRMConfig,
		},
	}
	return osProfile, instanceName, adminPassword
}

// constructNetworkProfile constructs the Network Profile for the instance to be created.
// The network profile consists of information such as nic ID that to be attached to the instance
// and if multiple ID's need to be configured select which one need to be primary.
func (az *AzureProvider) constructNetworkProfile() (networkProfile *compute.NetworkProfile, err error) {
	vnetProfile := az.getvnetProfile()
	vnetName := *(vnetProfile.Name)
	subnetList := *(vnetProfile.VirtualNetworkPropertiesFormat.Subnets)
	var index int

	for i, subnet := range subnetList {
		response, _ := regexp.MatchString("\\bworker\\b", *(subnet.Name))
		if response {
			index = i
			break
		}
	}

	subnetName := *(subnetList[index].Name)
	subnetProperties := *(subnetList[index].SubnetPropertiesFormat)
	nsgStruct := *(subnetProperties.NetworkSecurityGroup)
	var nsgName string
	if verifyForResource(*(nsgStruct.ID)) {
		nsgID := *(nsgStruct.ID)
		nsgName = extractResourceName(nsgID)
		err := az.createSecurityGroup(ctx, nsgName)
		if err != nil {
			return nil, fmt.Errorf("failed to create security group rules: %v", err)
		}
	} else {
		nsgName = az.generateResourceName("nsg")
		err := az.createSecurityGroup(ctx, nsgName)
		if err != nil {
			return nil, fmt.Errorf("failed to create security group rules: %v", err)
		}
		az.NsgName = nsgName
	}
	log.Info(fmt.Sprintf("network security group for node is: %s", nsgName))

	if !verifyForResource(az.IpName) {
		ipName := az.generateResourceName("ip")
		az.IpName = ipName
		_, err := az.createPublicIP(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create IP for node: %v", err)
		}
	}

	log.Info(fmt.Sprint("created IP for the node is: ", az.IpName))

	var vmNic network.Interface
	if verifyForResource(az.NicName) {
		vmNic, err = az.nicClient.Get(ctx, az.resourceGroupName, az.NicName, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create nic for the instance: %v", err)
		}
	} else {
		nicName := az.generateResourceName("nic")
		az.NicName = nicName
		vmNic, err = az.createNIC(ctx, vnetName, subnetName, nsgName)
		if err != nil {
			return nil, fmt.Errorf("failed to create nic for the instance: %v", err)
		}
	}
	nicID := vmNic.ID

	log.Info(fmt.Sprintf("created nic for the node is: %s", az.NicName))

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
func (az *AzureProvider) CreateWindowsVM(imageId, instanceType, sshKey string) (err error) {
	// Construct the VirtualMachine properties
	vmHardwareProfile := &compute.HardwareProfile{
		VMSize: compute.VirtualMachineSizeTypes(instanceType)}
	log.Info(fmt.Sprintf("constructed the HardwareProfile for node"))
	vmStorageProfile := az.constructStorageProfile(imageId)
	log.Info(fmt.Sprintf("constructed the Storage Profile for node"))
	vmOSProfile, instanceName, adminPassword := az.constructOSProfile()
	log.Info(fmt.Sprintf("constructed the OSProfile for the node"))
	vmNetworkProfile, err := az.constructNetworkProfile()
	log.Info(fmt.Sprintf("constructed the network profile for the node"))
	log.Info(fmt.Sprintf("constructed all the profiles, about to create instance."))
	nodeLocation := *(az.getvnetProfile().Location)
	future, err := az.vmClient.CreateOrUpdate(
		ctx,
		az.resourceGroupName,
		instanceName,
		compute.VirtualMachine{
			Location: to.StringPtr(nodeLocation),
			VirtualMachineProperties: &compute.VirtualMachineProperties{
				HardwareProfile: vmHardwareProfile,
				StorageProfile:  vmStorageProfile,
				OsProfile:       vmOSProfile,
				NetworkProfile:  vmNetworkProfile,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("instance failed to create: %v", err)
	}
	err = future.WaitForCompletionRef(ctx, az.vmClient.Client)
	if err != nil {
		return fmt.Errorf("instance failed to create: %v", err)
	}
	vmInfo, err := future.Result(az.vmClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to obtain instance info"))
	}
	if az.NsgName == "" {
		resource.AppendInstallerInfo([]string{*(vmInfo.Name)}, []string{}, az.resourceTrackerDir)
	} else {
		resource.AppendInstallerInfo([]string{*(vmInfo.Name)}, []string{az.NsgName}, az.resourceTrackerDir)
	}

	ipAddress, err := az.getIPAddress()
	if err != nil {
		// Output commandline message to help RDP into the created instance.
		log.Info(fmt.Sprintf("Successfully created windows instance: %s, please RDP into windows with the following:",
			instanceName))
		log.Info(fmt.Sprintf("xfreerdp /u:windows /v:%s /h:1080 /w:1920 /p:%s", az.IpName, adminPassword))
	} else {
		log.Info(fmt.Sprintf("Successfully created windows instance: %s, please RDP into windows with the following:",
			instanceName))
		log.Info(fmt.Sprintf("xfreerdp /u:windows /v:%s /h:1080 /w:1920 /p:%s", ipAddress, adminPassword))
	}
	return nil
}

// getNICname returns nicName by taking instance name as an argument.
func (az *AzureProvider) getNICname(vmName string) (err error, nicName string) {
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

// returnNicIpdetails returns ipName by taking instance name as an argument.
func (az *AzureProvider) getIPname(vmName string) (err error, ipName string) {
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
func (az *AzureProvider) destroyIP(ipName string) (err error) {
	future, err := az.ipClient.Delete(ctx, az.resourceGroupName, ipName)
	log.Info(fmt.Sprintf("deleting the IP"))
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the public IP: %s", ipName))
	}
	_, err = future.Result(az.ipClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the public IP: %s", ipName))
	}
	return nil
}

// destroyInstance deletes the instance by taking it's name as argument.
func (az *AzureProvider) destroyInstance(vmName string) (err error) {
	future, err := az.vmClient.Delete(ctx, az.resourceGroupName, vmName)
	log.Info(fmt.Sprintf("deleting the vm: %s", vmName))
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the instance: %s", vmName))
		return
	}
	err = future.WaitForCompletionRef(ctx, az.vmClient.Client)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the instance: %s", vmName))
	}
	return nil
}

// destroyNIC deletes the nic by taking it's name as argument.
func (az *AzureProvider) destroyNIC(nicName string) (err error) {
	future, err := az.nicClient.Delete(ctx, az.resourceGroupName, nicName)
	log.Info(fmt.Sprintf("deleting the nic"))
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the nic: %s", nicName))
	}
	_, err = future.Result(az.nicClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the nic: %s", nicName))
	}
	return nil
}

// destroyNSG deletes the security groups by taking it's name as argument.
func (az *AzureProvider) destroyNSG(nsgName string) (err error) {
	future, err := az.nsgClient.Delete(ctx, az.resourceGroupName, nsgName)
	log.Info(fmt.Sprintf("deleting the security groups"))
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the sec group: %s", nsgName))
	}

	_, err = future.Result(az.nsgClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the sec group: %s", nsgName))
	}
	return nil
}

// destroyDisk deletes the disk attached with the instance by taking its name
// as an argument.
func (az *AzureProvider) destroyDisk(vmInfo compute.VirtualMachine) (err error) {
	vmStorageProfile := *(vmInfo.VirtualMachineProperties.StorageProfile)
	vmOSdiskProperties := *(vmStorageProfile.OsDisk)
	diskName := *(vmOSdiskProperties.Name)
	future, err := az.diskClient.Delete(ctx, az.resourceGroupName, diskName)
	log.Info(fmt.Sprintf("deleting the attached root disk: %s", diskName))
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the root disk: %s", diskName))
	}

	_, err = future.Result(az.diskClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to delete the root disk:", diskName))
	}
	return nil
}

// DestroyWindowsVMs destroys all the resources created by the CreateWindows method.
func (az *AzureProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file
	log.Info(fmt.Sprintf("processing file '%s'", az.resourceTrackerDir))
	installerInfo, err := resource.ReadInstallerInfo(az.resourceTrackerDir)
	if err != nil {
		return fmt.Errorf("unable to get saved info from json file")
	}
	var terminatedInstances, deletedSg []string

	for _, vmName := range installerInfo.InstanceIDs {

		_, nicName := az.getNICname(vmName)
		_, ipName := az.getIPname(vmName)

		vmInfo, _ := az.vmClient.Get(ctx, az.resourceGroupName, vmName, compute.InstanceView)

		err = az.destroyInstance(vmName)
		if err == nil {
			log.Info(fmt.Sprintf("deleted the instance '%s'", vmName))
		}

		err = az.destroyNIC(nicName)
		if err == nil {
			log.Info(fmt.Sprintf("deleted the NIC of instance '%s'", vmName))
		}

		err := az.destroyIP(ipName)
		if err == nil {
			log.Info(fmt.Sprintf("deleted the IP of instance '%s'", vmName))
		}

		err = az.destroyDisk(vmInfo)
		if err == nil {
			log.Info(fmt.Sprintf("deleted the disk attached to the instance '%s'", vmName))
		}

		terminatedInstances = append(terminatedInstances, vmName)
	}

	for _, nsgName := range installerInfo.SecurityGroupIDs {
		err = az.destroyNSG(nsgName)
		if err == nil {
			log.Info(fmt.Sprintf("deleted the security group rule '%s'", nsgName))
		}

		deletedSg = append(deletedSg, nsgName)
	}

	// Update the 'windows-node-installer.json' file
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, az.resourceTrackerDir)
	if err != nil {
		log.Info(fmt.Sprintf("%s file was not updated, %v", az.resourceTrackerDir, err))
	}
	return nil
}
