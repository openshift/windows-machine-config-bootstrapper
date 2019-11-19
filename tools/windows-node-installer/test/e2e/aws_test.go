package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	awscp "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/aws"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// Get kubeconfig, AWS credentials, and artifact dir from environment variable set by the OpenShift CI operator.
	kubeconfig     = os.Getenv("KUBECONFIG")
	awscredentials = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	dir            = os.Getenv("ARTIFACT_DIR")
	privateKeyPath = os.Getenv("KUBE_SSH_KEY_PATH")

	// The CI-operator uses AWS region `us-east-1` which has the corresponding image ID: ami-0b8d82dea356226d3 for
	// Microsoft Windows Server 2019 Base with Containers.
	imageID      = "ami-0b8d82dea356226d3"
	instanceType = "m4.large"
	sshKey       = "libra"

	// awsProvider is setup as a variable for both creating, destroying,
	// and tear down Windows instance in case test fails in the middle.
	awsProvider = &awscp.AwsProvider{}

	// Set global variables for instance object, instance, security group,
	// and infrastructure IDs so that once they are created,
	// they will be used by all subsequent testing functions.
	createdInstance   = &ec2.Instance{}
	createdInstanceID = ""
	createdSgID       = ""
	infraID           = ""
)

// TestAwsE2eSerial runs all e2e tests for the AWS implementation serially. It creates the Windows instance,
// checks all properties of the instance and destroy the instance and check that resource are deleted.
func TestAwsE2eSerial(t *testing.T) {
	setupAWSCloudProvider(t)

	t.Run("test create Windows Instance", testCreateWindowsInstance)

	t.Run("test destroy Windows instance", testDestroyWindowsInstance)
}

// testCreateWindowsInstance tests the creation of a Windows instance and checks its properties and attached items.
func testCreateWindowsInstance(t *testing.T) {
	setupWindowsInstanceWithResources(t)

	t.Run("test created instance properties", testInstanceProperties)
	t.Run("test instance is attached a public subnet", testInstanceHasPublicSubnetAndIp)
	t.Run("test instance has name tag", testInstanceIsAttachedWithName)
	t.Run("test instance has infrastructure tag", testInstanceInfraTagExists)
	t.Run("test instance is attached the cluster worker's security group", testInstanceHasClusterWorkerSg)
	t.Run("test instance is attache a Windows security group", testInstanceIsAssociatedWithWindowsWorkerSg)
	t.Run("test instance is associated with cluster worker's IAM", testInstanceIsAssociatedWithClusterWorkerIAM)
}

// testDestroyWindowsInstance tests the deletion of a Windows instance and checks if the created instance and Windows
// security group are deleted.
func testDestroyWindowsInstance(t *testing.T) {
	t.Run("test instance is terminated", destroyingWindowsInstance)
	t.Run("test Windows security group is deleted", testSgIsDeleted)
	t.Run("test installer json file is deleted", testInstallerJsonFileIsDeleted)
}

// setupAWSCloudProvider creates provider ofr Cloud interface and asserts type into AWS provider.
// This is the first step of the e2e test and fails the test upon error.
func setupAWSCloudProvider(t *testing.T) {
	// The e2e test uses Microsoft Windows Server 2019 Base with Containers image, m4.large type, and libra ssh key.
	cloud, err := cloudprovider.CloudProviderFactory(kubeconfig, awscredentials, "default", dir,
		imageID, instanceType, sshKey, privateKeyPath)
	require.NoError(t, err, "Error obtaining aws interface object")
	credentials, err := cloud.CreateWindowsVM()
	require.NoError(t, err, "Error spinning up Windows VM")
	require.NotEmpty(t, credentials, "Credentials returned are empty")
	require.NotEmpty(t, credentials.GetPassword(), "Expected some password but got empty string")
	require.NotEmpty(t, credentials.GetInstanceId(), "Expected some instance id but got empty string")

	// Type assert to AWS so that we can test other functionality
	provider, ok := cloud.(*awscp.AwsProvider)
	assert.True(t, ok, "Error asserting cloudprovider to awsProvider")
	awsProvider = provider
}

