package azure

import (
	"context"
	"fmt"

	compute "github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2018-06-01/compute"
	kauth "github.com/Azure/azure-sdk-for-go/services/keyvault/auth"
	network "github.com/Azure/azure-sdk-for-go/services/network/mgmt/2017-09-01/network"
	autorest "github.com/Azure/go-autorest/autorest"
	aauth "github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	v1 "github.com/openshift/api/config/v1"
	client "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logger.Log.WithName("azure-instance")

//struct to hold azure platform specific information.
type azureProvider struct {
	// Creates an authorizer from the available client credentials
	authorizer autorest.Authorizer
	// client of existing OpenShift cluster
	openShiftClient *v1.PlatformStatus
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
	getFileSettings, err := aauth.GetSettingsFromFile()
	azureSubscriptionId := getFileSettings.GetSubscriptionID()
	fmt.Printf("subscriptionId: %v\n", azureSubscriptionId)
	/*
		if azureSubscriptionId == nil {
			fmt.Errorf("unable to get the subscription id")
			return
		}
	*/
	return &azureProvider{resourceauthorizer,
		provider, azureSubscriptionId,
		resourceTrackerDir}, nil
}

// Get the Azure Virtual Machine Client by passing the client authorizer
func getVMClient(az *azureProvider) compute.VirtualMachinesClient {
	vmClient := compute.NewVirtualMachinesClient(az.azureSubscriptionId)
	a := az.authorizer
	vmClient.Authorizer = a
	return vmClient
}

// CreateWindowsVM takes in imageId, instanceType and sshKey name to create Windows instance under the same
// resourceGroupName as the existing OpenShift
func (az *azureProvider) CreateWindowsVM(imageId, instanceType, sshKey string, nicName string, vmName string) error {
	// Get the specified virtual network by resource group.
	vnetClient := network.NewVirtualNetworksClient(az.azureSubscriptionId)
	vnetClient.Authorizer = az.authorizer
	vnetList, err := vnetClient.List(context.Background(), az.openShiftClient.Azure.ResourceGroupName)
	if err != nil {
		fmt.Errorf("failed to fetch the Vnet info: %v", err)
		return err
	}

	// Get vmClient to create an instance
	vmClient := getVMClient(az)

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
	winrmlisteners := &[]compute.WinRMListener{
		compute.WinRMListener{
			Protocol: "Http",
		},
	}
	c := &compute.WinRMConfiguration{
		Listeners: winrmlisteners,
	}
	vmOsProfile := &compute.OSProfile{
		ComputerName: to.StringPtr(vmName),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:       to.BoolPtr(true),
			EnableAutomaticUpdates: to.BoolPtr(false),
			TimeZone:               to.StringPtr("Eastern Standard Time"),
			WinRM:                  c,
		},
	}

	//construct the NetworkProfile.
	if vnetList.Values() == nil {
		fmt.Errorf("failed to fetch the virtual network in the existing resource group")
	}

	//find the length of the vnet struct array
	vnetLength := len(vnetList.Values())

	vmNetworkProfile := &compute.NetworkProfile{
		NetworkInterfaces: &[]compute.NetworkInterfaceReference{
			{
				ID: vnetList.Values()[vnetLength-1].ID,
				NetworkInterfaceReferenceProperties: &compute.NetworkInterfaceReferenceProperties{
					Primary: to.BoolPtr(true),
				},
			},
		},
	}

	instanceCreate, err := vmClient.CreateOrUpdate(
		context.Background(),
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

	err = instanceCreate.WaitForCompletionRef(context.Background(), vmClient.Client)
	if err != nil {
		return fmt.Errorf("cannot get the instance create or update response: %v", err)
	}

	//return instanceCreate.Result(vmClient)
	return nil
}
