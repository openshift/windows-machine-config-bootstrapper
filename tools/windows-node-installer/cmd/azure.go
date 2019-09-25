package cmd

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/azure"
	"github.com/spf13/cobra"
)

var (

	// azCreateFlagInfo contains information for creating an instance.
	azCreateFlagInfo struct {
		imageID      string
		instanceType string
		sshKey       string
		// ip address name to be attached to the instance.
		ipName string
		// nic name to be attached to the instance.
		nicName string
	}

	// azureOpts contains the specific cloud provider specific information
	azureOpts struct {
		// credentialPath is the location of the azure credentials file on the disk
		credentialPath string
		// credentialAccountID is the aws account id
		credentialAccountID string
	}
)

func init() {
	azureCmd := newAZCmd()
	rootCmd.AddCommand(azureCmd)
	azureCmd.AddCommand(azCreateCmd())
	azureCmd.AddCommand(azDestroyCmd())
}

func newAZCmd() *cobra.Command {
	azureCmd := &cobra.Command{
		Use:   "azure",
		Short: "Takes azure specific resource names from user",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return requiredAZFlags(cmd)
		},
		Run: func(_ *cobra.Command, args []string) {
			fmt.Println(args)
		},
	}
	azureCmd.PersistentFlags().StringVar(&azureOpts.credentialPath, "credentials", "",
		"file path to the cloud provider credentials of the existing OpenShift cluster (required).")

	azureCmd.PersistentFlags().StringVar(&azureOpts.credentialAccountID, "credential-account", "",
		"account name of a credential used to create the OpenShift Cluster specified in the provider's credentials"+
			" file. (required)")
	return azureCmd
}

// requiredazureFlags defines required flags for createInfo.
func requiredAZFlags(azureCmd *cobra.Command) error {
	err := azureCmd.MarkPersistentFlagRequired("credentials")
	if err != nil {
		return err
	}
	err = azureCmd.MarkPersistentFlagRequired("credential-account")
	if err != nil {
		return err
	}
	return nil
}

// createCmd defines `create` command and creates a Windows instance using parameters from the persistent flags to
// fill up information in azCreateFlagInfo. It uses PreRunE to check for whether required flags are provided.
func azCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Windows instance on the same provider as the existing OpenShift Cluster.",
		Long: "creates a Windows instance under the same virtual network (AWS-VCP, Azure-Vnet, " +
			"and etc.) used by a given OpenShift cluster running on the selected provider. " +
			"The created instance would be ready to join the OpenShift Cluster as a worker node.",
		TraverseChildren: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azureOpts.credentialPath,
				azureOpts.credentialAccountID, rootInfo.resourceTrackerDir)
			if err != nil {
				return fmt.Errorf("error creating cloud provider clients, %v", err)
			}

			// Type assertion to provide access to AzureProvider
			// so that we could pass azure specific info taken from the user.
			var az *azure.AzureProvider = cloud.(*azure.AzureProvider)
			az, ok := cloud.(*azure.AzureProvider)
			if ok {
				az.AzureUserInput.NicName = azCreateFlagInfo.nicName
				az.AzureUserInput.IpName = azCreateFlagInfo.ipName
			}

			err = cloud.CreateWindowsVM(azCreateFlagInfo.imageID, azCreateFlagInfo.instanceType, azCreateFlagInfo.sshKey)
			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.imageID, "image-id", "WindowsServer",
		"the image type to be used for instance creation (required)")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.instanceType, "instance-type", "2019-Datacenter",
		"SKU version of the image to be used  (required)")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.sshKey, "ssh-key", "",
		"name of existing ssh key on cloud provider for accessing the instance after it is created. (required)")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.ipName, "ipName", "",
		"ip name of the node to login")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.nicName, "nicName", "",
		"nic name for the node")
	return cmd
}

// destroyCmd defines `destroy` command and destroys resources specified in 'windows-node-installer.json' file.
func azDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy the Windows instances and resources specified in 'windows-node-installer.json' file.",
		Long: "Destroy all resources specified in 'windows-node-installer.json' file in the current or specified" +
			" directory, including instances and security groups. " +
			"The security groups still associated with any existing instances will not be deleted.",

		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azureOpts.credentialPath,
				azureOpts.credentialAccountID, rootInfo.resourceTrackerDir)
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
