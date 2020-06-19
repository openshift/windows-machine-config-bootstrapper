package aws

import (
	"bytes"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/resource"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	infraIDTagKeyPrefix = "kubernetes.io/cluster/"
	infraIDTagValue     = "owned"
	// durationFactor is amount of time to be multiplied with before each call to AWS api to get password for the
	// Windows VM created
	durationFactor = 10 * time.Second
	// awsPasswordDataTimeout is the maximum amount of time we can wait before password is available
	// for the Windows node created. From AWS docs:
	// We recommend that you wait up to 15 minutes
	//  after launching an instance before trying to retrieve the generated password.`
	// Ref: https://godoc.org/github.com/aws/aws-sdk-go/service/ec2#EC2.GetPasswordData
	awsPasswordDataTimeOut = 15 * time.Minute
	//RDP port for requests
	rdpPort = 3389
)

// Constant value
const (
	// Winrm port for https request
	WINRM_PORT = 5986
	// winUser is the user used to login into the instance
	winUser = "Administrator"
)

// awsProvider is a provider specific struct which contains clients for EC2, IAM, and the existing OpenShift
// cluster that is running on EC2.
// This is an implementation of the Cloud interface.
// TODO: Move this into top level `pkg/types` so that we will have all the types needed across all cloud providers
// instead of relying on importing individual packages
type AwsProvider struct {
	// imageID is the AMI image-id to be used for creating Virtual Machine
	imageID string
	// instanceType is the flavor of VM to be used
	instanceType string
	// sshKey is the ssh key to access the VM created. Please note that key should be uploaded to AWS before
	// using this flag. AWS encrypts the password of the Windows instance created with this public key
	sshKey string
	// A client for EC2.
	EC2 ec2iface.EC2API
	// A client for IAM.
	IAM *iam.IAM
	// openShiftClient is the client of the existing OpenShift cluster.
	openShiftClient *client.OpenShift
	// resourceTrackerDir is where `windows-node-installer.json` file is stored.
	resourceTrackerDir string
	// privateKeyPath is the location of the private key on the machine for the public key uploaded to AWS
	// This is used to decrypt the password for the Windows locally
	privateKeyPath string
}

// New returns the AWS implementations of the Cloud interface with AWS session in the same region as OpenShift Cluster.
// credentialPath is the file path the AWS credentials file.
// credentialAccountID is the account name the user uses to create VM instance.
// The credentialAccountID should exist in the AWS credentials file pointing at one specific credential.
// resourceTrackerDir is where created instance and security group information is stored.
// privateKeyPath is the path to private key which is used to decrypt the password for the Windows VM created
func New(openShiftClient *client.OpenShift, imageID, instanceType, sshKey, credentialPath, credentialAccountID,
	resourceTrackerDir, privateKeyPath string) (*AwsProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if err != nil {
		return nil, err
	}
	session, err := newSession(credentialPath, credentialAccountID, provider.AWS.Region)
	if err != nil {
		return nil, fmt.Errorf("could not create new AWS session: %v", err)
	}
	return &AwsProvider{imageID, instanceType, sshKey,
		ec2.New(session, aws.NewConfig()),
		iam.New(session, aws.NewConfig()),
		openShiftClient,
		resourceTrackerDir,
		privateKeyPath,
	}, nil
}

// newSession uses AWS credentials to create and returns a session for interacting with EC2.
func newSession(credentialPath, credentialAccountID, region string) (*awssession.Session, error) {
	if _, err := os.Stat(credentialPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to find AWS credentials from path '%v'", credentialPath)
	}
	return awssession.NewSession(&aws.Config{
		Credentials: credentials.NewSharedCredentials(credentialPath, credentialAccountID),
		Region:      aws.String(region),
	})
}

// CreateWindowsVMWithPrivateSubnet creates a WindowsVM within a private subnet of the OpenShift cluster. A public
// IP will not be associated with the WindowsVM created by this method.
func (a *AwsProvider) CreateWindowsVMWithPrivateSubnet() (windowsVM types.WindowsVM, err error) {
	return a.createWindowsVM(true)
}

// CreateWindowsVM takes in imageId, instanceType, and sshKey name to create a Windows instance under the same VPC as
// the existing OpenShift cluster with the following:
// - attaches existing cloud-specific cluster worker security group and IAM to gain the same access as the linux
// workers,
// - uses public subnet,
// - attaches public ip to allow external access,
// - adds a security group that allows traffic from within the VPC range and RDP access from user's IP,
// - uses given image id, instance type, and sshKey name
// - creates a unique name tag for the instance using the same prefix as the OpenShift cluster name, and
// - logs id and security group information of the created instance in 'windows-node-installer.json' file at the
// resourceTrackerDir.
// On success, the function outputs RDP access information in the commandline interface. It also returns the
// the Windows VM Object to interact with using SSH, Winrm etc.
func (a *AwsProvider) CreateWindowsVM() (windowsVM types.WindowsVM, err error) {
	return a.createWindowsVM(false)
}

