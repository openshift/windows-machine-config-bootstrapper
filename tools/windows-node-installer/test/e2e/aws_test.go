package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	amazonaws "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	awscp "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/aws"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
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

	instanceId := info.InstanceIDs[0]
	sgId := info.SecurityGroupIDs[0]

	// Create new client session and wait for instance status ok
	sess, err := session.NewSession(&amazonaws.Config{
		Region: amazonaws.String("us-east-1")},
	)
	assert.NoErrorf(t, err, "Couldn't create new aws session: %s", err)
	svc := ec2.New(sess)
	err = svc.WaitUntilSystemStatusOk(&ec2.DescribeInstanceStatusInput{
		InstanceIds: amazonaws.StringSlice([]string{instanceId}),
	})
	assert.NoErrorf(t, err, "fail to wait for instance launch: %s", err)

	// Test winrm port and ansible connection
	testWinrmPort(t, sgId, svc)
	testWinrmAnsible(t, instanceId, svc)

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

// testWinrmPort checks the security group of the instance and verify that the
// winrm port are in security group inbound rule so that the
// instance is able to listen to winrm request.
func testWinrmPort(t *testing.T, sgId string, svc *ec2.EC2) {
	// Testing winrm port
	SgResult, err := svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{
			amazonaws.String(sgId),
		},
	})
	assert.Nilf(t, err, "Couldn't retirve Security Group: %s", err)

	// Verify winrm port and protocol are in the inbound rule of the security group
	portOpen := false
	for _, rule := range SgResult.SecurityGroups[0].IpPermissions {
		if rule.FromPort != nil && *rule.FromPort == 5986 {
			portOpen = true
		}
	}
	if !portOpen {
		t.Fatalf("winrm port is missing in new launched instance.")
	}
}

// testWinrmAnsible verifies the connection of Windows instance using Ansible ping
// module. If the module executed successfully, which means the Windows instance
// is ready for Ansible communication.
func testWinrmAnsible(t *testing.T, instanceId string, svc *ec2.EC2) {
	// Getting instance password
	pwData, err := svc.GetPasswordData(&ec2.GetPasswordDataInput{
		InstanceId: amazonaws.String(instanceId),
	})
	if err != nil {
		assert.NoErrorf(t, err, "fail to get instance password: %s", err)
	}

	// Decypt password
	pwDecoded, err := base64.StdEncoding.DecodeString(strings.Trim(*pwData.PasswordData, "\r\n"))
	if err != nil {
		assert.NoErrorf(t, err, "fail to decode password in base64: %s", err)
	}
	privateKey, err := ioutil.ReadFile(os.Getenv("KUBE_SSH_KEY_PATH"))
	privateKeyByte := []byte(privateKey)
	decryptedPasswordByte, err := rsaDecode(t, pwDecoded, privateKeyByte)
	if err != nil {
		assert.NoErrorf(t, err, "fail to decrypt password: %s", err)
	}
	decryptedPassword := string(decryptedPasswordByte)

	// Getting instance ip address
	result, err := svc.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			amazonaws.String(instanceId),
		},
	})
	assert.NoErrorf(t, err, "Couldn't DescribeInstances: %s", err)
	ipAddress := *result.Reservations[0].Instances[0].PublicIpAddress
	ipAddressArg := ipAddress + ","

	// Test winrm Ansible connection
	extraVars := fmt.Sprintf("ansible_user=Administrator ansible_password='%s' ansible_connection=winrm ansible_ssh_port=5986 ansible_winrm_server_cert_validation=ignore", decryptedPassword)
	cmdAnsible := exec.Command("ansible", "all", "-i", ipAddressArg, "-e", extraVars, "-m", "win_ping", "-vvvvv")
	cmdAnsible.Stdout = os.Stdout
	cmdAnsible.Stderr = os.Stderr
	err = cmdAnsible.Run()
	if err != nil {
		t.Fatalf("ansible is not able to communicate with Windows instance.")
	}
}

// rsaDecode decrypted the encrypted password and return the original key phrase
func rsaDecode(t *testing.T, encryptedData []byte, privateKeyByte []byte) ([]byte, error) {
	privateKeyBlock, _ := pem.Decode(privateKeyByte)
	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	assert.NoErrorf(t, err, "failed to parse private key: %s", err)
	return rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedData)
}
