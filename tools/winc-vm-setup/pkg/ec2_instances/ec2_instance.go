package ec2_instances

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	v1 "github.com/openshift/api/config/v1"
	client "github.com/openshift/client-go/config/clientset/versioned"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net/http"
	"os"
	"strings"
)

type Instances []InstanceInfo

type InstanceInfo struct {
	Instanceid string   `json:"Instanceid"`
	SG         []SGInfo `json:"SG"`
}

type SGInfo struct {
	Groupid   string `json:"Groupid"`
	Groupname string `json:"Groupname"`
}

// CreateEC2WinC creates an Windows node instance based on a given AWS session and kubeconfig of an existing openshift cluster under the same VPC
// attaches existing openshift cluster worker security group and IAM
// uses public subnet, attaches public ip, and creates or attaches security group that allows all traffic 10.0.0.0/16 and RDP from my IP
// uses given image id, instance type, and keyname
// creates Name tag for the instance using the same prefix as the openshift cluster name
// writes id and security group information of an created instance
// provides RDP information in commandline
func CreateEC2WinC(sess *session.Session, clientset *client.Clientset, imageId, instanceType, keyName, path *string) {
	svc := ec2.New(sess, aws.NewConfig())
	svcIAM := iam.New(sess, aws.NewConfig())
	var sgID, instanceID *string
	var createdInst InstanceInfo
	var createdSG SGInfo
	// get infrastructure from OC using kubeconfig info
	infra := getInfrastrcture(clientset)
	// get infraID an unique readable id for the infrastructure
	infraID := infra.Status.InfrastructureName
	// get vpc id of the openshift cluster
	vpcID, err := getVPCByInfrastructure(svc, infra)
	if err != nil {
		log.Fatalf("We failed to find our vpc, %v", err)
	}
	// get openshift cluster worker security groupID
	workerSG := getClusterSGID(svc, infraID, "worker")
	// get openshift cluster worker iam profile
	iamProfile := getIAMrole(svcIAM, infraID, "worker") // unnecessary, could just rely on naming convention to set the iam specifics
	// get or create a public subnet under the vpcID
	subnetID, err := getPubSubnetOrCreate(svc, vpcID, infraID)
	sg, err := svc.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(infraID + "-winc-sg"),
		Description: aws.String("security group for rdp and all traffic"),
		VpcId:       aws.String(vpcID),
	})
	if err != nil {
		log.Printf("could not create Security Group, attaching existing instead: %v", err)
		sgs, err := svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: aws.StringSlice([]string{vpcID}),
				},
				{
					Name:   aws.String("group-name"),
					Values: aws.StringSlice([]string{infraID + "-winc-sg"}),
				},
			},
		})
		if err != nil || sgs == nil || len(sgs.SecurityGroups) == 0 {
			log.Fatalf("failed to create or find security group, %v", err)
		}
		sgID = sgs.SecurityGroups[0].GroupId
	} else {
		sgID = sg.GroupId
		// we only delete security group that is created with the instance. If it is reused, we will not log or delete SG when removing instances that are borrowing the SG.
		createdSG = SGInfo{
			Groupname: infraID + "-winc-sg",
			Groupid:   *sgID,
		}
	}
	// Specify the details of the instance
	runResult, err := svc.RunInstances(&ec2.RunInstancesInput{
		ImageId:            imageId,
		InstanceType:       instanceType,
		KeyName:            keyName,
		SubnetId:           aws.String(subnetID),
		MinCount:           aws.Int64(1),
		MaxCount:           aws.Int64(1),
		IamInstanceProfile: iamProfile,
		SecurityGroupIds:   []*string{sgID, aws.String(workerSG)},
	})
	if err != nil {
		log.Fatalf("Could not create instance: %v", err)
	} else {
		instanceID = runResult.Instances[0].InstanceId
		createdInst = InstanceInfo{
			Instanceid: *instanceID,
			SG:         []SGInfo{createdSG},
		}
		log.Println("Created instance", *instanceID)
	}
	_, err = svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: sgID,
		IpPermissions: []*ec2.IpPermission{
			(&ec2.IpPermission{}).
				SetIpProtocol("-1").
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp("10.0.0.0/16"),
				}),
			(&ec2.IpPermission{}).
				SetIpProtocol("tcp").
				SetFromPort(3389).
				SetToPort(3389).
				SetIpRanges([]*ec2.IpRange{
					(&ec2.IpRange{}).
						SetCidrIp(getMyIp() + "/32"),
				}),
		},
	})
	if err != nil {
		log.Printf("unable to set security group ingress, %v", err)
	}
	// Add tags to the created instance
	_, err = svc.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{runResult.Instances[0].InstanceId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(infraID + "-winNode"),
			},
		},
	})
	if err != nil {
		log.Println("Could not create tags for instance", runResult.Instances[0].InstanceId, err)
		return
	}
	ipRes, err := allocatePublicIp(svc)
	if err != nil {
		log.Printf("error allocating public ip to associate with instance, please manually allocate public ip, %v", err)
	} else {
		log.Println("waiting for the vm to be ready for attaching a public ip address...")
		err = svc.WaitUntilInstanceStatusOk(&ec2.DescribeInstanceStatusInput{
			InstanceIds: []*string{instanceID},
		})
		if err != nil {
			log.Printf("failed to wait for instance to be ok, %v", err)
		}
		_, err = svc.AssociateAddress(&ec2.AssociateAddressInput{
			AllocationId: ipRes.AllocationId,
			InstanceId:   instanceID,
		})
		if err != nil {
			log.Printf("failed to associate public ip for instance, %v", err)
		}
	}
	err = writeInstanceInfo(&Instances{createdInst}, path)
	if err != nil {
		log.Panicf("failed to write instance info to file at '%v', instance will not be able to be deleted, %v", *path, err)
	}
	log.Println("Successfully created windows node instance, please RDP into windows with the following:")
	log.Printf("xfreerdp /u:Administrator /v:%v  /h:1080 /w:1920 /p:'Secret2018'", *ipRes.PublicIp)
}