// createWindowsVM is a helper which creates the WindowsVM. It takes usePrivateSubnet as argument
// to decide if the the created VM has to be associated with public or private subnet
func (a *AwsProvider) createWindowsVM(usePrivateSubnet bool) (windowsVM types.WindowsVM, err error) {
	w := &types.Windows{}
	// If no AMI was provided, use the latest Windows AMI
	if a.imageID == "" {
		var err error
		a.imageID, err = a.getLatestWindowsAMI()
		if err != nil {
			return nil, fmt.Errorf("could not find latest Windows AMI: %s", err)
		}
	}
	// Obtains information from AWS and the existing OpenShift cluster for creating an instance.
	infraID, err := a.GetInfraID()
	if err != nil {
		return nil, err
	}
	networkInterface, err := a.getNetworkInterface(infraID, usePrivateSubnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get network interface, %v", err)
	}
	workerIAM, err := a.GetIAMWorkerRole(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster worker IAM, %v", err)
	}

	// PowerShell script to setup WinRM for Ansible, installing OpenSSH server and open firewall
	// port number 10250 on the Windows node created
	userDataWinrm := `<powershell>
        $url = "https://raw.githubusercontent.com/ansible/ansible/devel/examples/scripts/ConfigureRemotingForAnsible.ps1"
        $file = "$env:temp\ConfigureRemotingForAnsible.ps1"
        (New-Object -TypeName System.Net.WebClient).DownloadFile($url,  $file)
        & $file
        Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
        New-NetFirewallRule -DisplayName "` + types.FirewallRuleName + `"
        -Direction Inbound -Action Allow -Protocol TCP -LocalPort ` + types.ContainerLogsPort + ` -EdgeTraversalPolicy Allow
        </powershell>
        <persist>true</persist>`

	instance, err := a.createInstance(a.imageID, a.instanceType, a.sshKey, networkInterface, workerIAM, userDataWinrm)

	if err != nil {
		return nil, err
	}
	instanceID := *instance.InstanceId

	// Wait until instance is running and associate a unique name tag to the created instance.
	err = a.waitUntilInstanceRunning(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to wait till instance is running, %v", err)
	}
	_, err = a.createInstanceNameTag(instance, infraID)
	if err != nil {
		log.Printf("failed to assign name for instance: %s, %v", instanceID, err)
	}

	var publicIPAddress string
	if !usePrivateSubnet {
		// Get the public IP
		publicIPAddress, err = a.GetPublicIP(instanceID)
		if err != nil {
			return nil, err
		}
	}

	// get the private IP
	privateIPAddress, err := a.GetPrivateIP(instanceID)
	if err != nil {
		return nil, err
	}

	// Get the decrypted password
	decryptedPassword, err := a.GetPassword(instanceID)
	if err != nil {
		return nil, fmt.Errorf("error with instance creation %v", err)
	}

	// Build new credentials structure to be used by other actors. The actor is responsible for checking if
	// the credentials are being generated properly. This method won't guarantee the existence of credentials
	// if the VM is spun up.
	// Credentials can have both public or private IP Addresses.
	ipAddress := ""
	if usePrivateSubnet {
		ipAddress = privateIPAddress
	} else {
		ipAddress = publicIPAddress
	}
	credentials := types.NewCredentials(instanceID, ipAddress, decryptedPassword, winUser)
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
	if err := w.ConfigureOpenSSHServer(); err != nil {
		return w, fmt.Errorf("failed to configure OpenSSHServer on the Windows VM: %v", err)
	}
	if err := w.GetSSHClient(); err != nil {
		return w, fmt.Errorf("failed to get ssh client for the Windows VM created: %v", err)
	}

	err = resource.AppendInstallerInfo([]string{instanceID}, []string{}, a.resourceTrackerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to record instance ID to file at '%s',instance will not be able to be deleted, "+
			"%v", a.resourceTrackerDir, err)
	}
	log.Printf("External IP: %s", publicIPAddress)
	log.Printf("Internal IP: %s", privateIPAddress)
	return w, nil
}

// GetPublicIP returns the public IP address associated with the instance. Make to sure to call this function
// after the instance is in running state. Exposing this function to be used in testing later.
func (a *AwsProvider) GetPublicIP(instanceID string) (string, error) {
	// Till the instance comes to running state we cannot get the public ip address associated with it.
	// So, it's better to query the AWS api again to get the instance object and the ip address.
	instance, err := a.GetInstance(instanceID)
	if err != nil {
		return "", err
	}
	return *instance.PublicIpAddress, nil
}

