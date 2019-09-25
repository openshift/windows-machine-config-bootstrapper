package aws

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/client"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/resource"
	"k8s.io/apimachinery/pkg/util/rand"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// log is the global logger for the aws package. Each log record produced
// by this logger will have an identifier containing `aws-ec2` tag.
var log = logger.Log.WithName("aws-ec2")

// Constant value
const (
	// Winrm port for https request
	WINRM_PORT = 5986
)

// awsProvider is a provider specific struct which contains clients for EC2, IAM, and the existing OpenShift
// cluster that is running on EC2.
// This is an implementation of the Cloud interface.
type awsProvider struct {
	// imageID is the AMI image-id to be used for creating Virtual Machine
	imageID string
	// instanceType is the flavor of VM to be used
	instanceType string
	// sshKey is the ssh key to access the VM created. Please note that key should be uploaded to AWS before
	// using this flag
	sshKey string
	// A client for EC2.
	EC2 *ec2.EC2
	// A client for IAM.
	IAM *iam.IAM
	// openShiftClient is the client of the existing OpenShift cluster.
	openShiftClient *client.OpenShift
	// resourceTrackerDir is where `windows-node-installer.json` file is stored.
	resourceTrackerDir string
}

// New returns the AWS implementations of the Cloud interface with AWS session in the same region as OpenShift Cluster.
// credentialPath is the file path the AWS credentials file.
// credentialAccountID is the account name the user uses to create VM instance.
// The credentialAccountID should exist in the AWS credentials file pointing at one specific credential.
// resourceTrackerDir is where created instance and security group information is stored.
func New(openShiftClient *client.OpenShift, imageID, instanceType, sshKey, credentialPath, credentialAccountID,
	resourceTrackerDir string) (*awsProvider, error) {
	provider, err := openShiftClient.GetCloudProvider()
	if err != nil {
		return nil, err
	}
	session, err := newSession(credentialPath, credentialAccountID, provider.AWS.Region)
	if err != nil {
		return nil, err
	}
	return &awsProvider{imageID, instanceType, sshKey,
		ec2.New(session, aws.NewConfig()),
		iam.New(session, aws.NewConfig()),
		openShiftClient,
		resourceTrackerDir,
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
// On success, the function outputs RDP access information in the commandline interface.
func (a *awsProvider) CreateWindowsVM() (rerr error) {
	// Obtains information from AWS and the existing OpenShift cluster for creating an instance.
	infraID, err := a.openShiftClient.GetInfrastructureID()
	if err != nil {
		return err
	}
	networkInterface, err := a.getNetworkInterface(infraID)
	if err != nil {
		return fmt.Errorf("failed to get network interface, %v", err)
	}
	workerIAM, err := a.getIAMWorkerRole(infraID)
	if err != nil {
		return fmt.Errorf("failed to get cluster worker IAM, %v", err)
	}

	// PowerShell script to setup WinRM for Ansible
	userDataWinrm := `<powershell>
        $url = "https://raw.githubusercontent.com/ansible/ansible/devel/examples/scripts/ConfigureRemotingForAnsible.ps1"
        $file = "$env:temp\ConfigureRemotingForAnsible.ps1"
        (New-Object -TypeName System.Net.WebClient).DownloadFile($url,  $file)
        powershell.exe -ExecutionPolicy ByPass -File $file
        </powershell>
        <persist>true</persist>`

	instance, err := a.createInstance(a.imageID, a.instanceType, a.sshKey, networkInterface, workerIAM, userDataWinrm)

	if err != nil {
		return err
	}
	instanceID := *instance.InstanceId

	// Wait until instance is running and associate a unique name tag to the created instance.
	err = a.waitUntilInstanceRunning(instanceID)
	if err != nil {
		return fmt.Errorf("failed to wait till instance is running, %v", err)
	}
	_, err = a.createInstanceNameTag(instance, infraID)
	if err != nil {
		log.V(0).Info(fmt.Sprintf("failed to assign name for instance: %s, %v", instanceID, err))
	}

	err = resource.AppendInstallerInfo([]string{instanceID}, []string{}, a.resourceTrackerDir)
	if err != nil {
		return fmt.Errorf("failed to record instance ID to file at '%s',instance will not be able to be deleted, "+
			"%v", a.resourceTrackerDir, err)
	}

	// Output commandline message to help RDP into the created instance.
	log.V(0).Info(fmt.Sprintf("Successfully created windows instance: %s, please RDP into the Windows instance.",
		instanceID))

	return nil
}

// DestroyWindowsVMs destroys the created instances and security groups on AWS specified in the
// 'windows-node-installer.json' file. The security groups still in use by other instances will not be deleted.
func (a *awsProvider) DestroyWindowsVMs() error {
	// Read from `windows-node-installer.json` file.
	log.V(0).Info(fmt.Sprintf("processing file '%s'", a.resourceTrackerDir))
	destroyList, err := resource.ReadInstallerInfo(a.resourceTrackerDir)
	if err != nil {
		return err
	}

	var terminatedInstances, deletedSg []string

	// Delete all instances from the json file.
	for _, instanceID := range destroyList.InstanceIDs {
		err = a.terminateInstance(instanceID)
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
		err = a.deleteSG(sgID)
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
func (a *awsProvider) getNetworkInterface(infraID string) (*ec2.InstanceNetworkInterfaceSpecification, error) {
	vpc, err := a.getInfrastructureVPC(infraID)
	if err != nil {
		return nil, fmt.Errorf("failed to get VPC, %v", err)
	}
	workerSG, err := a.getClusterWorkerSGID(infraID)
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
func (a *awsProvider) createInstance(imageID, instanceType, sshKey string,
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
func (a *awsProvider) getInfrastructureVPC(infraID string) (*ec2.Vpc, error) {
	vpc, err := a.getVPCByInfrastructure(infraID)
	if err != nil {
		return nil, err
	}

	return vpc, nil
}

// findOrCreateSg tries to find Windows security group for RDP into windows instance and allow all traffic within the
// VPC. If no such security group exists, it creates a security group under the VPC with a group name
// '<infraID>-windows-worker-sg'.
// The function returns security group ID or error for both finding or creating a security group.
func (a *awsProvider) findOrCreateSg(infraID string, vpc *ec2.Vpc) (string, error) {
	sgID, err := a.findWindowsWorkerSg(infraID)
	if err != nil {
		return a.createWindowsWorkerSg(infraID, vpc)
	}
	return sgID, nil
}

// findWindowsWorkerSg creates the Windows worker security group with name <infraID>-windows-worker-sg.
func (a *awsProvider) createWindowsWorkerSg(infraID string, vpc *ec2.Vpc) (string, error) {
	sgName := strings.Join([]string{infraID, "windows", "worker", "sg"}, "-")
	sg, err := a.EC2.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String("security group for RDP and all traffic within VPC"),
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
func (a *awsProvider) authorizeSgIngress(sgID string, vpc *ec2.Vpc) error {
	myIP, err := getMyIp()
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
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// findWindowsWorkerSg finds the Windows worker security group based on security group name <infraID>-windows-worker-sg.
func (a *awsProvider) findWindowsWorkerSg(infraID string) (string, error) {
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

// getMyIp get the external IP of user's machine from https://checkip.amazonaws.com and returns an address or an error.
// The 'checkip' service is maintained by Amazon.
func getMyIp() (string, error) {
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
func (a *awsProvider) createInstanceNameTag(instance *ec2.Instance, infraID string) (string, error) {
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
		},
	})
	if err != nil {
		return "", err
	}
	return instanceName, nil
}

// getInstanceZone gets the instance zone name (ie: us-east-1a) from the input instance struct or returns an error.
func (a *awsProvider) getInstanceZone(instance *ec2.Instance) (string, error) {
	if instance == nil || instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
		return "", fmt.Errorf("failed to get intance availbility zone")
	}
	return *instance.Placement.AvailabilityZone, nil
}

// getVPCByInfrastructure finds the VPC of an infrastructure and returns the VPC struct or an error.
func (a *awsProvider) getVPCByInfrastructure(infraID string) (*ec2.Vpc, error) {
	res, err := a.EC2.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + infraID),
				Values: aws.StringSlice([]string{"owned"}),
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
func (a *awsProvider) getPublicSubnetId(infraID string, vpc *ec2.Vpc) (string, error) {
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

// getClusterWorkerSGID gets worker security group id from the existing cluster or returns an error.
func (a *awsProvider) getClusterWorkerSGID(infraID string) (string, error) {
	sg, err := a.EC2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{fmt.Sprintf("%s-worker-sg", infraID)}),
			},
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + infraID),
				Values: aws.StringSlice([]string{"owned"}),
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

// getIAMWorkerRole gets worker IAM information from the existing cluster including IAM arn or an error.
func (a *awsProvider) getIAMWorkerRole(infraID string) (*ec2.IamInstanceProfileSpecification, error) {
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
func (a *awsProvider) isSGInUse(sgID string) (bool, error) {
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

// deleteSG checks if security group is in use, deletes it if not in use based on sgID, and returns error if fails.
func (a *awsProvider) deleteSG(sgID string) error {
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

// terminateInstance will delete an AWS instance based on instance id and returns error if deletion fails.
func (a *awsProvider) terminateInstance(instanceID string) error {
	_, err := a.EC2.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	return err
}

// waitUntilInstanceRunning waits until the instance is running and returns error if timeout or instance goes
// to other states.The wait function tries for 40 times to see the instance in running state with 15 seconds in
// between or returns error.
func (a *awsProvider) waitUntilInstanceRunning(instanceID string) error {
	return a.EC2.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
}

// waitUntilInstanceTerminated waits until the instance is terminated and returns error if timeout or instance goes
// to other states. The wait function tries for 40 times to see the instance terminated with 15 seconds in between or
// returns error.
func (a *awsProvider) waitUntilInstanceTerminated(instanceID string) error {
	return a.EC2.WaitUntilInstanceTerminated(&ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
}