// DestroyEC2WinC destroys instances and security groups on AWS specified in file from the path and consume the file if succeeded
func DestroyEC2WinC(sess *session.Session, path *string) {
	svc := ec2.New(sess, aws.NewConfig())
	instances, err := readInstanceInfo(path)
	log.Printf("consuming file '%v'", *path)
	if err != nil {
		log.Fatalf("failed to read file from '%v', instance not deleted, %v", *path, err)
	}
	for _, inst := range *instances {
		for _, sg := range inst.SG {
			if sg.Groupid == "" {
				continue
			}
			err = deleteSG(svc, sg.Groupid)
			if err != nil {
				log.Printf("failed to delete security group: %v, %v", sg.Groupname, err)
			}
		}
		err = deleteInstance(svc, inst.Instanceid)
		if err != nil {
			log.Printf("failed to delete instance '%v', %v", inst.Instanceid, err)
		}
	}
	if err == nil {
		err = os.Remove(*path)
		if err != nil {
			log.Printf("failed to delete file at '%v'", err)
		}
	} else {
		log.Printf("file '%v' not deleted due to deletion error, %v", *path, err)
	}
}

// deleteSG will delete security group based on group id
// return error if deletion fails
func deleteSG(svc *ec2.EC2, groupid string) error {
	_, err := svc.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(groupid),
	})
	return err
}

// deleteInstance will delete an AWS instance based on instance id
// return error if deletion fails
func deleteInstance(svc *ec2.EC2, instanceID string) error {
	_, err := svc.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	return err
}

// writeInstanceInfo logs details of an created instance and append to a json file about instance id and attached security group
// return error if file write fails
func writeInstanceInfo(info *Instances, path *string) error {
	pastinfo, err := readInstanceInfo(path)
	if err == nil {
		for _, past := range *pastinfo {
			*info = append(*info, past)
		}
	}
	newinfo, err := json.Marshal(*info)
	if err != nil {
		return fmt.Errorf("failed to marshal information into json format, %v", err)
	}
	err = ioutil.WriteFile(*path, newinfo, 0644)
	if err != nil {
		return fmt.Errorf("failed to write instance info to file, deletion will have to be manual, %v", err)
	}
	return nil
}

// readInstanceInfo reads from a json file about instance id and attached security group
// return instance information including instance id, instance attached security group id and name, and error if fails
func readInstanceInfo(path *string) (*Instances, error) {
	if _, err := os.Stat(*path); os.IsNotExist(err) {
		return nil, fmt.Errorf("no InstanceInfo found at path '%v", *path)
	}
	var info Instances
	content, err := ioutil.ReadFile(*path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file at path '%v', %v", *path, err)
	}
	err = json.Unmarshal(content, &info)
	if err != nil {
		return nil, fmt.Errorf("failed to read json file at path '%v'", err)
	}
	return &info, nil
}

// getMyIp get the external IP of current machine from http://myexternalip.com
// TODO: Find a more reliable strategy than relying on a website
// returns external IP
func getMyIp() string {
	resp, err := http.Get("http://myexternalip.com/raw")
	if err != nil {
		log.Panic("Failed to get external IP Addr")
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		log.Panic("Failed to read external IP Addr")
	}
	return buf.String()
}

// allocatePublicIp find a randomly assigned ip by AWS that is available
// returns public ip related information and error messages if any
func allocatePublicIp(svc *ec2.EC2) (*ec2.AllocateAddressOutput, error) {
	ip, err := svc.AllocateAddress(&ec2.AllocateAddressInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to allocate an elastic ip, please assign public ip manually, %v", err)
	}
	return ip, nil
}

