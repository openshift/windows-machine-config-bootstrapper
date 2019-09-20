package azure

import (
	"context"
	"fmt"
	"strings"

	compute "github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-03-01/compute"
	network "github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	client "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	resource "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logger.Log.WithName("azure-vm")

var (
	resourceGroupName string
	subscriptionID    string
	infraID           string
)

// azureInfo specifically holds information passed by the user through flags.
type azureInfo struct {
	// IP resource name for the instance.
	IpName string
	// NIC resource name for the instance.
	NicName string
}

// AzureProvider holds azure platform specific information required for creating/deleting
// the windows node. In this struct it stores the clients that talk to different
// components while creating an instance.
type AzureProvider struct {
	vnetClient         network.VirtualNetworksClient
	vmClient           compute.VirtualMachinesClient
	ipClient           network.PublicIPAddressesClient
	subnetsClient      network.SubnetsClient
	nicClient          network.InterfacesClient
	nsgClient          network.SecurityGroupsClient
	authorizer         autorest.Authorizer
	AzureUserInput     *azureInfo
	resourceTrackerDir string
}

// New returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func New(openShiftClient *client.OpenShift, credentialPath, resourceTrackerDir string) (*AzureProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if err != nil {
		return nil, err
	}

	AzureUserInput := azureInfo{
		NicName: "",
		IpName:  "",
	}

	infraID, _ = openShiftClient.GetInfrastructureID()
	resourceauthorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return nil, err
	}
	getFileSettings, err := auth.GetSettingsFromFile()
	subscriptionID = getFileSettings.GetSubscriptionID()
	resourceGroupName = provider.Azure.ResourceGroupName

	vnetClient := getVnetClient(resourceauthorizer)
	vmClient := getVMClient(resourceauthorizer)
	ipClient := getIPClient(resourceauthorizer)
	subnetClient := getSubnetsClient(resourceauthorizer)
	nicClient := getNicClient(resourceauthorizer)
	nsgClient := getNsgClient(resourceauthorizer)

	return &AzureProvider{vnetClient, vmClient, ipClient,
		subnetClient, nicClient, nsgClient, resourceauthorizer,
		&AzureUserInput, resourceTrackerDir}, nil
}

// getVnetClient gets the Networking Client by passing the authorizer token.
func getVnetClient(authorizer autorest.Authorizer) network.VirtualNetworksClient {
	vnetClient := network.NewVirtualNetworksClient(subscriptionID)
	vnetClient.Authorizer = authorizer
	return vnetClient
}

// getVMClient gets the Virtual Machine Client by passing the authorizer token.
func getVMClient(authorizer autorest.Authorizer) compute.VirtualMachinesClient {
	vmClient := compute.NewVirtualMachinesClient(subscriptionID)
	vmClient.Authorizer = authorizer
	return vmClient
}

// getIPClient gets the IP Client by passing the authorizer token.
func getIPClient(authorizer autorest.Authorizer) network.PublicIPAddressesClient {
	ipClient := network.NewPublicIPAddressesClient(subscriptionID)
	ipClient.Authorizer = authorizer
	return ipClient
}

// getSubnetsClient gets the Subnet Client by passing the authorizer token.
func getSubnetsClient(authorizer autorest.Authorizer) network.SubnetsClient {
	subnetsClient := network.NewSubnetsClient(subscriptionID)
	subnetsClient.Authorizer = authorizer
	return subnetsClient
}

// getNicClient gets the NIC Client by passing the authorizer token.
func getNicClient(authorizer autorest.Authorizer) network.InterfacesClient {
	nicClient := network.NewInterfacesClient(subscriptionID)
	nicClient.Authorizer = authorizer
	return nicClient
}

// getNsgClient gets the network security group by passing the authorizer token.
func getNsgClient(authorizer autorest.Authorizer) network.SecurityGroupsClient {
	nsgClient := network.NewSecurityGroupsClient(subscriptionID)
	nsgClient.Authorizer = authorizer
	return nsgClient
}

// createPublicIP creates the public IP for the instance
func createPublicIP(ctx context.Context, ipClient network.PublicIPAddressesClient, ipName string) (ip network.PublicIPAddress, err error) {
	future, err := ipClient.CreateOrUpdate(
		ctx,
		resourceGroupName,
		ipName,
		network.PublicIPAddress{
			Name:     to.StringPtr(ipName),
			Location: to.StringPtr("centralus"),
			PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
				PublicIPAddressVersion:   network.IPv4,
				PublicIPAllocationMethod: network.Static,
			},
		},
	)

	if err != nil {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	err = future.WaitForCompletionRef(ctx, ipClient.Client)
	if err != nil {
		return ip, fmt.Errorf("cannot create public ip address: %v", err)
	}

	return future.Result(ipClient)
}

