package cloudprovider

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
)

// Cloud is the interface that needs to be implemented per provider to allow support for creating Windows nodes on
// that provider.
type Cloud interface {
	// CreateWindowsVM creates a Windows VM based on available image id, instance type, and ssh key name.
	// TODO: CreateWindowsVM should return a provider object for further interaction with the created instance.
	CreateWindowsVM(imageId, instanceType, sshKey string) error
	// DestroyWindowsVM uses 'windows-node-installer.json' file that contains IDs of created instance and
	// security group and deletes them.
	// Example 'windows-node-installer.json' file:
	// {
	//	"InstanceIDs": ["<example-instance-ID>"],
	//  "SecurityGroupIDs": ["<example-security-group-ID>"]
	// {
	// It deletes the security group only if the group is not associated with any instance.
	// The association between the instance and security group are available from individual cloud provider.
	DestroyWindowsVM() error
}

// CloudProviderFactory returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// Since the credential file may contain multiple accounts and the default account name/ID varies between providers,
// this function requires specifying the credentialAccountID of the user's credential account.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func CloudProviderFactory(kubeconfigPath, credentialPath, credentialAccountID, resourceTrackerDir string) (Cloud, error) {
	// Create a new client of the given OpenShift cluster based on kubeconfig.
	oc, err := client.GetOpenShift(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cloudProvider, err := oc.GetCloudProvider()
	if err != nil {
		return nil, err
	}

	switch provider := cloudProvider.Type; provider {
	default:
		return nil, fmt.Errorf("the '%v' cloud provider is not supported", provider)
	}
}
