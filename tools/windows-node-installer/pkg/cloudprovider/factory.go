package cloudprovider

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openshift/api/config/v1"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/aws"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"k8s.io/client-go/util/homedir"
)

// Cloud is the interface that needs to be implemented per provider to allow support for creating Windows nodes on
// that provider.
type Cloud interface {
	// CreateWindowsVM creates a Windows VM for a given cloud provider
	// TODO: CreateWindowsVM should return a provider object for further interaction with the created instance.
	CreateWindowsVM() error
	// DestroyWindowsVMs uses 'windows-node-installer.json' file that contains IDs of created instance and
	// security group and deletes them.
	// Example 'windows-node-installer.json' file:
	// {
	//	"InstanceIDs": ["<example-instance-ID>"],
	//  "SecurityGroupIDs": ["<example-security-group-ID>"]
	// {
	// It deletes the security group only if the group is not associated with any instance.
	// The association between the instance and security group are available from individual cloud provider.
	DestroyWindowsVMs() error
}

// CloudProviderFactory returns cloud specific interface for performing necessary functions related to creating or
// destroying an instance.
// The factory takes in kubeconfig of an existing OpenShift cluster and a cloud vendor specific credential file.
// Since the credential file may contain multiple accounts and the default account name/ID varies between providers,
// this function requires specifying the credentialAccountID of the user's credential account.
// The resourceTrackerDir is where the `windows-node-installer.json` file which contains IDs of created instance and
// security group will be created.
func CloudProviderFactory(kubeconfigPath, credentialPath, credentialAccountID, resourceTrackerDir,
	imageID, instanceType, sshKey string) (Cloud, error) {
	// File, dir, credential account sanity checks.
	kubeconfigPath, err := makeValidAbsPath(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("error resolving path for kubeconfig file, %v", err)
	}
	credentialPath, err = makeValidAbsPath(credentialPath)
	if err != nil {
		return nil, fmt.Errorf("error resolving path for credentials file, %v", err)
	}
	resourceTrackerDir, err = makeValidAbsPath(resourceTrackerDir)
	if err != nil {
		return nil, fmt.Errorf("error resolving path for resource tracker directory, %v", err)
	}

	// Create a new client of the given OpenShift cluster based on kubeconfig.
	oc, err := client.GetOpenShift(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cloudProvider, err := oc.GetCloudProvider()
	if err != nil {
		return nil, err
	}
	resourceTrackerFilePath, err := resource.MakeFilePath(resourceTrackerDir)
	if err != nil {
		return nil, err
	}

	switch provider := cloudProvider.Type; provider {
	case v1.AWSPlatformType:
		return aws.New(oc, imageID, instanceType, sshKey, credentialPath, credentialAccountID, resourceTrackerFilePath)
	default:
		return nil, fmt.Errorf("the '%v' cloud provider is not supported", provider)
	}
}

// makeValidAbsPath remakes a path into an absolute path and ensures that it exists.
// TODO: Break this function to validate files. dirs etc. As of now, we don't differentiate
// between files and dirs
func makeValidAbsPath(path string) (string, error) {
	if len(path) > 0 && !filepath.IsAbs(path) {
		// Expand `~` to `/home` directory of the user
		// TODO: remove dependency on `homedir` package from kubernetes repo
		if path[0] == '~' {
			path = filepath.Join(homedir.HomeDir(), path[1:])
		}
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	file, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("path %s does not exist", path)
	}
	if file.IsDir() {
		// Add a trailing slash if it doesn't exist only for directories
		if path[len(path)-1:] != "/" {
			path = path + "/"
		}
	}
	return path, nil
}
