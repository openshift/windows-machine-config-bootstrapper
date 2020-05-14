package azure

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-03-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-04-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/resource"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
)

const (
	// the '*' is used to match all the ports of the source IP address
	sourcePortRange = "*"
	// the '*' is used to match all the source IP addresses
	destinationAddressPrefix = "*"
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
	// winUser is the user used to login into the instance.
	winUser = "core"
	// sshRulePriority is the priority for the RDP rule
	sshRulePriority = 603
	// sshRuleName is the security group rule name for the RDP rule
	sshRuleName = "SSH"
	// SKU for ip address
	// We are using a Standard SKU in place of a Basic SKU as the Load balancer and IP SKU should match
	// https://docs.microsoft.com/en-us/azure/virtual-network/virtual-network-ip-addresses-overview-arm#sku
	ipSKU = "Standard"
)

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
	// resourcesClient to query resources. Can query all resources
	resourcesClient resources.Client
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
	// requiredRules is the set of SG rules that need to be created or deleted
	requiredRules map[string]*nsgRuleWrapper
}

// nsgRuleWrapper encapsulates an Azure NSG security rule from a WNI perspective
type nsgRuleWrapper struct {
	// client is the Azure security rules client
	client network.SecurityRulesClient
	// rgName is the name of the resource group that the NSG belongs to
	rgName string
	// requiredName is the required name of the security rule
	requiredName string
	// requiredSourceAddress is the required source address in the security rule
	requiredSourceAddress *string
	// requiredDestinationPortRange are the required destination ports of the rule
	requiredDestinationPortRange string
	// requiredPriority is the rules required priority in the NSG
	requiredPriority int32
	network.SecurityRule
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
	if subscriptionID == "" {
		return nil, fmt.Errorf("empty subscriptionID for azure")
	}
	infraID, err := openShiftClient.GetInfrastructureID()
	if err != nil {
		return nil, err
	}
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
	rulesClient := getRulesClient(resourceAuthorizer, subscriptionID)
	resourcesClient := getResourcesClient(resourceAuthorizer, subscriptionID)

	requiredRules, err := constructRequiredRules(rulesClient, resourceGroupName)
	if err != nil {
		return nil, fmt.Errorf("unable to construct required rules: %v", err)
	}

	var IpName, NicName, NsgName string

	return &AzureProvider{vnetClient, vmClient, ipClient,
		subnetClient, nicClient, nsgClient, diskClient, resourcesClient, resourceAuthorizer,
		resourceGroupName, subscriptionID, infraID, IpName, NicName, NsgName,
		imageID, instanceType, resourceTrackerDir, requiredRules}, nil
}

