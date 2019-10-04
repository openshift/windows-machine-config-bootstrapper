package e2e

import (
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	awscp "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/aws"
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
	cloud, err := cloudprovider.CloudProviderFactory(kubeconfig, awscredentials, "default", dir,
		"ami-0b8d82dea356226d3", "m4.large", "libra")
	assert.NoError(t, err, "error creating clients")

	err = cloud.CreateWindowsVM()
	assert.NoError(t, err, "error creating Windows instance")
	if err != nil {
		// TODO: Remove the lingering resources as part of test setup.
		t.Fatalf("Error while creating a VM delete the virtual machine")
	}
	// Type Assert to AWS so that we can test other functionality
	aws, ok := cloud.(*awscp.AwsProvider)
	if !ok {
		t.Fatal("Error asserting cloudprovider to aws")
	}
	// Check instance and security group information in windows-node-installer.json.
	info, err := resource.ReadInstallerInfo(dir + "/" + "windows-node-installer.json")
	assert.NoError(t, err, "error reading windows-node-installer.json file")
	assert.NotEmpty(t, info.SecurityGroupIDs, "security group is not created")
	assert.NotEmpty(t, info.InstanceIDs, "instance is not created")
	instance, err := aws.GetInstance(info.InstanceIDs[0])
	if err != nil {
		t.Fatalf("error getting ec2 instance object from instance ID %v", info.InstanceIDs[0])
	}
	infraID, err := aws.GetInfraID()
	if err != nil {
		t.Fatalf("error while getting infrastructure ID for the given OpenShift cluster")
	}
	if !checkIfInfraTagExists(instance, infraID) {
		t.Fatalf("Infrastructure tag not available for %v", info.InstanceIDs[0])
	}
	err = cloud.DestroyWindowsVMs()
	assert.NoError(t, err, "error deleting instance")

	// the windows-node-installer.json should be removed after resource is deleted.
	_, err = resource.ReadInstallerInfo(dir)
	assert.Error(t, err, "error deleting windows-node-installer.json file")
}

// checkIfInfraTagExists checks if the infrastructure tag exists on the created instance
func checkIfInfraTagExists(instance *ec2.Instance, infraID string) bool {
	if instance == nil {
		return false
	}
	key := "kubernetes.io/cluster/" + infraID
	value := "owned"
	for _, tag := range instance.Tags {
		if *tag.Key == key && *tag.Value == value {
			return true
		}
	}
	return false
}