// GetPrivateIP returns the private IP address associated with the instance. Make to sure to call this function
// after the instance is in running state. Exposing this function to be used in testing later.
func (a *AwsProvider) GetPrivateIP(instanceID string) (string, error) {
	// Till the instance comes to running state we cannot get the private ip address associated with it.
	// So, it's better to query the AWS api again to get the instance object and the ip address.
	instance, err := a.GetInstance(instanceID)
	if err != nil {
		return "", err
	}
	return *instance.PrivateIpAddress, nil
}

// GetPassword returns the password associated with the string. Exposing this to be used in tests later
func (a *AwsProvider) GetPassword(instanceID string) (string, error) {
	privateKey, err := ioutil.ReadFile(a.privateKeyPath)
	if err != nil {
		return "", err
	}
	privateKeyBytes := privateKey
	decodedEncryptedData, err := a.getDecodedPassword(instanceID)
	if err != nil {
		return "", err
	}
	decryptedPwd, err := rsaDecrypt(decodedEncryptedData, privateKeyBytes)
	if err != nil {
		return "", err
	}
	return string(decryptedPwd), nil
}

// getDecodedPassword gets the decoded password from the AWS cloud provider API
func (a *AwsProvider) getDecodedPassword(instanceID string) ([]byte, error) {
	// The docs within the aws-sdk says
	// `If you try to retrieve the password before it's available,
	// 	the output returns an empty string. We recommend that you wait up to 15 minutes
	//  after launching an instance before trying to retrieve the generated password.`
	// Ref: https://godoc.org/github.com/aws/aws-sdk-go/service/ec2#EC2.GetPasswordData
	pwdData, err := a.waitUntilPasswordDataIsAvailable(instanceID)
	if err != nil {
		return nil, err
	}
	// Decode password
	decodedPassword, err := base64.StdEncoding.DecodeString(strings.Trim(*pwdData.PasswordData, "\r\n"))
	if err != nil {
		return nil, err
	}
	return decodedPassword, nil

}

// DestroyWindowsVM destroys the given Windows VM
func (a *AwsProvider) DestroyWindowsVM(instanceID string) error {
	if err := a.TerminateInstance(instanceID); err != nil {
		return fmt.Errorf("failed deleting the instance %s: %v", instanceID, err)
	}
	if err := a.waitUntilInstanceTerminated(instanceID); err != nil {
		// As of now, I want the error thrown to be consistent across all cloud providers, so I am not specializing
		// the information in the error message string but the err will have more context. This is cause our operator
		// to block for a while
		return fmt.Errorf("failed deleting the instance %s: %v", instanceID, err)
	}
	return nil
}

// IsVMRunning checks if the VM is in running state currently and then returns failure
func (a *AwsProvider) IsVMRunning(instanceID string) error {
	// Check if the instance exists, if not no need to wait return immediately
	instance, err := a.GetInstance(instanceID)
	if err != nil {
		return fmt.Errorf("instance retrieval failed with: %v", err)
	}
	if *instance.State.Name != ec2.InstanceStateNameRunning {
		return fmt.Errorf("instance is in %s state", *instance.State.Name)
	}
	return nil
}

// waitUntilPasswordDataIsAvailable waits till the ec2 password data is available.
// AWS sdk's WaitUntilPasswordDataAvailable is returning inspite of password data being available.
// So, building this function as a wrapper around AWS sdk's GetPasswordData method with constant back-off
func (a *AwsProvider) waitUntilPasswordDataIsAvailable(instanceID string) (*ec2.GetPasswordDataOutput, error) {
	startTime := time.Now()
	for i := 0; ; i++ {
		currTime := time.Since(startTime)
		if currTime >= awsPasswordDataTimeOut {
			return nil, fmt.Errorf("timed out waiting for password to be available")
		}
		if err := a.IsVMRunning(instanceID); err != nil {
			return nil, err
		}
		// Get the ec2 passworddata output.
		pwdData, err := a.getPasswordDataOutput(instanceID)
		if err != nil {
			// Eventually we may get succeed, so let's continue till we hit 15 min limit
			log.Printf("error while getting password: %s", err)
			continue
		}
		if len(*pwdData.PasswordData) > 0 {
			return pwdData, nil
		}
		time.Sleep(time.Duration(i) * durationFactor)
	}
}

// getPasswordData returns the password passworddataoutput, if this returns nil, the password is not yet generated for the
// instance
func (a *AwsProvider) getPasswordDataOutput(instanceID string) (*ec2.GetPasswordDataOutput, error) {
	pwdData, err := a.EC2.GetPasswordData(&ec2.GetPasswordDataInput{
		InstanceId: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting encrypted password from aws cloud provider with %v", err)
	}
	return pwdData, nil
}

// rsaDecrypt decyptes the encrypted password and returns it.
// From AWS docs:
// The keys that Amazon EC2 uses are 2048-bit SSH-2 RSA keys. You can have up to five thousand key pairs per Region.
// Ref: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-key-pairs.html
func rsaDecrypt(decodedEncryptedData, privateKeyBytes []byte) ([]byte, error) {
	privateKeyBlock, _ := pem.Decode(privateKeyBytes)
	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key with %v", err)
	}
	decryptedPassword, err := rsa.DecryptPKCS1v15(crand.Reader, privateKey, decodedEncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password with %v", err)
	}
	return decryptedPassword, nil
}

