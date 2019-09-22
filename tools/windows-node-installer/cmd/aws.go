package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
)

var (

	// createFlagInfo contains information for creating an instance.
	createFlagInfo struct {
		imageID      string
		instanceType string
		sshKey       string
	}

	// awsOpts contains the specific cloud provider specific information
	awsOpts struct {
		// credentialPath is the location of the aws credentials file on the disk
		credentialPath string
		// credentialAccountID is the aws account id
		credentialAccountID string
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
		Short: "Takes aws specific resource names from user",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return requiredAWSFlags(cmd)
		},
		Run: func(_ *cobra.Command, args []string) {
			fmt.Println(args)
		},
	}
	awsCmd.PersistentFlags().StringVar(&awsOpts.credentialPath, "credentials", "",
		"file path to the cloud provider credentials of the existing OpenShift cluster (required).")

	awsCmd.PersistentFlags().StringVar(&awsOpts.credentialAccountID, "credential-account", "",
		"account name of a credential used to create the OpenShift Cluster specified in the provider's credentials"+
			" file. (required)")
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
		Long: "creates a Windows instance under the same virtual network (AWS-VCP, Azure-Vnet, " +
			"and etc.) used by a given OpenShift cluster running on the selected provider. " +
			"The created instance would be ready to join the OpenShift Cluster as a worker node.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return validateCreateFlags(cmd)
		},
		TraverseChildren: true,
		RunE: func(_ *cobra.Command, args []string) error {
			cloud, err := cloudprovider.CloudProviderFactory( rootInfo.kubeconfigPath, awsOpts.credentialPath,
				awsOpts.credentialAccountID,rootInfo.resourceTrackerDir)
			if err != nil {
				return fmt.Errorf("error creating cloud provider clients, %v", err)
			}
			err = cloud.CreateWindowsVM(createFlagInfo.imageID, createFlagInfo.instanceType, createFlagInfo.sshKey)
			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&createFlagInfo.imageID, "image-id", "",
		"ami ID of a base image for the instance (i.e."+
			": ami-06a4e829b8bbad61e for Microsoft Windows Server 2019 Base image on AWS). (required)")
	cmd.PersistentFlags().StringVar(&createFlagInfo.instanceType, "instance-type", "",
		"name of a type of instance (i.e.: m4.large for AWS, etc). (required)")
	cmd.PersistentFlags().StringVar(&createFlagInfo.sshKey, "ssh-key", "",
		"name of existing ssh key on cloud provider for accessing the instance after it is created. (required)")
	return cmd
}

// validateCreateFlags defines required flags for createCmd.
func validateCreateFlags(createCmd *cobra.Command) error {
	err := createCmd.MarkPersistentFlagRequired("image-id")
	if err != nil {
		return err
	}
	err = createCmd.MarkPersistentFlagRequired("instance-type")
	if err != nil {
		return err
	}
	err = createCmd.MarkPersistentFlagRequired("ssh-key")
	if err != nil {
		return err
	}
	return nil
}

// destroyCmd defines `destroy` command and destroys resources specified in 'windows-node-installer.json' file.
func destroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy the Windows instances and resources specified in 'windows-node-installer.json' file.",
		Long: "Destroy all resources specified in 'windows-node-installer.json' file in the current or specified" +
			" directory, including instances and security groups. " +
			"The security groups still associated with any existing instances will not be deleted.",

		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, awsOpts.credentialPath,
				awsOpts.credentialAccountID, rootInfo.resourceTrackerDir)
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
