package cmd

import (
	"fmt"

	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/spf13/cobra"
)

var (

	// awsInfo contains information for aws Cloud provider
	awsInfo struct {
		// imageID is the AMI image-id to be used for creating Virtual Machine
		imageID string
		// instanceType is the flavor of VM to be used
		instanceType string
		// sshKey is the ssh key to access the VM created. Please note that key should be uploaded to AWS before
		// using this flag
		sshKey string
		// credentialPath is the location of the aws credentials file on the disk
		credentialPath string
		// credentialAccountID is the aws account id
		credentialAccountID string
		// privateKeyPath is the location of the private key on the machine for the public key uploaded to AWS
		// This is used to decrypt the password for the Windows locally
		privateKeyPath string
	}
)

func init() {
	awsCmd := newAWSCmd()
	rootCmd.AddCommand(awsCmd)
	awsCmd.AddCommand(createCmd())
	awsCmd.AddCommand(destroyCmd())
}

func newAWSCmd() *cobra.Command {
	awsCmd := &cobra.Command{
		Use:   "aws",
		Short: "Create and destroy windows instances in aws",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return requiredAWSFlags(cmd)
		},
	}
	awsCmd.PersistentFlags().StringVar(&awsInfo.credentialPath, "credentials", "",
		"file path to the cloud provider credentials of the existing OpenShift cluster (required)")

	awsCmd.PersistentFlags().StringVar(&awsInfo.credentialAccountID, "credential-account", "",
		"account name of a credential used to create the OpenShift Cluster specified in the provider's credentials"+
			" file (required)")
	return awsCmd
}

// requiredAWSFlags makes certain flags mandatory for the aws provider
func requiredAWSFlags(awsCmd *cobra.Command) error {
	err := awsCmd.MarkPersistentFlagRequired("credentials")
	if err != nil {
		return err
	}
	err = awsCmd.MarkPersistentFlagRequired("credential-account")
	if err != nil {
		return err
	}
	return nil
}

// createCmd defines `create` command and creates a Windows instance using parameters from the persistent flags to
// fill up information in createFlagInfo. It uses PreRunE to check for whether required flags are provided.
func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Windows instance on the same provider as the existing OpenShift Cluster.",
		Long: "creates a Windows instance under the same virtual network (AWS-VCP " +
			"used by a given OpenShift cluster running on the selected provider. " +
			"The created instance would be ready to join the OpenShift Cluster as a worker node.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return validateCreateFlags(cmd)
		},
		TraverseChildren: true,
		RunE: func(_ *cobra.Command, args []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, awsInfo.credentialPath,
				awsInfo.credentialAccountID, rootInfo.resourceTrackerDir,
				awsInfo.imageID, awsInfo.instanceType, awsInfo.sshKey, awsInfo.privateKeyPath)
			if err != nil {
				return fmt.Errorf("error creating aws client, %v", err)
			}
			// TODO: Use the Windows VM object to get password, user name etc here.
			_, err = cloud.CreateWindowsVM()
			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&awsInfo.imageID, "image-id", "",
		"ami ID of a base image for the instance (i.e."+
			": ami-06a4e829b8bbad61e for Microsoft Windows Server 2019 Base image on AWS)")
	cmd.PersistentFlags().StringVar(&awsInfo.instanceType, "instance-type", "",
		"name of a type of instance (i.e.: m4.large for AWS, etc) (required)")
	cmd.PersistentFlags().StringVar(&awsInfo.sshKey, "ssh-key", "",
		"name of existing ssh key on cloud provider for accessing the instance after it is created (required)")
	cmd.PersistentFlags().StringVar(&awsInfo.privateKeyPath, "private-key", "",
		"path of the private key for accessing the instance after it is created (required)")
	return cmd
}

// validateCreateFlags defines required flags for createCmd.
func validateCreateFlags(createCmd *cobra.Command) error {
	err := createCmd.MarkPersistentFlagRequired("instance-type")
	if err != nil {
		return err
	}
	err = createCmd.MarkPersistentFlagRequired("ssh-key")
	if err != nil {
		return err
	}
	err = createCmd.MarkPersistentFlagRequired("private-key")
	if err != nil {
		return err
	}
	return nil
}

// destroyCmd defines `destroy` command and destroys resources specified in 'windows-node-installer.json' file.
func destroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy all security groups and instances specified in 'windows-node-installer.json' file.",
		Long: "Destroy all resources specified in 'windows-node-installer.json' file in the current or specified" +
			" directory, including instances and security groups. " +
			"The security groups still associated with any existing instances will not be deleted.",

		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, awsInfo.credentialPath,
				awsInfo.credentialAccountID, rootInfo.resourceTrackerDir, "", "", "", "")
			if err != nil {
				return fmt.Errorf("error creating cloud provider clients, %v", err)
			}
			err = cloud.DestroyWindowsVMs()
			if err != nil {
				return fmt.Errorf("error destroying Windows instance, %v", err)
			}
			return nil
		},
	}
	return cmd
}
