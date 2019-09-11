package azure

import (
	"fmt"

	compute "github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2018-06-01/compute"
	kauth "github.com/Azure/azure-sdk-for-go/services/keyvault/auth"
	network "github.com/Azure/azure-sdk-for-go/services/network/mgmt/2017-09-01/network"
	autorest "github.com/Azure/go-autorest/autorest"
	aauth "github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	client "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logger.Log.WithName("azure-instance")

//struct to hold azure platform specific information.
type azureProvider struct {
	// Creates an authorizer from the available client credentials
	authorizer autorest.Authorizer
	// client of existing OpenShift cluster
	openShiftClient *client.OpenShift
	// Azure Subscription Id
	azureSubscriptionId string
	// file location to store `windows-node-installer.json`
	resourceTrackerDir string
}

// New returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// Since the credential file may contain multiple accounts and the default account name/ID varies between providers,
// this function requires specifying the credentialAccountID of the user's credential account.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func New(openShiftClient *client.OpenShift, credentialPath, resourceTrackerDir string) (*azureProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if err != nil {
		fmt.Errorf("unable to get the provider: %v", err)
		return nil, err
	}
	resourceauthorizer, err := kauth.NewAuthorizerFromFile(credentialPath)
	if err != nil {
		fmt.Errorf("unable to initialize the resourceauthorizer: %v", err)
		return nil, err
	}
	azureSubscriptionId, err := aauth.GetSubscriptionID()
	if err != nil {
		fmt.Errorf("unable to get the subscription id: %v", err)
		return nil, error
	}
	return &azureProvider{resourceauthorizer,
		openShiftClient, resourceTrackerDir,
		virtualNetworkName,
		azureSubscriptionId}, nil
}

// Get the Azure Virtual Machine Client by passing the client authorizer
func getVMClient(authorizer autorest.Authorizer) compute.VirtualMachinesClient {
	vmClient := compute.NewVirtualMachinesClient(az.azureSubscriptionId)
	a, _ := az.authorizer
	vmClient.Authorizer = a
	return vmClient
}

// CreateWindowsVM takes in imageId, instanceType and sshKey name to create Windows instance under the same
// resourceGroupName as the existing OpenShift
func (az *azureProvider) CreateWindowsVM(imageId, instanceType, sshKey string, nicName string, vmName string) error {
	// Get the specified virtual network by resource group.
	vnetClient := network.NewVirtualNetworksClient(az.azureSubscriptionId)
	vnetClient.Authorizer = az.authorizer
	provider, err := az.openShiftClient.GetCloudProvider()
	vnetList, err := vnetClient.List(provider.Azure.ResourceGroupName)
	if err != nil {
		fmt.Errorf("failed to fetch the Vnet info: %v", err)
		return err
	}

	// Get vmClient to create an instance
	vmClient := getVMClient(az.authorizer)

	// Construct the VirtualMachine properties
	// construct the hardware profile.
	vmHardwareProfile := &compute.HardwareProfile{
		VMSize: compute.VirtualMachineSizeTypesBasicA0}

	//construct the StorageProfile.
	vmStorageProfile := &compute.StorageProfile{
		ImageReference: &compute.ImageReference{
			Publisher: to.StringPtr("publisher"),
			Offer:     to.StringPtr("offer"),
			Sku:       to.StringPtr(imageId),
			Version:   to.StringPtr("latest"),
		},
	}

	//construct the OSProfile.
	vmOsProfile := &compute.OSProfile{
		ComputerName: to.StringPtr(vmName),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:       to.BoolPtr(true),
			EnableAutomaticUpdates: to.BoolPtr(false),
			TimeZone:               to.StringPtr("Eastern Standard Time"),
			WinRM.Listeners: {
				Protocol: "Http",
			},
		},
	}

	//construct the NetworkProfile.
	if vnetList.vnlr.Value[] == nil {
		fmt.Errorf("failed to fetch the virtual network in the existing resource group")
	}
	
	vmNetworkProfile := &compute.NetworkProfile{
		NetworkInterfaces: &[]compute.NetworkInterfaceReference{
			{
				ID: vnetList.vnlr.*Value[0].ID,
				NetworkInterfaceReferenceProperties: &compute.NetworkInterfaceReferenceProperties{
					Primary: to.BoolPtr(true),
				},
			},
		},
	}

	instanceCreate, err := vmClient.CreateOrUpdate(
		az.openShiftClient.Azure.ResourceGroupName,
		vmName,
		compute.VirtualMachine{
			Location: to.StringPtr("centralus"),
			VirtualMachineProperties: &compute.VirtualMachineProperties{
				HardwareProfile: vmHardwareProfile,
				StorageProfile:  vmStorageProfile,
				OsProfile:       vmOsProfile,
				NetworkProfile:  vmNetworkProfile,
			},
		},
	)

	if err != nil {
		fmt.Errorf("failed to create the instance: %v", err)
		return err
	}
	return nil
}