package cloudprovider

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
)

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