// GetInfraID returns the infrastructure ID associated with the OpenShift cluster. This is public for
// testing purposes as of now.
func (a *AwsProvider) GetInfraID() (string, error) {
	infraID, err := a.openShiftClient.GetInfrastructureID()
	if err != nil {
		return "", fmt.Errorf("erroring getting OpenShift infrastructure ID associated with the cluster")
	}
	return infraID, nil
}

// DestroyWindowsVMs destroys the created instances and security groups on AWS specified in the
// 'windows-node-installer.json' file. The security groups still in use by other instances will not be deleted.
func (a *AwsProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file.
	log.Printf("processing file '%s'", a.resourceTrackerDir)
	destroyList, err := resource.ReadInstallerInfo(a.resourceTrackerDir)
	if err != nil {
		return err
	}

	var terminatedInstances, deletedSg []string

	// Delete all instances from the json file.
	for _, instanceID := range destroyList.InstanceIDs {
		err = a.TerminateInstance(instanceID)
		if err != nil {
			log.Printf("failed to terminate instance %s: %s", instanceID, err)
		}
	}
	// Wait for instances termination after they are initiated.
	for _, instanceID := range destroyList.InstanceIDs {
		err = a.waitUntilInstanceTerminated(instanceID)
		if err != nil {
			log.Printf("timeout waiting for instance %s to terminate: %s", instanceID, err)
		} else {
			terminatedInstances = append(terminatedInstances, instanceID)
		}
	}

	// Delete security groups after associated instances are terminated.
	for _, sgID := range destroyList.SecurityGroupIDs {
		err = a.DeleteSG(sgID)
		if err != nil {
			log.Printf("failed to delete security group %s: %s", sgID, err)
		} else {
			deletedSg = append(deletedSg, sgID)
		}
	}

	// Update 'windows-node-installer.json' file.
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, a.resourceTrackerDir)
	if err != nil {
		log.Printf("%s file was not updated: %s", a.resourceTrackerDir, err)
	}
	return nil
}

// getNetworkInterface is a wrapper function that includes all networking related work including getting OpenShift
// cluster's VPC and its worker security group, a public subnet within the VPC, and a Windows security group.
// It returns a valid ec2 network interface or an error.
func (a *AwsProvider) getNetworkInterface(infraID string, usePrivateSubnet bool) (*ec2.InstanceNetworkInterfaceSpecification, error) {
	vpc, err := a.getInfrastructureVPC(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get VPC, %v", err)
	}
	workerSG, err := a.GetClusterWorkerSGID(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster worker security group, %v", err)
	}
	subnetID, err := a.getSubnetID(infraID, vpc, usePrivateSubnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get a Public subnet, %v", err)
	}
	sgID, err := a.handleSg(infraID, vpc)
	if err != nil {
		return nil, fmt.Errorf("failed to create Windows worker security group, %v", err)
	}
	return &ec2.InstanceNetworkInterfaceSpecification{
		AssociatePublicIpAddress: aws.Bool(!usePrivateSubnet),
		DeleteOnTermination:      aws.Bool(true),
		DeviceIndex:              aws.Int64(0),
		Groups:                   aws.StringSlice([]string{workerSG, sgID}),
		SubnetId:                 aws.String(subnetID),
	}, nil
}

// createInstance creates one VM instance based on the given information and returns a instance struct with all its
// information or an error if no instance is created. userDataInput is a plaintext input, this will be passed
// and executed when launching the instance, it can be empty string if no data is given.
func (a *AwsProvider) createInstance(imageID, instanceType, sshKey string,
	networkInterface *ec2.InstanceNetworkInterfaceSpecification, iamProfile *ec2.IamInstanceProfileSpecification, userDataInput string) (
	*ec2.Instance, error) {
	tagSpec, err := a.createOpenShiftTagSpecification()
	if err != nil {
		return nil, fmt.Errorf("error creating tag specification %v", err)
	}
	runResult, err := a.EC2.RunInstances(&ec2.RunInstancesInput{
		ImageId:            aws.String(imageID),
		InstanceType:       aws.String(instanceType),
		KeyName:            aws.String(sshKey),
		MinCount:           aws.Int64(1),
		MaxCount:           aws.Int64(1),
		NetworkInterfaces:  []*ec2.InstanceNetworkInterfaceSpecification{networkInterface},
		IamInstanceProfile: iamProfile,
		UserData:           aws.String(base64.StdEncoding.EncodeToString([]byte(userDataInput))),
		TagSpecifications:  tagSpec,
	})
	if err != nil {
		return nil, err
	}
	// runResult should always contain the information of created instance if no error occurred.
	// The len(runResult.Instances) < 1 statement ensures instance[0] does not access invalid address,
	if len(runResult.Instances) < 1 || runResult.Instances[0] == nil {
		return nil, fmt.Errorf("failed to create an instance")
	}
	return runResult.Instances[0], nil
}