// getPublicIP gets the existing public IP
func getPublicIP(ctx context.Context, ipClient network.PublicIPAddressesClient, ipName string) (network.PublicIPAddress, error) {
	return ipClient.Get(ctx, resourceGroupName, ipName, "")
}

// getVirtualNetworkSubnet gets the subnet from the existing virtual network.
func getVirtualNetworkSubnet(ctx context.Context, subnetsClient network.SubnetsClient, vnetName string, subnetName string) (subnet network.Subnet, err error) {
	return subnetsClient.Get(ctx, resourceGroupName, vnetName, subnetName, "")
}

// getNetworkSecurityGroup gets the network security groups from the existing virtual network.
func getNetworkSecurityGroup(ctx context.Context, nsgClient network.SecurityGroupsClient, nsgName string) (nsg network.SecurityGroup, err error) {
	return nsgClient.Get(ctx, resourceGroupName, nsgName, "")
}

// getNic gets the nic of the existing openshift cluster.
func getNic(ctx context.Context, nicClient network.InterfacesClient, nicName string) (network.Interface, error) {
	return nicClient.Get(ctx, resourceGroupName, nicName, "")
}

// createNIC creates the nic for the instance
func createNIC(ctx context.Context, az *AzureProvider, vnetName, subnetName, nsgName, ipName, nicName string) (nic network.Interface, err error) {
	subnet, err := getVirtualNetworkSubnet(ctx, az.subnetsClient, vnetName, subnetName)
	if err != nil {
		fmt.Errorf("failed to get subnet: %v", err)
	}

	ip, err := getPublicIP(ctx, az.ipClient, ipName)
	if err != nil {
		fmt.Errorf("failed to get ip address: %v", err)
	}

	nicParams := network.Interface{
		Name:     to.StringPtr(nicName),
		Location: to.StringPtr("centralus"),
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
		nsg, err := getNetworkSecurityGroup(ctx, az.nsgClient, nsgName)
		if err != nil {
			fmt.Errorf("failed to get nsg: %v", err)
		}
		nicParams.NetworkSecurityGroup = &nsg
	}

	future, err := az.nicClient.CreateOrUpdate(ctx, resourceGroupName, nicName, nicParams)
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
func createSecurityGroup(ctx context.Context, az *AzureProvider, nsgName string) (err error) {
	future, err := az.nsgClient.CreateOrUpdate(
		ctx,
		resourceGroupName,
		nsgName,
		network.SecurityGroup{
			Location: to.StringPtr("centralus"),
			SecurityGroupPropertiesFormat: &network.SecurityGroupPropertiesFormat{
				SecurityRules: &[]network.SecurityRule{
					{
						Name: to.StringPtr("rdp_access"),
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

// generateResourceName generates the names for the individual resource components of an instance
func generateResourceName(resource string) (name string) {
	name = strings.Join([]string{infraID, "windows", "worker", resource}, "_")
	return name
}

// getIPName checks if user has given any IP information if not it will go ahead and
// create an IP for you. Finally it returns the name of the IP.
func getIPName(az *AzureProvider) (ipName string, err error) {
	if az.AzureUserInput.IpName == "" {
		ipName = generateResourceName("ip")
		_, err := createPublicIP(context.Background(), az.ipClient, ipName)
		if err != nil {
			return "", err
		}
		az.AzureUserInput.IpName = ipName
	} else {
		ipName = az.AzureUserInput.IpName
	}
	return ipName, err
}

// getvnetProfile gets the vnet Profile of the existing openshift cluster.
func getvnetProfile(vnetClient network.VirtualNetworksClient) (vnetProfile network.VirtualNetwork) {
	vnetList, err := vnetClient.List(context.Background(), resourceGroupName)
	if err != nil {
		fmt.Errorf("cannot get the vnetProfile info: %v", err)
		return
	}
	vnetListValues := vnetList.Values()
	vnetProfile = vnetListValues[0]
	return vnetProfile
}

// getnsgName gets the network security group name.
func getnsgName(name *string, az *AzureProvider) (nsgName string, err error) {
	if name == nil {
		nsgName = generateResourceName("nsg")
		err := createSecurityGroup(context.Background(), az, nsgName)
		if err != nil {
			return "", err
		}
	} else {
		nsgName = *name
	}
	return nsgName, nil
}

// getnicProfile gets the network profile information
func getnicProfile(ctx context.Context, az *AzureProvider, vnetName, subnetName, nsgName, ipName string) (vmnic network.Interface, err error) {
	if az.AzureUserInput.NicName == "" {
		nicName := generateResourceName("nic")
		vmnic, err = createNIC(context.Background(), az, vnetName, subnetName, nsgName, ipName, nicName)
		if err != nil {
			return
		}
		az.AzureUserInput.NicName = nicName
	} else {
		vmnic, err = getNic(context.Background(), az.nicClient, az.AzureUserInput.NicName)
		if err != nil {
			return
		}
	}
	return vmnic, nil
}

// constructStorageProfile constructs the Storage Profile for the creation of windows instance. The Storage Profile consists of the
// Image reference details such as which instance type, version etc.
func constructStorageProfile(imageId, instanceType string) (storageProfile *compute.StorageProfile) {
	storageProfile = &compute.StorageProfile{
		ImageReference: &compute.ImageReference{
			Publisher: to.StringPtr("MicrosoftWindowsServer"),
			Offer:     to.StringPtr(imageId),
			Sku:       to.StringPtr(instanceType),
			Version:   to.StringPtr("latest"),
		},
	}
	return storageProfile
}

// constructWinRMConfig constructs the winrm Listeners for the instance for now it will configure only one listener, but this may
// scale to multiple listeners depending on the use case. By default they listen via Http protocol.
func constructWinRMConfig() (winRMConfig *compute.WinRMConfiguration) {
	winRMListeners := &[]compute.WinRMListener{
		compute.WinRMListener{
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
// TODO: need to come up with proper way of naming the instance, it got many naming restrictions.
func constructOSProfile(adminUserName, adminPassword string) (osProfile *compute.OSProfile, vmName string) {
	instanceName := "wincwindows"
	computeWinRMConfig := constructWinRMConfig()
	osProfile = &compute.OSProfile{
		ComputerName:  to.StringPtr(instanceName),
		AdminUsername: to.StringPtr(adminUserName),
		AdminPassword: to.StringPtr(adminPassword),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:       to.BoolPtr(true),
			EnableAutomaticUpdates: to.BoolPtr(false),
			TimeZone:               to.StringPtr("centralus"),
			WinRM:                  computeWinRMConfig,
		},
	}
	return osProfile, instanceName
}

// constructNetworkProfile constructs the Network Profile for the instance to be created. The network profile consists of information such as nic ID that to be
// attached to the instance and if multiple ID's need to be configured select which one need to be primary.
func constructNetworkProfile(nicID *string) (networkProfile *compute.NetworkProfile) {
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
	return networkProfile
}

// CreateWindowsVM takes in imageId, instanceType and sshKey name to create Windows instance under the same
// resourceGroupName as the existing OpenShift
// TODO: If it fails during the instance creation process it has to delete the resources created
// untill that step.
func (az *AzureProvider) CreateWindowsVM(imageId, instanceType, sshKey string) (err error) {

	// Construct the VirtualMachine properties
	// Construct the Hardware profile.
	vmHardwareProfile := &compute.HardwareProfile{
		VMSize: compute.VirtualMachineSizeTypesBasicA0}

	// Construct the StorageProfile.
	if imageId == "" {
		imageId = "WindowsServer"
	}
	if instanceType == "" {
		instanceType = "2019-Datacenter"
	}

	vmStorageProfile := constructStorageProfile(imageId, instanceType)
	// Generate a user admin password.
	adminPassword := generateResourceName("password")
	vmOSProfile, instanceName := constructOSProfile("windows", adminPassword)

	ipName, err := getIPName(az)
	if err != nil {
		return err
	}
	log.V(0).Info("created IP for node: %s", ipName)
	vnetProfile := getvnetProfile(az.vnetClient)
	vnetName := *(vnetProfile.Name)
	subnetList := *(vnetProfile.VirtualNetworkPropertiesFormat.Subnets)
	subnetName := *(subnetList[1].Name)

	// Collect the existing network security group from the existing cluster.
	nsgName, err := getnsgName(subnetList[1].NetworkSecurityGroup.Name, az)
	if err != nil {
		return err
	}
	log.V(0).Info("created security group for node: %s", nsgName)
	// Create a nic Profile for the instance.
	vmnicProfile, err := getnicProfile(context.Background(), az, vnetName, subnetName, nsgName, ipName)
	if err != nil {
		return fmt.Errorf("cannot create a NIC profile: %v", err)
	}

	vmNetworkProfile := constructNetworkProfile(vmnicProfile.ID)

	log.V(0).Info("constructed all the profiles, about the create instance.")

	future, err := az.vmClient.CreateOrUpdate(
		context.Background(),
		resourceGroupName,
		instanceName,
		compute.VirtualMachine{
			Location: to.StringPtr("centralus"),
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

	err = future.WaitForCompletionRef(context.Background(), az.vmClient.Client)
	if err != nil {
		return fmt.Errorf("instance could not complete create operation: %v", err)
	}

	vmInfo, err := future.Result(az.vmClient)
	if err != nil {
		log.V(0).Info("failed to obtain instance result info: %v", err)
	}
	resource.AppendInstallerInfo([]string{*(vmInfo.Name)}, []string{nsgName}, az.resourceTrackerDir)

	// Output commandline message to help RDP into the created instance.
	log.V(0).Info("Successfully created windows instance: %s, please RDP into windows with the following:")
	log.V(0).Info("xfreerdp /u:windows /v:%s  /h:1080 /w:1920 /p:%s", ipName, adminPassword)

	return nil
}

// returnNicIpdetails returns nicName and ipName of the instance by taking instance name as an argument.
func returnNicIpdetails(az *AzureProvider, vmName string) (nicName string, ipName string, err error) {
	vmStruct, err := az.vmClient.Get(context.Background(), resourceGroupName, vmName, "instanceView")
	if err != nil {
		log.Error(err, "cannot fetch the instance data of %s", vmName)
		return
	}
	networkProfile := vmStruct.VirtualMachineProperties.NetworkProfile
	networkInterface := (*networkProfile.NetworkInterfaces)[0]
	nicName = *networkInterface.ID

	vnetProfile := getvnetProfile(az.vnetClient)
	subnetList := *(vnetProfile.VirtualNetworkPropertiesFormat.Subnets)
	subnetName := *(subnetList[1].Name)
	ipNamePtr, err := az.ipClient.Get(context.Background(), resourceGroupName, subnetName, "")
	ipName = *ipNamePtr.Name
	if err != nil {
		log.Error(err, "cannot fetch the instance public IP of %s", vmName)
	}
	return
}

// deletePublicIP deletes the already created Public IP of the instance.
func deletePublicIP(ctx context.Context, ipClient network.PublicIPAddressesClient, ipName string) (result network.PublicIPAddressesDeleteFuture, err error) {
	return ipClient.Delete(ctx, resourceGroupName, ipName)
}

// deleteNIC deletes the already created nic of the instance.
func deleteNIC(ctx context.Context, nicClient network.InterfacesClient, nicName string) (result network.InterfacesDeleteFuture, err error) {
	return nicClient.Delete(ctx, resourceGroupName, nicName)
}

// deleteSecGroup deletes the security group rules created for the instance.
func deleteSecGroup(ctx context.Context, nsgClient network.SecurityGroupsClient, nsgName string) (result network.SecurityGroupsDeleteFuture, err error) {
	return nsgClient.Delete(ctx, resourceGroupName, nsgName)
}

// DestroyWindowsVMs destroys all the resources created by the CreateWindows method.
func (az *AzureProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file
	log.V(0).Info("processing file '%s'", az.resourceTrackerDir)
	installerInfo, err := resource.ReadInstallerInfo(az.resourceTrackerDir)
	if err != nil {
		return fmt.Errorf("unable to get saved info from json file")
	}
	var terminatedInstances, deletedSg []string

	for _, vmname := range installerInfo.InstanceIDs {

		nicName, ipName, err := returnNicIpdetails(az, vmname)
		_, err = deletePublicIP(context.Background(), az.ipClient, ipName)
		if err != nil {
			log.Error(err, "failed to delete the public IP: %s", vmname)
		}

		log.V(0).Info("deleted the IP of instance '%s'", vmname)

		_, err = deleteNIC(context.Background(), az.nicClient, nicName)
		if err != nil {
			log.Error(err, "failed to delete the nic: %s", vmname)
		}

		log.V(0).Info("deleted the NIC of instance '%s'", vmname)

		future, err := az.vmClient.Delete(context.Background(), resourceGroupName, vmname)
		if err != nil {
			log.Error(err, "failed to delete the instance: %s", vmname)
		}

		log.V(0).Info("deleted the instance '%s'", vmname)

		err = future.WaitForCompletionRef(context.Background(), az.vmClient.Client)
		if err != nil {
			log.Error(err, "cannot complete the instance delete operation on %s: %s", vmname, err)
		}

		terminatedInstances = append(terminatedInstances, vmname)
	}

	for _, nsgName := range installerInfo.SecurityGroupIDs {
		_, err := deleteSecGroup(context.Background(), az.nsgClient, nsgName)
		if err != nil {
			log.Error(err, "failed to delete the sec group: %s", nsgName)
		}

		log.V(0).Info("deleted the security group rule '%s'", nsgName)

		deletedSg = append(deletedSg, nsgName)
	}

	// Update the 'windows-node-installer.json' file
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, az.resourceTrackerDir)
	if err != nil {
		log.V(0).Info("%s file was not updated, %v", az.resourceTrackerDir, err)
	}
	return nil
}
