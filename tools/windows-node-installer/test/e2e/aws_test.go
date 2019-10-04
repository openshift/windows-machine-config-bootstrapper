package e2e

import (
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

// TestCreateDestroyWindowsInstance creates and terminates Windows instance on AWS.
// TODO: After creation, check for the following properties on the instance:
// - instance exist
// - in running state
// - has public subnet
// - Public IP
// - OpenShift cluster Worker SG
// - Windows SG
// - OpenShift cluster IAM
// - has name
// - has OpenShift cluster vpc
// - instance and SG IDs are recorded to the json file
// After termination, it checks for the following:
// - instance terminated
// - Windows SG deleted
// - json file deleted
func TestCreateDestroyWindowsInstance(t *testing.T) {
	// Get kubeconfig, AWS credentials, and artifact dir from environment variable set by the OpenShift CI operator.
	kubeconfig := os.Getenv("KUBECONFIG")
	awscredentials := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	dir := os.Getenv("ARTIFACT_DIR")

	// The e2e test uses Microsoft Windows Server 2019 Base with Containers image, m4.large type, and libra ssh key.
	// The CI-operator uses AWS region `us-east-1` which has the corresponding image ID: ami-0b8d82dea356226d3 for
	// Microsoft Windows Server 2019 Base with Containers.
	awsCloud, err := cloudprovider.CloudProviderFactory(kubeconfig, awscredentials, "default", dir,
		"ami-0b8d82dea356226d3", "m4.large", "libra")
	assert.NoError(t, err, "error creating clients")

	err = awsCloud.CreateWindowsVM()
	assert.NoError(t, err, "error creating Windows instance")

	// Check instance and security group information in windows-node-installer.json.
	info, err := resource.ReadInstallerInfo(dir + "/" + "windows-node-installer.json")
	assert.NoError(t, err, "error reading windows-node-installer.json file")
	assert.NotEmpty(t, info.SecurityGroupIDs, "security group is not created")
	assert.NotEmpty(t, info.InstanceIDs, "instance is not created")

	err = awsCloud.DestroyWindowsVMs()
	assert.NoError(t, err, "error deleting instance")

	// the windows-node-installer.json should be removed after resource is deleted.
	_, err = resource.ReadInstallerInfo(dir)
	assert.Error(t, err, "error deleting windows-node-installer.json file")
}