// createOpenShiftTagSpecification creates TagSpecification object to add OpenShift tag while
// launching an instance.
func (a *AwsProvider) createOpenShiftTagSpecification() ([]*ec2.TagSpecification, error) {
	infraID, err := a.GetInfraID()
	if err != nil {
		return nil, fmt.Errorf("error getting infrastructure ID for the OpenShift cluster, %v", err)
	}
	tags := map[string]string{
		// Add OpenShift tag, so that
		// - The kubelet can communicate with cloud provider
		// - TearDown & Reaper job in OpenShift CI can delete the virtual machine as part of cluster
		infraIDTagKeyPrefix + infraID: infraIDTagValue}

	ec2Tags, err := createTagList(tags)
	if err != nil {
		return nil, fmt.Errorf("error creating %v tags, %v", tags, err)
	}

	tagSpec := &ec2.TagSpecification{
		ResourceType: aws.String(ec2.ResourceTypeInstance),
		Tags:         ec2Tags,
	}

	return []*ec2.TagSpecification{tagSpec}, nil
}

// createTagList is a generic function to create ec2.Tag object list given
// a map of required tags
func createTagList(tags map[string]string) ([]*ec2.Tag, error) {
	var ec2Tags []*ec2.Tag
	if len(tags) == 0 {
		return nil, fmt.Errorf("no tags specified")
	}
	for key, value := range tags {
		ec2Tags = append(ec2Tags, &ec2.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return ec2Tags, nil

}

// getLatestWindowsAMI returns the imageid of the latest released "Windows Server with Containers" image
func (a *AwsProvider) getLatestWindowsAMI() (string, error) {
	// Have to create these variables, as the below functions require pointers to them
	windowsAMIOwner := "amazon"
	windowsAMIFilterName := "name"
	// This filter will grab all ami's that match the exact name. The '?' indicate any character will match.
	// The ami's will have the name format: Windows_Server-2019-English-Full-ContainersLatest-2020.01.15
	// so the question marks will match the date of creation
	windowsAMIFilterValue := "Windows_Server-2019-English-Full-ContainersLatest-????.??.??"
	searchFilter := ec2.Filter{Name: &windowsAMIFilterName, Values: []*string{&windowsAMIFilterValue}}

	describedImages, err := a.EC2.DescribeImages(&ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{&searchFilter},
		Owners:  []*string{&windowsAMIOwner},
	})
	if err != nil {
		return "", err
	}
	if len(describedImages.Images) < 1 {
		return "", fmt.Errorf("found zero images matching given filter: %v", searchFilter)
	}

	// Find the last created image
	latestImage := describedImages.Images[0]
	latestTime, err := time.Parse(time.RFC3339, *latestImage.CreationDate)
	if err != nil {
		return "", err
	}
	for _, image := range describedImages.Images[1:] {
		newTime, err := time.Parse(time.RFC3339, *image.CreationDate)
		if err != nil {
			return "", err
		}
		if newTime.After(latestTime) {
			latestImage = image
			latestTime = newTime
		}
	}
	return *latestImage.ImageId, nil
}

// getInfrastructureVPC gets the VPC of a given infrastructure or returns error.
func (a *AwsProvider) getInfrastructureVPC(infraID string) (*ec2.Vpc, error) {
	vpc, err := a.GetVPCByInfrastructure(infraID)
	if err != nil {
		return nil, err
	}

	return vpc, nil
}

// handleSg handles the Windows security group activities like finding, creating, verifying sg and updating them for
// WinRM, SSH and RDP access for Windows instance and allow all traffic within the VPC. If no such security group exists,
// it creates a security group under the VPC with a group name '<infraID>-windows-worker-sg'. It then verifies if the sg
// contains all the rules required for RDP and updates them.
// The function returns security group ID or error for both finding or creating a security group.
func (a *AwsProvider) handleSg(infraID string, vpc *ec2.Vpc) (string, error) {
	myIP, err := GetMyIp()
	if err != nil {
		return "", fmt.Errorf("error getting IP: %s", err)
	}
	sg, err := a.findWindowsWorkerSg(infraID)
	if err != nil {
		createdSG, err := a.createWindowsWorkerSg(infraID, vpc)
		if err != nil {
			return "", fmt.Errorf("error creating new security group: %s", err)
		}
		err = resource.AppendInstallerInfo([]string{}, []string{*createdSG.GroupId}, a.resourceTrackerDir)
		if err != nil {
			return "", fmt.Errorf("failed to record security group ID to file at '%s',"+
				"security group will not be deleted, %v", a.resourceTrackerDir, err)
		}
		// Get sg of type *ec2.securityGroup using the GroupId of newly created sg(type *ec2.CreateSecurityGroupOutput).
		// This newly created sg will have unpopulated fields.
		sg = &ec2.SecurityGroup{GroupId: createdSG.GroupId}
	}

	// Once we have found or created the security group, we can check if there are any rules to be
	// updated in the sg and update them.
	_, err = a.verifyAndUpdateSg(myIP, sg, vpc)

	log.Printf(fmt.Sprintf("Using existing Security Group: %s", *sg.GroupId))

	return *sg.GroupId, nil
}

// findWindowsWorkerSg finds the Windows worker security group based on security group name <infraID>-windows-worker-sg.
func (a *AwsProvider) findWindowsWorkerSg(infraID string) (*ec2.SecurityGroup, error) {
	sgName := strings.Join([]string{infraID, "windows", "worker", "sg"}, "-")
	sgs, err := a.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("group-name"),
				Values: aws.StringSlice([]string{sgName}),
			},
		},
	})
	if err != nil || len(sgs.SecurityGroups) == 0 {
		return nil, fmt.Errorf("worker security group not found, %v", err)
	}

	return sgs.SecurityGroups[0], nil
}