// getInfrastrcture gets information of current Infrastrcture refferred by the openshift client, each client should have only one infrastructure
// returns information of the infrastructure including infraID/InfrastructureName
func getInfrastrcture(c *client.Clientset) v1.Infrastructure {
	infra, err := c.ConfigV1().Infrastructures().List(metav1.ListOptions{})
	if err != nil || infra == nil || len(infra.Items) != 1 { // we should only have 1 infrastructure
		log.Fatalf("Error getting infrastructure, %v", err)
	}
	return infra.Items[0]
}

// getVPCByInfrastructure gets VPC of the infrastructure.
// returns VPC id and error messages
func getVPCByInfrastructure(svc *ec2.EC2, infra v1.Infrastructure) (string, error) {
	res, err := svc.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{infra.Status.InfrastructureName + "-vpc"}), //TODO: use this kubernetes.io/cluster/{infraName}: owned
			},
			{
				Name:   aws.String("state"),
				Values: aws.StringSlice([]string{"available"}),
			},
		},
	})
	if err != nil {
		log.Panicf("Unable to describe VPCs, %v", err)
	}
	if len(res.Vpcs) == 0 {
		log.Panicf("No VPCs found.")
		return "", err
	} else if len(res.Vpcs) > 1 {
		log.Panicf("More than one VPCs are found, we returned the first one")
	}
	//vpcAttri, err := svc.DescribeVpcAttribute(&ec2.DescribeVpcAttributeInput{
	//	Attribute:aws.String(ec2.VpcAttributeNameEnableDnsSupport),
	//	VpcId: res.Vpcs[0].VpcId,
	//})
	//if err != nil {
	//	log.Printf("failed to find vpc attribute, no public DNS assigned, %v", err)
	//}
	//vpcAttri.SetEnableDnsHostnames(&ec2.AttributeBooleanValue{Value: aws.Bool(true)})
	//vpcAttri.SetEnableDnsSupport(&ec2.AttributeBooleanValue{Value: aws.Bool(true)})
	return *res.Vpcs[0].VpcId, err
}

// getPubSubnetOrCreate gets the public subnet under a given vpc id. If no subnet is available, then it creates one.
// returns subent id and error messages
func getPubSubnetOrCreate(svc *ec2.EC2, vpcID, infraID string) (string, error) {
	// search subnet by the vpcid
	subnets, err := svc.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: aws.StringSlice([]string{vpcID}), // grab subnet owned by the vpcID
			},
		},
	})
	if err != nil {
		log.Printf("failed to search subnet based on given VpcID: %v, %v, will create one instead", vpcID, err)
		// create a subnet based on vpcID
		subnet, err := svc.CreateSubnet(&ec2.CreateSubnetInput{ // create subnet under the vpc (most likely not used since openshift-installer creates 6+ of them)
			CidrBlock: aws.String("10.0.0.0/16"),
			VpcId:     aws.String(vpcID),
		})
		if err != nil {
			log.Fatalf("Failed to search or create public subnet based on given VpcID: %v, %v", vpcID, err)
		}
		return *subnet.Subnet.SubnetId, err
	}
	for _, subnet := range subnets.Subnets { // find public subnet within the vpc
		for _, tag := range subnet.Tags {
			if *tag.Key == "Name" && strings.Contains(*tag.Value, infraID+"-public-") {
				return *subnet.SubnetId, err
			}
		}
	}
	return "", fmt.Errorf("failed to find public subnet in vpc: %v", vpcID)
}

// getClusterSGID gets security group id from an existing cluster either 'worker' or 'master'
// returns security group id
func getClusterSGID(svc *ec2.EC2, infraID, clusterFunction string) string {
	sg, err := svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{infraID + "-" + clusterFunction + "-sg"}),
			},
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + infraID),
				Values: aws.StringSlice([]string{"owned"}),
			},
		},
	})
	if err != nil {
		log.Panicf("Failed to attach security group of openshift cluster worker, please manually add it, %v", err)
	}
	if sg == nil || len(sg.SecurityGroups) != 1 {
		log.Panicf("nil or more than one security groups are found for the openshift cluster %v nodes, please add openshift cluster %v SG manually", clusterFunction, clusterFunction)
		return ""
	}
	return *sg.SecurityGroups[0].GroupId
}

// getIAMrole gets IAM information from an existing cluster either 'worker' or 'master'
// returns IAM information including IAM arn
func getIAMrole(svcIAM *iam.IAM, infraID, clusterFunction string) *ec2.IamInstanceProfileSpecification {
	iamspc, err := svcIAM.GetInstanceProfile(&iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(fmt.Sprintf("%s-%s-profile", infraID, clusterFunction)),
	})
	if err != nil {
		log.Panicf("failed to find iam role, please attache manually %v", err)
	}
	return &ec2.IamInstanceProfileSpecification{
		Arn: iamspc.InstanceProfile.Arn,
	}
}