// setupWindowsInstanceWithResources creates a Windows instance and updates global information for infraID,
// createdInstanceID, and createdSgID. All information updates are required to be successful or instance will be
// teared down.
func setupWindowsInstanceWithResources(t *testing.T) {
	credentials, err := awsProvider.CreateWindowsVM()
	if err != nil {
		tearDownInstance(t, "error creating Windows instance", err)
	}

	infraID, err = awsProvider.GetInfraID()
	if err != nil {
		tearDownInstance(t, "error while getting infrastructure ID for the OpenShift cluster", err)
	}
	require.NotEmpty(t, credentials, "Credentials returned are empty")
	require.NotEmpty(t, credentials.GetPassword(), "Expected some password but got empty string")
	require.NotEmpty(t, credentials.GetInstanceId(), "Expected instanceID to be present but got empty string")

	// Check instance and security group information in windows-node-installer.json.
	info, err := resource.ReadInstallerInfo(dir + "/" + "windows-node-installer.json")
	if err != nil {
		tearDownInstance(t, "error reading from windows-node-installer.json file", err)
	}

	if len(info.SecurityGroupIDs) > 0 && info.SecurityGroupIDs[0] != "" {
		createdSgID = info.SecurityGroupIDs[0]
	} else {
		tearDownInstance(t, "security group ID information is empty", err)
	}

	if len(info.SecurityGroupIDs) > 0 && info.SecurityGroupIDs[0] != "" {
		createdInstanceID = info.InstanceIDs[0]
	} else {
		tearDownInstance(t, "instance ID information is empty", err)
	}
}

// getInstance gets the instance information from AWS based on instance ID and returns errors if fails.
func getInstance(instanceID string) (*ec2.Instance, error) {
	instances, err := awsProvider.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})

	if err != nil {
		return nil, err
	}
	if len(instances.Reservations) < 1 || len(instances.Reservations[0].Instances) < 1 {
		return nil, fmt.Errorf("instance does not exist")
	}
	return instances.Reservations[0].Instances[0], err
}

// tearDownInstance removes the lingering resources including instance and Windows security group when required steps of
// the test fail.
func tearDownInstance(t *testing.T, Msg string, terr error) {
	t.Logf("%s, tearing down lingering resources", Msg)

	if createdInstanceID != "" {
		err := awsProvider.TerminateInstance(createdInstanceID)
		if err != nil {
			t.Errorf("error terminating instance during teardown, %v", err)
		}
	}

	if createdSgID != "" {
		err := awsProvider.DeleteSG(createdSgID)
		if err != nil {
			t.Errorf("error terminating instance during teardown, %v", err)
		}
	}
	assert.FailNow(t, terr.Error(), Msg)
}

// testInstanceProperties updates the createdInstance global object and asserts if an instance is in the running
// state, has the right image id, instance type, and ssh key associated.
func testInstanceProperties(t *testing.T) {
	instance, err := getInstance(createdInstanceID)
	if err != nil {
		tearDownInstance(t, "error getting ec2 instance object after creation", err)
	} else {
		createdInstance = instance
	}
	assert.Equal(t, ec2.InstanceStateNameRunning, *createdInstance.State.Name,
		"created instance is not in running state")

	assert.Equalf(t, imageID, *createdInstance.ImageId, "created instance image ID mismatch")

	assert.Equalf(t, instanceType, *createdInstance.InstanceType, "created instance type mismatch")

	assert.Equalf(t, sshKey, *createdInstance.KeyName, "created instance ssh key mismatch")
}

// testInstanceHasPublicSubnetAndIp asserts if the instance is associated with a public IP address and  subnet by
// checking if the subnet routing table has internet gateway attached.
func testInstanceHasPublicSubnetAndIp(t *testing.T) {
	assert.NotEmpty(t, createdInstance.PublicIpAddress, "instance does not have a public IP address")

	routeTables, err := awsProvider.EC2.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("association.subnet-id"),
				Values: []*string{createdInstance.SubnetId},
			},
		},
	})
	if err != nil || len(routeTables.RouteTables) < 1 {
		assert.Fail(t, fmt.Sprintf("error finding route table for subnet %s, %v", *createdInstance.SubnetId, err))
		return
	}

	for _, route := range routeTables.RouteTables[0].Routes {
		igws, err := awsProvider.EC2.DescribeInternetGateways(&ec2.DescribeInternetGatewaysInput{
			InternetGatewayIds: []*string{route.GatewayId},
		})
		if err == nil && len(igws.InternetGateways) > 0 {
			return
		}
	}
	assert.Fail(t, "subnet associated is not a public subnet")
}

// testInstanceIsAttachedWithName asserts if an instance has a Name tag value.
// Instance input needs to be updated before use.
func testInstanceIsAttachedWithName(t *testing.T) {
	for _, tag := range createdInstance.Tags {
		if *tag.Key == "Name" && tag.Value != nil {
			return
		}
	}
	assert.Fail(t, "instance is not assigned a name")
}