// createWindowsWorkerSg creates the Windows worker security group with name <infraID>-windows-worker-sg.
func (a *AwsProvider) createWindowsWorkerSg(infraID string, vpc *ec2.Vpc) (*ec2.CreateSecurityGroupOutput, error) {
	sgName := strings.Join([]string{infraID, "windows", "worker", "sg"}, "-")
	sg, err := a.EC2.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String("security group for RDP, winrm, ssh and all traffic within VPC"),
		VpcId:       vpc.VpcId,
	})
	if err != nil {
		return nil, err
	}

	return sg, nil
}

// verifyAndUpdateSg verifies if an update is required to the rules in existing sg, or adding all the rules if the sg
// is new. These rules are updated in the sg using addIngressRules. Returns error if rules
// to be updated cannot be returned or  cannot be updated.
func (a *AwsProvider) verifyAndUpdateSg(myIP string, sg *ec2.SecurityGroup, vpc *ec2.Vpc) ([]*ec2.IpPermission, error) {

	// Get the list of rules to be updated, returns empty list is there are no rules to be updated
	rules := a.getRulesForSgUpdate(myIP, sg.IpPermissions, *vpc.CidrBlock)

	// call addIngressRules() only if there are any rules returned from getRulesForSgUpdate()
	if len(rules) != 0 {
		err := a.addIngressRules(*sg.GroupId, rules)
		if err != nil {
			return nil, fmt.Errorf("could not set rules in sg %s: %s", *sg.GroupId, err)
		}
	}
	return rules, nil
}

// getRulesForSgUpdate returns a list of rules which are required to be updated in sg with local IP. If there are no
// rules to be updated it returns an empty slice. This serves as an input to addIngressRules function.
func (a *AwsProvider) getRulesForSgUpdate(myIP string, rules []*ec2.IpPermission, vpcCidr string) []*ec2.IpPermission {
	rulesForUpdate := make([]*ec2.IpPermission, 0)

	ports, hasClusterCidrRule := examineRulesInSg(myIP, rules, vpcCidr)

	// If ClusterCidrRule is missing, add default rule with IpProtocol value "-1"
	if !hasClusterCidrRule {
		rulesForUpdate = append(rulesForUpdate,
			(&ec2.IpPermission{}).
				// -1 is to allow all ports.
				SetIpProtocol("-1").
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(vpcCidr),
				}))
	}

	// create a list of rules to be updated in sg
	rulesForUpdate = append(rulesForUpdate, createRulesFromPorts(ports, myIP)...)
	return rulesForUpdate
}

// Create a set of rules to be added in sg given the input ports, helps in creating an appended list of all rules
func createRulesFromPorts(ports []int64, myIP string) []*ec2.IpPermission {
	populatedRules := make([]*ec2.IpPermission, 0)
	for _, port := range ports {
		populatedRules = append(populatedRules,
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				SetFromPort(port).
				SetToPort(port).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(myIP + "/32"),
				}))
		log.Printf(fmt.Sprintf("Added rule with port %d to the security groups of your local IP \n", port))
	}
	return populatedRules
}