// constructRequiredRules populates the required rules map
func constructRequiredRules(rulesClient network.SecurityRulesClient, resourceGroupName string) (map[string]*nsgRuleWrapper,
	error) {
	myIP, err := GetMyIP()
	if err != nil {
		return nil, fmt.Errorf("unable to get public IP address: %v", err)
	}

	requiredRules := make(map[string]*nsgRuleWrapper)
	requiredRules[rdpRuleName] = &nsgRuleWrapper{rulesClient, resourceGroupName, rdpRuleName, myIP, rdpPort,
		rdpRulePriority, network.SecurityRule{}}
	requiredRules[winRMRuleName] = &nsgRuleWrapper{rulesClient, resourceGroupName, winRMRuleName, myIP, winRMPort,
		winRMPortPriority, network.SecurityRule{}}
	requiredRules[vnetRuleName] = &nsgRuleWrapper{rulesClient, resourceGroupName, vnetRuleName,
		to.StringPtr("10.0.0.0/16"), vnetPorts, vnetRulePriority, network.SecurityRule{}}
	requiredRules[sshRuleName] = &nsgRuleWrapper{rulesClient, resourceGroupName, sshRuleName, myIP, strconv.Itoa(types.SshPort),
		sshRulePriority, network.SecurityRule{}}

	return requiredRules, nil
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

// getRulesClient returns the SecurityRulesClient
func getRulesClient(authorizer autorest.Authorizer, subscriptionID string) network.SecurityRulesClient {
	rulesClient := network.NewSecurityRulesClient(subscriptionID)
	rulesClient.Authorizer = authorizer
	return rulesClient
}

// getDiskClient gets the disk client by passing the authorizer token.
func getDiskClient(authorizer autorest.Authorizer, subscriptionID string) compute.DisksClient {
	diskClient := compute.NewDisksClient(subscriptionID)
	diskClient.Authorizer = authorizer
	return diskClient
}

// getResourcesClient gets the resources client by passing the authorizer token.
func getResourcesClient(authorizer autorest.Authorizer, subscriptionID string) resources.Client {
	resourcesClient := resources.NewClient(subscriptionID)
	resourcesClient.Authorizer = authorizer
	return resourcesClient
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

// GetMyIP returns the IP address string of the user
// by talking to https://checkip.azurewebsites.net/
func GetMyIP() (ip *string, err error) {
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
			Sku:      &network.PublicIPAddressSku{Name: ipSKU},
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

	// In Azure, for egress as well as for load balancer service to work on OpenShift, the
	// Windows VM is expected to be placed behind the worker nodes' load balancer.
	workerBackendAddressPools, err := az.getWorkerBackendAddressPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get worker node backend pool id: %v", err)
	}

	nicParams := network.Interface{
		Name:     to.StringPtr(az.NicName),
		Location: to.StringPtr(nodeLocation),
		InterfacePropertiesFormat: &network.InterfacePropertiesFormat{
			IPConfigurations: &[]network.InterfaceIPConfiguration{
				{
					Name: to.StringPtr(ipConfigName),
					InterfaceIPConfigurationPropertiesFormat: &network.InterfaceIPConfigurationPropertiesFormat{
						Subnet:                          &subnet,
						PrivateIPAllocationMethod:       network.Dynamic,
						PublicIPAddress:                 &ip,
						LoadBalancerBackendAddressPools: &workerBackendAddressPools,
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

// ensureSecurityGroupRules ensures that the required security group rules are present in the given NSG
func (az *AzureProvider) ensureSecurityGroupRules(ctx context.Context, nsgRules []network.SecurityRule) error {

	// Collect the existing rules
	for _, nsgRule := range nsgRules {
		switch *nsgRule.Name {
		case rdpRuleName:
			az.requiredRules[rdpRuleName].SecurityRule = nsgRule
		case winRMRuleName:
			az.requiredRules[winRMRuleName].SecurityRule = nsgRule
		case vnetRuleName:
			az.requiredRules[vnetRuleName].SecurityRule = nsgRule
		}
		if az.requiredRules[rdpRuleName].Name != nil && az.requiredRules[winRMRuleName].Name != nil &&
			az.requiredRules[vnetRuleName].Name != nil {
			break
		}
	}

	for _, rule := range az.requiredRules {
		if err := rule.createOrUpdate(ctx, az.NsgName); err != nil {
			return fmt.Errorf("unable to create or update %s/%s: %v", az.NsgName, rule.requiredName, err)
		}
	}

	return nil
}

// updateSecurityGroup updates the given NSG with the required set of security group rules
func (az *AzureProvider) updateSecurityGroup(ctx context.Context) error {
	sg, err := az.nsgClient.Get(ctx, az.resourceGroupName, az.NsgName, "")
	if err != nil {
		return fmt.Errorf("cannot obtain the security group %s: %v", az.NsgName, err)
	}

	var nsgRules []network.SecurityRule
	if !checkForNil(sg.SecurityGroupPropertiesFormat) && !checkForNil(sg.SecurityGroupPropertiesFormat.SecurityRules) {
		nsgRules = *sg.SecurityGroupPropertiesFormat.SecurityRules
	} else {
		return fmt.Errorf("cannot obtain the security group properties format for %s: %v", az.NsgName, err)
	}

	return az.ensureSecurityGroupRules(ctx, nsgRules)
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

// getWorkerBackendAddressPools gets the backend address pools of the worker node loadbalancer
func (az *AzureProvider) getWorkerBackendAddressPools(ctx context.Context) ([]network.BackendAddressPool, error) {
	// Create a filter to get a resource of type virtualMachine which contains 'worker' in its name
	filter := "resourceType eq 'Microsoft.Compute/virtualMachines' and substringof('worker', name) and " +
		"resourceGroup eq '" + az.resourceGroupName + "'"
	// number of resources to retrieve
	// https://learnk8s.io/kubernetes-node-size
	// Kubernetes can technically accommodate 5000 nodes, considering that for optimal performance we limit it at 500 worker nodes
	top := int32(500)
	// Use getResource() function to get all the workerVMs by filtering VMs which have 'worker' in their name
	// In the vmClient, they need full vmName. We can use list() but list would return all the VMs in the
	// resource group, we will then have to filter it to get the worker VMs using go code iterating through
	// all the VM names. getResource() does all that work for us. We just need to use the output in that case.
	vmResources, err := az.getResource(ctx, filter, top)
	if err != nil {
		return nil, fmt.Errorf("failed to get worker VMs: %v", err)
	}
	if vmResources == nil {
		return nil, fmt.Errorf("failed to get worker VMs resource")
	}

	// keep track of workerVM load balancer
	// using string as we are assuming all the worker VMs are behind a single load balancer
	var backendAddressPool string

	// Iterate over all the VM resources and get the worker backend pools attached to all the resources.
	for _, vmResource := range vmResources {
		if vmResource.ID == nil {
			return nil, fmt.Errorf("id for VM resource cannot be nil")
		}
		vmName := extractResourceName(*vmResource.ID)
		// get the backend address pool
		workerVMBackendAddressPool, err := az.getVMBackendAddressPool(ctx, vmName)
		if err != nil {
			return nil, fmt.Errorf("unable to get backend address pools for vm %s", vmName)
		}
		if backendAddressPool == "" {
			backendAddressPool = workerVMBackendAddressPool
		} else if backendAddressPool != workerVMBackendAddressPool {
			return nil, fmt.Errorf("multiple backend address pools for worker nodes is not supported")
		}
	}

	// create an array of the backend address pools that are attached to worker nodes
	// which will then be applied to the Windows node
	lbBackendPoolAddressIDs := []network.BackendAddressPool{
		{
			ID: &backendAddressPool,
		},
	}

	return lbBackendPoolAddressIDs, nil
}

// getVMBackendAddressPool gets the backend address pool associated with the VM
// we are assuming that all the worker VMs have only one backend address pool associated.
func (az *AzureProvider) getVMBackendAddressPool(ctx context.Context, vmName string) (string, error) {

	err, nicName := az.getNICname(ctx, vmName)
	if err != nil {
		return "", fmt.Errorf("failed to get NIC name %v", err)
	}

	_, interfaceIPConfigs, err := az.getInfoFromNIC(ctx, nicName)
	if err != nil {
		return "", fmt.Errorf("failed to get ip configuration for NIC %s: %v", nicName, err)
	}

	// get the 1st IP config attached to the worker node
	ipConf := interfaceIPConfigs[0]
	if ipConf.InterfaceIPConfigurationPropertiesFormat == nil {
		return "", fmt.Errorf("ip configuration properties for VM %s is nil", vmName)
	}
	ipConfigProp := *ipConf.InterfaceIPConfigurationPropertiesFormat
	if ipConfigProp.LoadBalancerBackendAddressPools == nil {
		return "", fmt.Errorf("load balancer backend address pools for VM %s cannot be nil", vmName)
	}
	backendPools := *ipConfigProp.LoadBalancerBackendAddressPools
	if backendPools == nil {
		return "", fmt.Errorf("backend address pool id for %s cannot be nil", vmName)
	}
	if len(backendPools) < 1 {
		return "", fmt.Errorf("backend address pools for %s cannot be 0", vmName)
	}
	if len(backendPools) > 1 {
		return "", fmt.Errorf("multiple backend address pools for %s are currently not supported", vmName)
	}
	// get the backendAddressPools attached to the IP config
	backendAddressPool := backendPools[0]

	return *backendAddressPool.ID, nil
}

// getResource gets a resource, the generic way.
func (az *AzureProvider) getResource(ctx context.Context, filter string, num int32) ([]resources.GenericResource, error) {
	list, err := az.resourcesClient.List(
		ctx,
		filter,
		"",
		&num,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources for filter %s: %v", filter, err)
	}
	return list.Values(), nil
}

// getInfoFromNIC gets the interface ip configuration from NIC name by calling the azure api
func (az *AzureProvider) getInfoFromNIC(ctx context.Context, nicName string) (network.Interface, []network.InterfaceIPConfiguration, error) {
	interfaceStruct, err := az.nicClient.Get(ctx, az.resourceGroupName, nicName, "")
	if err != nil {
		return network.Interface{}, []network.InterfaceIPConfiguration{},
			fmt.Errorf("cannot fetch the network interface data of NIC %s: %v", nicName, err)
	}

	if interfaceStruct.InterfacePropertiesFormat == nil {
		return network.Interface{}, []network.InterfaceIPConfiguration{},
			fmt.Errorf("ip configuration for NIC %s cannot be nil", nicName)
	}
	interfacePropFormat := *interfaceStruct.InterfacePropertiesFormat

	if interfacePropFormat.IPConfigurations == nil {
		return network.Interface{}, []network.InterfaceIPConfiguration{},
			fmt.Errorf("ip configuration for NIC %s cannot be nil", nicName)
	}
	interfaceIPConfigs := *interfacePropFormat.IPConfigurations

	if len(interfaceIPConfigs) < 1 {
		return network.Interface{}, []network.InterfaceIPConfiguration{},
			fmt.Errorf("ip configuration properties for NIC %s cannot be 0", nicName)
	}
	// we assume that all worker nodes have only one IP config.
	// if the worker nodes have more than one IP config, throw an error
	if len(interfaceIPConfigs) > 1 {
		return network.Interface{}, []network.InterfaceIPConfiguration{},
			fmt.Errorf(" NIC %s has multiple ip configurations. This is not supported by WNI", nicName)
	}

	return interfaceStruct, interfaceIPConfigs, nil
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

// getPublicIPAddress gets the public IP Address by IP resource name as an argument.
func (az *AzureProvider) getPublicIPAddress(ctx context.Context) (ipAddress *string, err error) {
	result, err := az.ipClient.Get(ctx, az.resourceGroupName, az.IpName, "")
	if errorCheck(err) {
		return nil, err
	}
	ipAddressProperties := *(result.PublicIPAddressPropertiesFormat)
	ipAddress = &(*(ipAddressProperties.IPAddress))
	return
}

// constructAdditionalContent constructs the commands needed to be executed on first login into the Windows node.
func constructAdditionalContent(instanceName, adminPassword string) *[]compute.AdditionalUnattendContent {
	// On first time Logon it will copy the custom file injected to a temporary directory
	// on windows node, and then it will execute the steps inside the custom script
	// which will configure winRM Https & Http listeners running on port 5986 & 5985 respectively.
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
		"<LogonCount>1</LogonCount><Enabled>true</Enabled></AutoLogon>", instanceName, winUser, adminPassword)
	additionalContent := &[]compute.AdditionalUnattendContent{
		{
			// OobeSystem is a configuration setting that is applied during the end-user first boot experience, also
			// called as Out-Of-Box experience. The configuration settings are processed before user first logon
			// to the Windows node. It is a const provided by the azure SDK module.
			PassName: "OobeSystem",
			// Microsoft-Windows-Shell-Setup contains elements and settings that control how the Windows shell need to
			// be installed. This component is selected so that AutoLogon and FirstLogonCommands settings can be used.
			// Currently the azure SDK module allows only to set up shell component.
			ComponentName: "Microsoft-Windows-Shell-Setup",
			// AutoLogon specifies credentials for an account that is used to automatically log on to the
			// windows node.
			SettingName: "AutoLogon",
			Content:     to.StringPtr(autoLogonData),
		},
		{
			PassName:      "OobeSystem",
			ComponentName: "Microsoft-Windows-Shell-Setup",
			// FirstLogonCommands specifies commands to run the first time that an end user logs on to the windows node.
			SettingName: "FirstLogonCommands",
			Content:     to.StringPtr(firstLogonData),
		},
	}
	return additionalContent
}

// constructOSProfile constructs the OS Profile for the creation of windows instance. The OS Profile consists of information
// such as configuring remote management listeners, instance access setup.
func (az *AzureProvider) constructOSProfile(ctx context.Context) (osProfile *compute.OSProfile, vmName, password string) {
	instanceName := windowsWorker + randomString(5)
	adminPassword := randomPasswordString(12)
	additionalContent := constructAdditionalContent(instanceName, adminPassword)

	// the data runs the script from the url location, script sets up both HTTP & HTTPS WinRM listeners so that
	// ansible can connect to it and run remote scripts on the windows node. Also open firewall port number 10250.
	data := `$url = "https://raw.githubusercontent.com/ansible/ansible/devel/examples/scripts/ConfigureRemotingForAnsible.ps1"
    $file = "$env:temp\ConfigureRemotingForAnsible.ps1"
    (New-Object -TypeName System.Net.WebClient).DownloadFile($url,  $file)
    & $file
    Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
    Start-Service sshd
    New-NetFirewallRule -DisplayName "` + types.FirewallRuleName + `"
    -Direction Inbound -Action Allow -Protocol TCP -LocalPort ` + types.ContainerLogsPort + ` - EdgeTraversalPolicy Allow`

	var nodeLocation string
	if !checkForNil(az.getvnetLocation(ctx)) {
		nodeLocation = *(az.getvnetLocation(ctx))
	}
	timeZoneMap := getTimeZoneMap()
	osProfile = &compute.OSProfile{
		ComputerName:  to.StringPtr(instanceName),
		AdminUsername: to.StringPtr(winUser),
		AdminPassword: to.StringPtr(adminPassword),
		CustomData:    to.StringPtr(base64.StdEncoding.EncodeToString([]byte(data))),
		WindowsConfiguration: &compute.WindowsConfiguration{
			ProvisionVMAgent:          to.BoolPtr(true),
			EnableAutomaticUpdates:    to.BoolPtr(false),
			TimeZone:                  to.StringPtr(timeZoneMap[nodeLocation]),
			AdditionalUnattendContent: additionalContent,
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

	if checkForNil(nsgStruct.ID) {
		return nil, fmt.Errorf("failed to find worker NSG")
	}

	vmRandString := strings.Split(vmName, "-")[1]
	nsgID := *(nsgStruct.ID)
	nsgName := extractResourceName(nsgID)
	az.NsgName = nsgName

	err = az.updateSecurityGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update security group rules: %v", err)
	}

	if len(az.IpName) == 0 {
		ipName := az.generateResourceName("ip", vmRandString)
		az.IpName = ipName
		_, err := az.createPublicIP(ctx)
		if errorCheck(err) {
			return nil, fmt.Errorf("failed to create IP for node: %v", err)
		}
	}

	var nic *network.Interface
	if len(az.NicName) > 0 {
		retrievedNIC, err := az.nicClient.Get(ctx, az.resourceGroupName, az.NicName, "")
		if err != nil {
			return nil, fmt.Errorf("failed to attach user provided nic for the instance: %v", err)
		}
		nic = &retrievedNIC
	} else {
		nicName := az.generateResourceName("nic", vmRandString)
		ipConfigName := az.generateResourceName("ipConfig", vmRandString)
		az.NicName = nicName
		nic, err = az.createNIC(ctx, vnetName, subnetName, nsgName, ipConfigName)
		if err != nil {
			return nil, fmt.Errorf("failed to create nic for the instance: %v", err)
		}
	}
	if nic.ID == nil {
		return nil, fmt.Errorf("nic ID is nil")
	}

	networkProfile = &compute.NetworkProfile{
		NetworkInterfaces: &[]compute.NetworkInterfaceReference{
			{
				ID: nic.ID,
				NetworkInterfaceReferenceProperties: &compute.NetworkInterfaceReferenceProperties{
					Primary: to.BoolPtr(true),
				},
			},
		},
	}
	return networkProfile, nil
}

// CreateWindowsVM takes in imageId, instanceType and sshKey name to create Windows instance under the same
// resourceGroupName as the existing OpenShift. The returned Windows VM object from this method will provide access
// to the instance via Winrm, SSH
// TODO: If it fails during the instance creation process it has to delete the resources created
// untill that step.
func (az *AzureProvider) CreateWindowsVM() (types.WindowsVM, error) {
	w := &types.Windows{}
	// Construct the VirtualMachine properties
	rand.Seed(time.Now().UnixNano())
	ctx := context.Background()
	vmHardwareProfile := &compute.HardwareProfile{
		VMSize: compute.VirtualMachineSizeTypes(az.instanceType)}
	log.Printf("constructed the HardwareProfile for node")

	vmStorageProfile := az.constructStorageProfile(az.imageID)
	log.Printf("constructed the Storage Profile for node")

	vmOSProfile, instanceName, adminPassword := az.constructOSProfile(ctx)
	log.Printf("constructed the OSProfile for the node")

	vmNetworkProfile, err := az.constructNetworkProfile(ctx, instanceName)
	if errorCheck(err) {
		return nil, err
	}
	log.Printf("constructed the network profile for the node")

	log.Printf("constructed all the profiles, about to create instance.")
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
		log.Printf("failed to obtain instance info: %s", err)
	}

	resourceTrackerFilePath, err := resource.MakeFilePath(az.resourceTrackerDir)
	if errorCheck(err) {
		log.Printf("unable to create resource file: %s", err)
	}

	err = resource.AppendInstallerInfo([]string{*(vmInfo.Name)}, []string{az.NsgName}, resourceTrackerFilePath)
	if errorCheck(err) {
		log.Printf("unable to add installer info to the resource file: %s", err)
	}

	publicIPAddress, ipErr := az.getPublicIPAddress(ctx)
	if errorCheck(ipErr) {
		log.Printf("failed to get the IP address of %s: %s", az.IpName, ipErr)
		*publicIPAddress = az.IpName
	}
	privateIPAddress, err := az.getPrivateIP(ctx)
	if err != nil {
		log.Printf("failed to get the private IP address %s: %s", *(vmInfo.Name), err)
	}

	// Build new credentials structure to be used by other actors. The actor is responsible for checking if
	// the credentials are being generated properly. This method won't guarantee the existence of credentials
	// if the VM is spun up
	credentials := types.NewCredentials(instanceName, *publicIPAddress, adminPassword, winUser)
	w.Credentials = credentials

	// Setup Winrm and SSH client so that we can interact with the Windows Object we created
	if err := w.SetupWinRMClient(); err != nil {
		return nil, fmt.Errorf("failed to setup winRM client for the Windows VM: %v", err)
	}

	// Wait for some time before starting configuring of ssh server. This is to let sshd service be available
	// in the list of services
	// TODO: Parse the output of the `Get-Service sshd, ssh-agent` on the Windows node to check if the windows nodes
	// has those services present
	time.Sleep(time.Minute)
	if err := w.GetSSHClient(); err != nil {
		return w, fmt.Errorf("failed to get ssh client for the Windows VM created: %v", err)
	}

	resultData := fmt.Sprintf("xfreerdp /u:core /v:%s /h:1080 /w:1920 /p:'%s'\n", *publicIPAddress, adminPassword)
	resultPath := az.resourceTrackerDir + instanceName
	err = resource.StoreCredentialData(resultPath, resultData)
	if errorCheck(err) {
		log.Printf("unable to write data into resource file: %s", err)
		log.Printf("xfreerdp /u:core /v:%s /h:1080 /w:1920 /p:%s \n", *publicIPAddress, adminPassword)
	} else {
		log.Printf("Please check file %s in directory %s to access the node",
			instanceName, az.resourceTrackerDir)
		log.Printf("External IP: %s", *publicIPAddress)
		log.Printf("Internal IP: %s", privateIPAddress)
	}
	return w, nil

}

// getNICname returns nicName by taking instance name as an argument.
func (az *AzureProvider) getNICname(ctx context.Context, vmName string) (err error, nicName string) {
	vmStruct, err := az.vmClient.Get(ctx, az.resourceGroupName, vmName, "instanceView")
	if err != nil {
		log.Printf("cannot fetch the instance data of %s: %s", vmName, err)
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
		log.Printf("cannot fetch the instance data of %s: %s", vmName, err)
		return
	}
	networkProfile := vmStruct.VirtualMachineProperties.NetworkProfile
	networkInterface := (*networkProfile.NetworkInterfaces)[0]
	nicID := *networkInterface.ID
	nicName := extractResourceName(nicID)
	interfaceStruct, err := az.nicClient.Get(ctx, az.resourceGroupName, nicName, "")
	if err != nil {
		log.Printf("cannot fetch the network interface data of %s: %s", vmName, err)
	}
	interfacePropFormat := *(interfaceStruct.InterfacePropertiesFormat)
	interfaceIPConfigs := *(interfacePropFormat.IPConfigurations)
	ipConfigProp := *(interfaceIPConfigs[0].InterfaceIPConfigurationPropertiesFormat)
	ipID := *(ipConfigProp.PublicIPAddress.ID)
	ipName = extractResourceName(ipID)
	return nil, ipName
}

// getPrivateIP gets the private ip address of the vm
// privateIP wont be stored in the credentials struct
func (az *AzureProvider) getPrivateIP(ctx context.Context) (string, error) {
	nicInfo, err := az.nicClient.Get(ctx, az.resourceGroupName, az.NicName, "")
	if err != nil {
		return "", fmt.Errorf("cannot get nic info: %v", err)
	}
	if nicInfo.IPConfigurations == nil {
		return "", fmt.Errorf("cannot get IP configuration for nic info: %v", err)
	}
	nicIPConf := *(nicInfo.IPConfigurations)
	if len(nicIPConf) == 0 {
		return "", fmt.Errorf("empty IP configuration for nic")
	}
	privateIp := nicIPConf[0].PrivateIPAddress
	return *privateIp, nil
}

// disassociateIP disassociates public IP with the ipConfig.
// This is needed to delete the public IP address as public IP address cannot be deleted without detaching it from
// the ipConfig.
func (az *AzureProvider) disassociateIP(ctx context.Context, nicName string) error {
	interfaceStruct, interfaceIPConfigs, err := az.getInfoFromNIC(ctx, nicName)
	if err != nil {
		return fmt.Errorf("failed to get ip configuration for NIC %s: %v", nicName, err)
	}
	// remove the ip address association by setting it as 'nil'
	interfaceIPConfigs[0].PublicIPAddress = nil

	// nicClient.CreateOrUpdate() needs update 'parameter' of type network.interface
	// here we are sending the edited interface struct with public ip address set to nil
	future, err := az.nicClient.CreateOrUpdate(ctx, az.resourceGroupName, az.NicName, interfaceStruct)
	if err != nil {
		return fmt.Errorf("failed to disassociate/detach the Public IP address from nic %s: %v ", nicName, err)
	}
	err = future.WaitForCompletionRef(ctx, az.nicClient.Client)
	if err != nil {
		return fmt.Errorf("failed to disassociate/detach the Public IP address from nic %s: %v ", nicName, err)
	}
	return nil
}

// destroyIP deletes the IP address by taking it's name as argument.
func (az *AzureProvider) destroyIP(ctx context.Context, ipName string) (err error) {
	_, err = az.ipClient.Delete(ctx, az.resourceGroupName, ipName)
	if errorCheck(err) {
		log.Printf("failed to delete the public IP: %s,%s", ipName, err)
		return
	}
	return
}

// destroyInstance deletes the instance by taking it's name as argument.
func (az *AzureProvider) destroyInstance(ctx context.Context, vmName string) (err error) {
	future, err := az.vmClient.Delete(ctx, az.resourceGroupName, vmName)
	if errorCheck(err) {
		log.Printf("failed to delete the instance: %s: %s", vmName, err)
		return
	}
	err = future.WaitForCompletionRef(ctx, az.vmClient.Client)
	if errorCheck(err) {
		log.Printf("failed to delete instance %s: %s", vmName, err)
		return
	}
	return nil
}

// destroyNIC deletes the nic by taking it's name as argument.
func (az *AzureProvider) destroyNIC(ctx context.Context, nicName string) (err error) {
	_, err = az.nicClient.Delete(ctx, az.resourceGroupName, nicName)
	if errorCheck(err) {
		log.Printf("failed to delete nic %s: %s", nicName, err)
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

// deleteNSGRules deletes the rdp, vnet and WinRM traffic rules from the worker subnet security group rules.
func (az *AzureProvider) deleteNSGRules(ctx context.Context, nsgName string) (err error) {
	_, err = az.nsgClient.Get(ctx, az.resourceGroupName, nsgName, "")
	if err != nil {
		return fmt.Errorf("cannot obtain the worker subnet security group rules: %v", err)
	}

	errMsg := ""
	for _, rule := range az.requiredRules {
		if err := rule.delete(ctx, nsgName); err != nil {
			errMsg += err.Error() + "\n"
		}
	}

	if errMsg != "" {
		err = errors.New(errMsg)
		log.Printf("unable to delete SG rules: %s", err)
	}
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
		log.Printf("failed to delete the root disk %s: %s", diskName, err)
		return
	}
	return
}

// DestroyWindowsVMWithResources destroys the Windows VMs along with all the errors associated with it.
func (az *AzureProvider) DestroyWindowsVM(vmName string) error {
	ctx := context.Background()
	vmInfo, err := az.vmClient.Get(ctx, az.resourceGroupName, vmName, compute.InstanceView)
	if err != nil {
		log.Printf("error getting vminfo for %s: %v", vmName, err)
	}
	err, nicName := az.getNICname(ctx, vmName)
	if err != nil {
		log.Printf("error getting the nic name for %s: %v", vmName, err)
	}
	err, ipName := az.getIPname(ctx, vmName)
	if err != nil {
		log.Printf("error getting the ip name for the instance %s: %v", vmName, err)
	}

	err = az.destroyInstance(ctx, vmName)
	if err != nil {
		log.Printf("error destroying the instance %s: %v", vmName, err)
	}

	err = az.disassociateIP(ctx, nicName)
	if err != nil {
		log.Printf("error disassociating the Public IP of instance %s: %v", vmName, err)
	}

	err = az.destroyNIC(ctx, nicName)
	if err != nil {
		log.Printf("error deleting the NIC of instance %s: %v", vmName, err)
	}

	err = az.destroyIP(ctx, ipName)
	if err != nil {
		log.Printf("error deleting the IP of instance %s: %v", vmName, err)
	}

	// This may still leak resources as we'll not be able to get vm info once the VM gets deleted.
	err = az.destroyDisk(ctx, vmInfo)
	if err != nil {
		log.Printf("error deleting the disk attached to the instance %s: %v", vmName, err)
	}
	return nil
}

// DestroyWindowsVMs destroys all the resources created by the CreateWindows method.
func (az *AzureProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file
	log.Printf("processing file '%s'", az.resourceTrackerDir)
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
		log.Printf("deleting the resources associated with instance %s", vmName)
		az.DestroyWindowsVM(vmName)

		rdpFilePath = az.resourceTrackerDir + vmName
		err = resource.DeleteCredentialData(rdpFilePath)
		if errorCheck(err) {
			log.Printf("unable to remove file %s: %s", rdpFilePath, err)
		}

		terminatedInstances = append(terminatedInstances, vmName)
	}

	for _, nsgName := range installerInfo.SecurityGroupIDs {
		err = az.deleteNSGRules(ctx, nsgName)
		if !errorCheck(err) {
			log.Printf("deleted the created security group rules in worker subnet")
		}

		deletedSg = append(deletedSg, nsgName)
	}

	// Update the 'windows-node-installer.json' file
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, resourceTrackerFilePath)
	if errorCheck(err) {
		log.Printf("%s file was not updated: %v", resourceTrackerFilePath, err)
	}
	return nil
}

// populateSecurityRule populates the SecurityRule struct with required values
func (n *nsgRuleWrapper) populateSecurityRule() {
	n.SecurityRule = network.SecurityRule{
		Name: to.StringPtr(n.requiredName),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Protocol:                 network.SecurityRuleProtocolTCP,
			SourcePortRange:          to.StringPtr(sourcePortRange),
			SourceAddressPrefixes:    &[]string{*n.requiredSourceAddress},
			DestinationAddressPrefix: to.StringPtr(destinationAddressPrefix),
			DestinationPortRange:     to.StringPtr(n.requiredDestinationPortRange),
			Access:                   network.SecurityRuleAccessAllow,
			Direction:                network.SecurityRuleDirectionInbound,
			Priority:                 to.Int32Ptr(n.requiredPriority),
		},
	}
}

// createOrUpdate creates or updates the security rule with the required information present in the struct
func (n *nsgRuleWrapper) createOrUpdate(ctx context.Context, nsgName string) error {
	// This implies that the security rule was not present and needs to be created
	if n.Name == nil || n.SourceAddressPrefixes == nil {
		n.populateSecurityRule()
	} else {
		// Check if the sourceAddress is present
		for _, sourceAddress := range *n.SourceAddressPrefixes {
			// sourceAddress is already present in rule, so there is no need to update
			if sourceAddress == *n.requiredSourceAddress {
				return nil
			}
		}
		*n.SourceAddressPrefixes = append(*n.SourceAddressPrefixes, *n.requiredSourceAddress)
	}

	future, err := n.client.CreateOrUpdate(ctx, n.rgName, nsgName, n.requiredName, n.SecurityRule)
	if err != nil {
		return err
	}
	err = future.WaitForCompletionRef(ctx, n.client.Client)
	if err != nil {
		return err
	}
	_, err = future.Result(n.client)
	return err
}

// delete deletes the rule from the given NSG
func (n *nsgRuleWrapper) delete(ctx context.Context, nsgName string) error {
	future, err := n.client.Delete(ctx, n.rgName, nsgName, n.requiredName)
	if err != nil {
		return err
	}
	err = future.WaitForCompletionRef(ctx, n.client.Client)
	if err != nil {
		return err
	}
	_, err = future.Result(n.client)
	return err
}
