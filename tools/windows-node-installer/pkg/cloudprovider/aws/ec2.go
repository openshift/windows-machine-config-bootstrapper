package aws

import (
	"bytes"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/util/rand"
	"net/http"
	"os"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"strings"
	"time"
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
	// sshPort to access the OpenSSH server installed on the windows node. This is needed
	// for our CI testing.
	sshPort = 22
)

// log is the global logger for the aws package. Each log record produced
// by this logger will have an identifier containing `aws-ec2` tag.
var log = logger.Log.WithName("aws-ec2")

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
	EC2 *ec2.EC2
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
		return nil, err
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
	session := awssession.Must(awssession.NewSession(&aws.Config{
		Credentials: credentials.NewSharedCredentials(credentialPath, credentialAccountID),
		Region:      aws.String(region),
	}))
	return session, nil
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
// the credentials to access the Windows VM created,
func (a *AwsProvider) CreateWindowsVM() (credentials *types.Credentials, rerr error) {
	// Obtains information from AWS and the existing OpenShift cluster for creating an instance.
	infraID, err := a.GetInfraID()
	if err != nil {
		return nil, err
	}
	networkInterface, err := a.getNetworkInterface(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network interface, %v", err)
	}
	workerIAM, err := a.GetIAMWorkerRole(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster worker IAM, %v", err)
	}

	// PowerShell script to setup WinRM for Ansible and installing OpenSSH server on the Windows node created
	userDataWinrm := `<powershell>
        $url = "https://raw.githubusercontent.com/ansible/ansible/devel/examples/scripts/ConfigureRemotingForAnsible.ps1"
        $file = "$env:temp\ConfigureRemotingForAnsible.ps1"
        (New-Object -TypeName System.Net.WebClient).DownloadFile($url,  $file)
        powershell.exe -ExecutionPolicy ByPass -File $file
        powershell -NonInteractive -ExecutionPolicy Bypass Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
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
		log.V(0).Info(fmt.Sprintf("failed to assign name for instance: %s, %v", instanceID, err))
	}

	// Get the public IP
	publicIPAddress, err := a.GetPublicIP(instanceID)
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
	// if the VM is spun up
	credentials = types.NewCredentials(instanceID, publicIPAddress, decryptedPassword, winUser)
	err = resource.AppendInstallerInfo([]string{instanceID}, []string{}, a.resourceTrackerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to record instance ID to file at '%s',instance will not be able to be deleted, "+
			"%v", a.resourceTrackerDir, err)
	}

	// Output commandline message to help RDP into the created instance.
	log.Info(fmt.Sprintf("Successfully created windows instance: %s, "+
		"please RDP into the Windows instance created at %s using Admininstrator as user and %s password",
		instanceID, publicIPAddress, decryptedPassword))
	// TODO: Output the information to a file
	return credentials, nil
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
		// Get the ec2 passworddata output.
		pwdData, err := a.getPasswordDataOutput(instanceID)
		if err != nil {
			// Eventually we may get succeed, so let's continue till we hit 15 min limit
			log.Info("error while getting password", err)
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
	log.V(0).Info(fmt.Sprintf("processing file '%s'", a.resourceTrackerDir))
	destroyList, err := resource.ReadInstallerInfo(a.resourceTrackerDir)
	if err != nil {
		return err
	}

	var terminatedInstances, deletedSg []string

	// Delete all instances from the json file.
	for _, instanceID := range destroyList.InstanceIDs {
		err = a.TerminateInstance(instanceID)
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to terminate instance: %s", instanceID))
		}
	}
	// Wait for instances termination after they are initiated.
	for _, instanceID := range destroyList.InstanceIDs {
		err = a.waitUntilInstanceTerminated(instanceID)
		if err != nil {
			log.Error(err, fmt.Sprintf("timeout waiting for instance: %s to terminate", instanceID))
		} else {
			terminatedInstances = append(terminatedInstances, instanceID)
		}
	}

	// Delete security groups after associated instances are terminated.
	for _, sgID := range destroyList.SecurityGroupIDs {
		err = a.DeleteSG(sgID)
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to delete security group: %s", sgID))
		} else {
			deletedSg = append(deletedSg, sgID)
		}
	}

	// Update 'windows-node-installer.json' file.
	err = resource.RemoveInstallerInfo(terminatedInstances, deletedSg, a.resourceTrackerDir)
	if err != nil {
		log.V(0).Info(fmt.Sprintf("%s file was not updated, %v", a.resourceTrackerDir, err))
	}
	return nil
}

// getNetworkInterface is a wrapper function that includes all networking related work including getting OpenShift
// cluster's VPC and its worker security group, a public subnet within the VPC, and a Windows security group.
// It returns a valid ec2 network interface or an error.
func (a *AwsProvider) getNetworkInterface(infraID string) (*ec2.InstanceNetworkInterfaceSpecification, error) {
	vpc, err := a.getInfrastructureVPC(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get VPC, %v", err)
	}
	workerSG, err := a.GetClusterWorkerSGID(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster worker security group, %v", err)
	}
	publicSubnetID, err := a.getPublicSubnetId(infraID, vpc)
	if err != nil {
		return nil, fmt.Errorf("failed to get a Public subnet, %v", err)
	}
	sgID, err := a.findOrCreateSg(infraID, vpc)
	if err != nil {
		return nil, fmt.Errorf("failed to create Windows worker security group, %v", err)
	}
	return &ec2.InstanceNetworkInterfaceSpecification{
		AssociatePublicIpAddress: aws.Bool(true),
		DeleteOnTermination:      aws.Bool(true),
		DeviceIndex:              aws.Int64(0),
		Groups:                   aws.StringSlice([]string{workerSG, sgID}),
		SubnetId:                 aws.String(publicSubnetID),
	}, nil
}

// createInstance creates one VM instance based on the given information and returns a instance struct with all its
// information or an error if no instance is created. userDataInput is a plaintext input, this will be passed
// and executed when launching the instance, it can be empty string if no data is given.
func (a *AwsProvider) createInstance(imageID, instanceType, sshKey string,
	networkInterface *ec2.InstanceNetworkInterfaceSpecification, iamProfile *ec2.IamInstanceProfileSpecification, userDataInput string) (
	*ec2.Instance, error) {
	runResult, err := a.EC2.RunInstances(&ec2.RunInstancesInput{
		ImageId:            aws.String(imageID),
		InstanceType:       aws.String(instanceType),
		KeyName:            aws.String(sshKey),
		MinCount:           aws.Int64(1),
		MaxCount:           aws.Int64(1),
		NetworkInterfaces:  []*ec2.InstanceNetworkInterfaceSpecification{networkInterface},
		IamInstanceProfile: iamProfile,
		UserData:           aws.String(base64.StdEncoding.EncodeToString([]byte(userDataInput))),
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

// getInfrastructureVPC gets the VPC of a given infrastructure or returns error.
func (a *AwsProvider) getInfrastructureVPC(infraID string) (*ec2.Vpc, error) {
	vpc, err := a.GetVPCByInfrastructure(infraID)
	if err != nil {
		return nil, err
	}

	return vpc, nil
}

// findOrCreateSg tries to find Windows security group for RDP into windows instance and allow all traffic within the
// VPC. If no such security group exists, it creates a security group under the VPC with a group name
// '<infraID>-windows-worker-sg'.
// The function returns security group ID or error for both finding or creating a security group.
func (a *AwsProvider) findOrCreateSg(infraID string, vpc *ec2.Vpc) (string, error) {
	sgID, err := a.findWindowsWorkerSg(infraID)
	if err != nil {
		return a.createWindowsWorkerSg(infraID, vpc)
	}

	log.Info(fmt.Sprintf("Using existing Security Group: %s.", sgID))

	// Check winrm port open status for the existing security group
	iswinrmPortOpen, err := a.IsPortOpen(sgID, WINRM_PORT)
	if err != nil {
		return "", err
	}
	// TODO: Add a map so that we can have a specific protocol to port mapping.
	// Add winrm port to security group if it doesn't exist
	if !iswinrmPortOpen {
		err := a.addPortToSg(sgID, WINRM_PORT)
		if err != nil {
			return "", err
		}
		log.Info(fmt.Sprintf("Winrm port is now added to Security Group %s", sgID))
	} else {
		log.Info(fmt.Sprintf("Winrm port already opened for Security Group: %s.", sgID))
	}

	// Check if ssh port is open
	isSSHPortOpen, err := a.IsPortOpen(sgID, sshPort)
	if err != nil {
		return "", err
	}

	// Add ssh port to security group if it doesn't exist
	if !isSSHPortOpen {
		err := a.addPortToSg(sgID, sshPort)
		if err != nil {
			return "", err
		}
		log.Info(fmt.Sprintf("ssh port is now added to Security Group %s", sgID))
	} else {
		log.Info(fmt.Sprintf("ssh port already opened for Security Group: %s.", sgID))
	}

	return sgID, nil
}

// IsPort checks whether the given port is open in the given security group.
// Return boolean for the checking result.
func (a *AwsProvider) IsPortOpen(sgId string, port int64) (bool, error) {
	// Get security group information
	SgResult, err := a.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{
			aws.String(sgId),
		},
	})
	if err != nil {
		return false, err
	}

	// Search winrm port in the inbound rule of the security group
	for _, rule := range SgResult.SecurityGroups[0].IpPermissions {
		if (rule.FromPort != nil && *rule.FromPort == port) && (rule.ToPort != nil && *rule.ToPort == port) {
			return true, nil
		}
	}
	log.Info(fmt.Sprintf("Given port %v is not open for Security Group: %s.", port, sgId))
	return false, nil
}

// addPortToSg adds the given port to the given security group.
func (a *AwsProvider) addPortToSg(sgId string, port int64) error {
	myIP, err := GetMyIp()
	if err != nil {
		return err
	}

	// Add winrm https port to security group
	log.Info(fmt.Sprintf("Adding winrm https port to Security Group %s", sgId))
	_, err = a.EC2.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: aws.String(sgId),
		IpPermissions: []*ec2.IpPermission{
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				SetFromPort(port).
				SetToPort(port).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(myIP + "/32"),
				}),
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// findWindowsWorkerSg creates the Windows worker security group with name <infraID>-windows-worker-sg.
func (a *AwsProvider) createWindowsWorkerSg(infraID string, vpc *ec2.Vpc) (string, error) {
	sgName := strings.Join([]string{infraID, "windows", "worker", "sg"}, "-")
	sg, err := a.EC2.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String("security group for RDP, winrm, ssh and all traffic within VPC"),
		VpcId:       vpc.VpcId,
	})
	if err != nil {
		return "", err
	}

	err = a.authorizeSgIngress(*sg.GroupId, vpc)
	if err != nil {
		return "", err
	}

	err = resource.AppendInstallerInfo([]string{}, []string{*sg.GroupId}, a.resourceTrackerDir)
	if err != nil {
		return "", fmt.Errorf("failed to record security group ID to file at '%s',"+
			"security group will not be deleted, %v", a.resourceTrackerDir, err)
	}

	return *sg.GroupId, nil
}

// authorizeSgIngress attaches inbound rules for RDP from user's external IP address and all traffic within the VPC
// on newly created security group for Windows instance. The function returns error if failed.
func (a *AwsProvider) authorizeSgIngress(sgID string, vpc *ec2.Vpc) error {
	myIP, err := GetMyIp()
	if err != nil {
		return err
	}

	_, err = a.EC2.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: aws.String(sgID),
		IpPermissions: []*ec2.IpPermission{
			(&ec2.IpPermission{}).
				// -1 is to allow all ports.
				SetIpProtocol("-1").
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(*vpc.CidrBlock),
				}),
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				SetFromPort(3389).
				SetToPort(3389).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(myIP + "/32"),
				}),
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				// winrm ansible https port
				SetFromPort(WINRM_PORT).
				SetToPort(WINRM_PORT).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(myIP + "/32"),
				}),
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				// SSH port to be opened for
				// TODO: add validation to check if the port is already opened, inform the user
				// of the same and this should be a no-op
				SetFromPort(sshPort).
				SetToPort(sshPort).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(myIP + "/32"),
				}),
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// findWindowsWorkerSg finds the Windows worker security group based on security group name <infraID>-windows-worker-sg.
func (a *AwsProvider) findWindowsWorkerSg(infraID string) (string, error) {
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
		return "", fmt.Errorf("worker security group not found, %v", err)
	}
	return *sgs.SecurityGroups[0].GroupId, nil
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
	_, err = a.EC2.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{instance.InstanceId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(instanceName),
			},
			// Add OpenShift tag, so that
			// - The kubelet can communicate with cloud provider
			// - TearDown & Reaper job in OpenShift CI can delete the virtual machine as part of cluster
			{
				Key:   aws.String(infraIDTagKeyPrefix + infraID),
				Value: aws.String(infraIDTagValue),
			},
		},
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
		log.V(0).Info(fmt.Sprintf("more than one VPC is found, using %s", *res.Vpcs[0].VpcId))
	}
	return res.Vpcs[0], nil
}

// getPublicSubnetId tries to find a public subnet under the VPC and returns subnet id or an error.
// These subnets belongs to the OpenShift cluster.
func (a *AwsProvider) getPublicSubnetId(infraID string, vpc *ec2.Vpc) (string, error) {
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

	// Finding public subnet within the vpc.
	for _, subnet := range subnets.Subnets {
		for _, tag := range subnet.Tags {
			// TODO: find public subnet by checking igw gateway in routing.
			if *tag.Key == "Name" && strings.Contains(*tag.Value, infraID+"-public-") {
				return *subnet.SubnetId, nil
			}
		}
	}
	return "", fmt.Errorf("could not find a public subnet in VPC: %v", *vpc.VpcId)
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
		log.V(0).Info(fmt.Sprintf("Security Group %s is in use by: %s", sgID, strings.Join(reservingInstances, ", ")))
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