// examineRulesInSg returns a list of ports which need to be updated in the rules for local IP and a flag indicating
// whether or not the rule with IP protocol "-1" which allows communication within the cluster is present in the rule
// set.
func examineRulesInSg(myIP string, rules []*ec2.IpPermission, vpcCidr string) ([]int64, bool) {
	var ports []int64
	var hasClusterCIDRRule bool
	portTracker := map[int64]bool{}
	// Loop through the rules, then loop through the ips to get a map of ports
	if rules != nil {
		for _, rule := range rules {
			for _, ips := range rule.IpRanges {
				if *ips.CidrIp == myIP+"/32" {
					portTracker[*rule.FromPort] = true
				}
				if *ips.CidrIp == vpcCidr && *rule.IpProtocol == "-1" {
					hasClusterCIDRRule = true
				}
			}
		}
	}
	// Add ports that need to be updated in a slice ports
	if !portTracker[WINRM_PORT] {
		ports = append(ports, WINRM_PORT)
	}
	if !portTracker[types.SshPort] {
		ports = append(ports, types.SshPort)
	}
	if !portTracker[rdpPort] {
		ports = append(ports, rdpPort)
	}
	return ports, hasClusterCIDRRule
}

// addIngressRules makes the call to the AWS AuthorizeSecurityGroupIngress to attach inbound rules for RDP from user's
// external IP address and all traffic within the VPC  on newly created security group for Windows instance.
// The function returns error if failed.
func (a *AwsProvider) addIngressRules(sgID string, rules []*ec2.IpPermission) error {
	_, err := a.EC2.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: rules,
	})
	if err != nil {
		return err
	}
	return nil
}

// GetMyIp get the external IP of user's machine from https://checkip.amazonaws.com and returns an address or an error.
// The 'checkip' service is maintained by Amazon.
// This function is exposed for testing purpose.
func GetMyIp() (string, error) {
	resp, err := http.Get("https://checkip.amazonaws.com")
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		return "", nil
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// createInstanceNameTag creates a name tag for a created instance with format: <infraID>-windows-worker-<zone>-
// <random 4 characters string>. The function returns a tagged instance Name or error if failed.
func (a *AwsProvider) createInstanceNameTag(instance *ec2.Instance, infraID string) (string, error) {
	zone, err := a.getInstanceZone(instance)
	if err != nil {
		return "", err
	}
	instanceName := strings.Join([]string{infraID, "windows", "worker", zone, rand.String(4)}, "-")
	tags, err := createTagList(map[string]string{"Name": instanceName})
	if err != nil {
		return "", fmt.Errorf("error creating %v tags, %v", tags, err)
	}
	_, err = a.EC2.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{instance.InstanceId},
		Tags:      tags,
	})
	if err != nil {
		return "", err
	}
	return instanceName, nil
}

// getInstanceZone gets the instance zone name (ie: us-east-1a) from the input instance struct or returns an error.
func (a *AwsProvider) getInstanceZone(instance *ec2.Instance) (string, error) {
	if instance == nil || instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
		return "", fmt.Errorf("failed to get intance availbility zone")
	}
	return *instance.Placement.AvailabilityZone, nil
}

// GetVPCByInfrastructure finds the VPC of an infrastructure and returns the VPC struct or an error.
// This function is exposed for testing purpose.
func (a *AwsProvider) GetVPCByInfrastructure(infraID string) (*ec2.Vpc, error) {
	res, err := a.EC2.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:" + infraIDTagKeyPrefix + infraID),
				Values: aws.StringSlice([]string{infraIDTagValue}),
			},
			{
				Name:   aws.String("state"),
				Values: aws.StringSlice([]string{"available"}),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(res.Vpcs) < 1 {
		return nil, fmt.Errorf("failed to find the VPC of the infrastructure")
	} else if len(res.Vpcs) > 1 {
		log.Printf("more than one VPC is found, using %s", *res.Vpcs[0].VpcId)
	}
	return res.Vpcs[0], nil
}

// getSubnetID tries to find a subnet under the VPC and returns subnet ID or an error.
// These subnets belongs to the OpenShift cluster. It can be either public or private
func (a *AwsProvider) getSubnetID(infraID string, vpc *ec2.Vpc, usePrivateSubnet bool) (string, error) {
	// search subnet by the vpcid owned by the vpcID
	subnets, err := a.EC2.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{vpc.VpcId},
			},
		},
	})
	if err != nil {
		return "", err
	}

	// Get the instance offerings that support Windows instances
	scope := "Availability Zone"
	productDescription := "Windows"
	f := false
	offerings, err := a.EC2.DescribeReservedInstancesOfferings(&ec2.DescribeReservedInstancesOfferingsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("scope"),
				Values: []*string{&scope},
			},
		},
		IncludeMarketplace: &f,
		InstanceType:       &a.instanceType,
		ProductDescription: &productDescription,
	})
	if err != nil {
		return "", fmt.Errorf("error checking instance offerings of %s: %v", a.instanceType, err)
	}
	if offerings.ReservedInstancesOfferings == nil {
		return "", fmt.Errorf("no instance offerings returned for %s", a.instanceType)
	}

	// Finding required subnet within the vpc.
	foundSubnet := false
	requiredSubnet := ""
	if usePrivateSubnet {
		requiredSubnet = "-private-"
	} else {
		requiredSubnet = "-public-"
	}
	for _, subnet := range subnets.Subnets {
		for _, tag := range subnet.Tags {
			// TODO: find public subnet by checking igw gateway in routing.
			if *tag.Key == "Name" && strings.Contains(*tag.Value, infraID+requiredSubnet) {
				foundSubnet = true
				// Ensure that the instance type we want is supported in the zone that the subnet is in
				for _, instanceOffering := range offerings.ReservedInstancesOfferings {
					if instanceOffering.AvailabilityZone == nil {
						continue
					}
					if *instanceOffering.AvailabilityZone == *subnet.AvailabilityZone {
						return *subnet.SubnetId, nil
					}
				}
			}
		}
	}

	err = fmt.Errorf("could not find the required subnet in VPC: %v", *vpc.VpcId)
	if !foundSubnet {
		err = fmt.Errorf("could not find the required subnet in a zone that supports %s instance type",
			a.instanceType)
	}
	return "", err
}