// testInstanceInfraTagExists asserts if the infrastructure tag exists on the created instance.
// Instance input needs to be updated before use.
func testInstanceInfraTagExists(t *testing.T) {
	key := "kubernetes.io/cluster/" + infraID
	value := "owned"
	for _, tag := range createdInstance.Tags {
		if *tag.Key == key && *tag.Value == value {
			return
		}
	}
	assert.Fail(t, "infrastructure tag not found")
}

// testInstanceHasClusterWorkerSg asserts if the created instance has OpenShift cluster worker security group attached.
func testInstanceHasClusterWorkerSg(t *testing.T) {
	workerSg, err := awsProvider.GetClusterWorkerSGID(infraID)
	assert.NoError(t, err, "failed to get OpenShift cluster worker security group")

	for _, sg := range createdInstance.SecurityGroups {
		if *sg.GroupId == workerSg {
			return
		}
	}
	assert.Fail(t, "instance is not associated with OpenShift cluster worker security group")
}

// testInstanceIsAssociatedWithWindowsWorkerSg asserts if the created instance has a security group made for the
// Windows instance attached by checking the group name, recorded id, necessary ports, and ip-permission.cidr.
func testInstanceIsAssociatedWithWindowsWorkerSg(t *testing.T) {
	myIp, err := awscp.GetMyIp()
	assert.NoError(t, err, "error getting user's public IP")

	vpc, err := awsProvider.GetVPCByInfrastructure(infraID)
	if err != nil {
		assert.Fail(t, "error getting OpenShift cluster VPC, %v", err)
		return
	}

	sgs, err := awsProvider.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("ip-permission.from-port"),
				Values: aws.StringSlice([]string{"-1", "3389"}),
			},
			{
				Name:   aws.String("ip-permission.to-port"),
				Values: aws.StringSlice([]string{"-1", "3389"}),
			},
			{
				Name:   aws.String("ip-permission.from-port"),
				Values: aws.StringSlice([]string{"-1", "22"}),
			},
			{
				Name:   aws.String("ip-permission.to-port"),
				Values: aws.StringSlice([]string{"-1", "22"}),
			},
			{
				Name:   aws.String("ip-permission.protocol"),
				Values: aws.StringSlice([]string{"tcp"}),
			},
			{
				Name:   aws.String("group-id"),
				Values: aws.StringSlice([]string{createdSgID}),
			},
			{
				Name:   aws.String("ip-permission.cidr"),
				Values: aws.StringSlice([]string{myIp + "/32", *vpc.CidrBlock}),
			},
		},
	})
	if err != nil || len(sgs.SecurityGroups) < 1 {
		assert.Fail(t, "instance is not associated with a Windows security group, %v", err)
	}
}

// testInstanceIsAssociatedWithClusterWorkerIAM asserts if the created instance has the OpenShift cluster worker's IAM
// attached.
func testInstanceIsAssociatedWithClusterWorkerIAM(t *testing.T) {
	iamProfile, err := awsProvider.GetIAMWorkerRole(infraID)
	assert.NoError(t, err, "error getting OpenShift Cluster Worker IAM")

	assert.Equal(t, *iamProfile.Arn, *createdInstance.IamInstanceProfile.Arn, "instance is not associated with worker IAM profile")
}

// destroyingWindowsInstance destroys Windows instance and updates the createdInstance global object.
func destroyingWindowsInstance(t *testing.T) {
	err := awsProvider.DestroyWindowsVMs()
	if err != nil {
		tearDownInstance(t, "error destroying Windows instance", err)
	}

	createdInstance, err = getInstance(createdInstanceID)
	if err != nil {
		tearDownInstance(t, "error getting ec2 instance object after termination", err)
	} else {
		createdInstanceID = ""
	}

	assert.Equal(t, ec2.InstanceStateNameTerminated, *createdInstance.State.Name,
		"instance is not in the terminated state")
}

// testSgIsDeleted asserts if a security group is deleted by checking whether the security group exist on AWS.
// If delete is successful, the id in createdSgID is erased.
func testSgIsDeleted(t *testing.T) {
	sgs, err := awsProvider.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: aws.StringSlice([]string{createdSgID}),
	})

	if err == nil && len(sgs.SecurityGroups) > 0 {
		assert.Fail(t, "security group is not deleted")
	} else {
		createdSgID = ""
	}
}

// testInstallerJsonFileIsDeleted asserts that the windows-node-installer.json is deleted.
func testInstallerJsonFileIsDeleted(t *testing.T) {
	// the windows-node-installer.json should be removed after resource is deleted.
	_, err := resource.ReadInstallerInfo(dir)
	assert.Error(t, err, "error deleting windows-node-installer.json file")
}
