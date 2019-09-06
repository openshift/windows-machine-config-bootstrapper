package cloudprovider

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
)

// CloudProviderFactory returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// Since the credential file may contain multiple accounts and the default account name/ID varies between providers,
// this function requires specifying the credentialAccountID of the user's credential account.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.

//struct to hold azure specific information.
type svc struct {
	resourceGroupName     string // name of the resource group
	resourceGroupLocation string // name of the resource location

	deploymentName string                 // name of the deployment
	templateFile   map[string]interface{} // name of the templateFile (need from user)
	parametersFile map[string]interface{} // name of the parametersFile (need from user)
	authdata       map[string]interface{} // Azure auth data
}

func readJSON(path string) (map[string]interface{}, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal("failed to read file: %v", err)
	}
	contents := make(map[string]interface{})
	json.Unmarshal(data, &contents)
	return contents, nil
}

func CloudProviderFactory(
	kubeconfigPath,
	credentialPath,
	credentialAccountID,
	resourceTrackerDir string) (Cloud, error) {
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
	case "Azure":
		//Gather the azure related information.
		//Read the .auth file to get the subscrptionID and other details.
		data, err := readJSON("azureauth_location")
		template, err := readJSON("windowsnode-template.json")
		if err != nil {
			log.Fatal("Unable to read template file")
			return nil, err
		}
		params, err := readJSON("windowsnode-params.json")
		if err != nil {
			log.Fatal("Unable to read parameters file")
			return nil, err
		}
		azureinfo := svc{resourceGroupName: cloudProvider.Azure.ResourceGroupName,
			resourceGroupLocation: "centralus", deploymentName: "windowsNode",
			templateFile: template, parametersFile: params, authdata: data}
		return nil, err

	default:
		return nil, fmt.Errorf("the '%v' cloud provider is not supported", provider)
	}
}
