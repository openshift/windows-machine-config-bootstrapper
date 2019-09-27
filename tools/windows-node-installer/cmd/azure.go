package cmd

import (
	"fmt"
	"os"

	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/azure"
	"github.com/spf13/cobra"
)

// azCreateFlagInfo contains azure specific information for creating an instance.
// the fields inside the struct gets filled once the flags are parsed.
// all the fields are optional except credentialPath.
var azCreateFlagInfo struct {
	imageID             string
	instanceType        string
	credentialPath      string
	credentialAccountID string
	// Provide the IP address if the installer doesn't want to create one.
	ipName string
	// Provide the nic name if the installer doesn't want to create one.
	nicName string
}

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
	}
	azureCmd.PersistentFlags().StringVar(&azCreateFlagInfo.credentialPath, "credentials", "",
		"file location to the azure cloud provider credentials (required).")

	return azureCmd
}

// requiredazureFlags defines required flags for createInfo.
func requiredAZFlags(azureCmd *cobra.Command) error {
	err := azureCmd.MarkPersistentFlagRequired("credentials")
	if err != nil {
		return err
	}
	return nil
}

// createCmd defines `create` command and creates a Windows instance using parameters from the persistent flags to
// fill up information in azCreateFlagInfo.
func azCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Windows node on the Azure cloud provider.",
		Long: "creates a Windows instance under the same Vnet of" +
			"the existing OpenShift cluster. " +
			"The created instance would be used as a worker node for the OpenShift Cluster.",
		TraverseChildren: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			os.Setenv("AZURE_AUTH_LOCATION", azCreateFlagInfo.credentialPath)
			getFileSettings, err := auth.GetSettingsFromFile()
			if err != nil {
				return fmt.Errorf("unable to get the settings from path: %v", err)
			}
			if azCreateFlagInfo.credentialAccountID == "" {
				azCreateFlagInfo.credentialAccountID = getFileSettings.GetSubscriptionID()
			}
			// Provide the azCreateFlagInfo.credentialAccountID as a default
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azCreateFlagInfo.credentialPath,
				azCreateFlagInfo.credentialAccountID, rootInfo.resourceTrackerDir)
			if err != nil {
				return fmt.Errorf("error creating cloud provider clients, %v", err)
			}

			// Type assertion to provide access to AzureProvider
			// so that we could pass azure specific info taken from the user.
			var az *azure.AzureProvider = cloud.(*azure.AzureProvider)
			az, ok := cloud.(*azure.AzureProvider)
			if ok {
				az.NicName = azCreateFlagInfo.nicName
				az.IpName = azCreateFlagInfo.ipName
			}

			err = cloud.CreateWindowsVM(azCreateFlagInfo.imageID, azCreateFlagInfo.instanceType, "")
			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.imageID, "image-id", "MicrosoftWindowsServer:WindowsServer:2019-Datacenter:latest",
		"image-id to be used for node creation ")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.instanceType, "instance-type", "Basic_A1",
		"instance-type for node creation, for more info "+
			"https://docs.microsoft.com/en-us/rest/api/compute/virtualmachines/listavailablesizes ")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.ipName, "ipName", "",
		"ip resource name for the node to login")
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.nicName, "nicName", "",
		"nic resource name for the node")
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
			os.Setenv("AZURE_AUTH_LOCATION", azCreateFlagInfo.credentialPath)
			getFileSettings, err := auth.GetSettingsFromFile()
			if err != nil {
				return fmt.Errorf("unable to get the settings from path: %v", err)
			}
			if azCreateFlagInfo.credentialAccountID == "" {
				azCreateFlagInfo.credentialAccountID = getFileSettings.GetSubscriptionID()
			}
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azCreateFlagInfo.credentialPath,
				azCreateFlagInfo.credentialAccountID, rootInfo.resourceTrackerDir)
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