// GetInstance gets instance ec2 instance object from the given instanceID. We're making this method public
// to use it in tests as of now.
func (a *AwsProvider) GetInstance(instanceID string) (*ec2.Instance, error) {
	instances, err := a.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
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

// GetClusterWorkerSGID gets worker security group id from the existing cluster or returns an error.
// This function is exposed for testing purpose.
func (a *AwsProvider) GetClusterWorkerSGID(infraID string) (string, error) {
	sg, err := a.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{fmt.Sprintf("%s-worker-sg", infraID)}),
			},
			{
				Name:   aws.String("tag:" + infraIDTagKeyPrefix + infraID),
				Values: aws.StringSlice([]string{infraIDTagValue}),
			},
		},
	})
	if err != nil {
		return "", err
	}
	if sg == nil || len(sg.SecurityGroups) < 1 {
		return "", fmt.Errorf("no security group is found for the cluster worker nodes")
	}
	return *sg.SecurityGroups[0].GroupId, nil
}

// GetIAMWorkerRole gets worker IAM information from the existing cluster including IAM arn or an error.
// This function is exposed for testing purpose.
func (a *AwsProvider) GetIAMWorkerRole(infraID string) (*ec2.IamInstanceProfileSpecification, error) {
	iamspc, err := a.IAM.GetInstanceProfile(&iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(fmt.Sprintf("%s-worker-profile", infraID)),
	})
	if err != nil {
		return nil, err
	}
	return &ec2.IamInstanceProfileSpecification{
		Arn: iamspc.InstanceProfile.Arn,
	}, nil
}

// isSGInUse checks if the security group is used by any existing instances and returns true only if it is certain
// that the security group is in use and logs the list of instances using the group.
func (a *AwsProvider) isSGInUse(sgID string) (bool, error) {
	instances, err := a.EC2.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance.group-id"),
				Values: aws.StringSlice([]string{sgID}),
			},
		},
	})
	if err != nil {
		return false, err
	}

	// Collect all instances that are using this security group and show them as message for user to understand what
	// specific instances are preventing the security group from being deleted.
	var reservingInstances []string
	for _, reserve := range instances.Reservations {
		for _, instance := range reserve.Instances {
			reservingInstances = append(reservingInstances, *instance.InstanceId)
		}
	}

	if len(reservingInstances) > 0 {
		log.Printf("Security Group %s is in use by: %s", sgID, strings.Join(reservingInstances, ", "))
		return true, nil
	}
	return false, nil
}

// DeleteSG checks if security group is in use, deletes it if not in use based on sgID, and returns error if fails.
// This function is exposed for testing purpose.
func (a *AwsProvider) DeleteSG(sgID string) error {
	sgInUse, err := a.isSGInUse(sgID)
	if err != nil {
		return err
	}
	if sgInUse {
		return fmt.Errorf("security group is in use")
	}

	_, err = a.EC2.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(sgID),
	})
	return err
}

// TerminateInstance will delete an AWS instance based on instance id and returns error if deletion fails.
// This function is exposed for testing purpose.
func (a *AwsProvider) TerminateInstance(instanceID string) error {
	_, err := a.EC2.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	return err
}

// waitUntilInstanceRunning waits until the instance is running and returns error if timeout or instance goes
// to other states.The wait function tries for 40 times to see the instance in running state with 15 seconds in
// between or returns error.
func (a *AwsProvider) waitUntilInstanceRunning(instanceID string) error {
	return a.EC2.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
}

// waitUntilInstanceTerminated waits until the instance is terminated and returns error if timeout or instance goes
// to other states. The wait function tries for 40 times to see the instance terminated with 15 seconds in between or
// returns error.
func (a *AwsProvider) waitUntilInstanceTerminated(instanceID string) error {
	return a.EC2.WaitUntilInstanceTerminated(&ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
}
