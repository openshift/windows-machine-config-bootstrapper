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
	// provide the urn of the imageID for the instance to be created.
	imageID string
	// provide the instance flavor.
	instanceType string
	// service principal credential file compatible with `--sdk-auth` compatible format.
	credentialPath string
	// provide the corresponding subscriptionID where the node need to be created.
	subscriptionID string
	// provide the IP address if the installer doesn't want to create one.
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

// newAZCmd defines azure command for the wni, this asks for the mandatory azure credential file.
// By default subscriptionID is derived from the credential file else it will be overwritten by the input
// given by the user.
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
	azureCmd.PersistentFlags().StringVar(&azCreateFlagInfo.subscriptionID, "subscriptionID", "",
		"provide the azure subscriptionID for the node to be created.")

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

// setEnvVariable returns the subscriptionID from the credential file.
func setEnvVariable(filePath string) (subscriptionID string, err error) {
	os.Setenv("AZURE_AUTH_LOCATION", filePath)
	getFileSettings, err := auth.GetSettingsFromFile()
	if err != nil {
		return "", err
	}
	subscriptionID = getFileSettings.GetSubscriptionID()
	return
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
			if azCreateFlagInfo.subscriptionID == "" {
				subscriptionID, err := setEnvVariable(azCreateFlagInfo.credentialPath)
				azCreateFlagInfo.subscriptionID = subscriptionID
				if err != nil {
					return fmt.Errorf("eror getting subscription ID, %v", err)
				}
			}
			// Provide the azCreateFlagInfo.credentialAccountID as a default
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azCreateFlagInfo.credentialPath,
				azCreateFlagInfo.subscriptionID, rootInfo.resourceTrackerDir,
				azCreateFlagInfo.imageID, azCreateFlagInfo.instanceType, "")
			if err != nil {
				return fmt.Errorf("error creating azure clients, %v", err)
			}

			// Type assertion to provide access to AzureProvider
			// so that we could pass azure specific info taken from the user.
			var az *azure.AzureProvider = cloud.(*azure.AzureProvider)
			az, ok := cloud.(*azure.AzureProvider)
			if ok {
				az.NicName = azCreateFlagInfo.nicName
				az.IpName = azCreateFlagInfo.ipName
			} else {
				return fmt.Errorf("error type asserting. %v", err)
			}

			err = cloud.CreateWindowsVM()
			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	// specify the urn of the image-id, by default "MicrosoftWindowsServer:WindowsServer:2019-Datacenter:latest" is considered, but to override
	// pass the value with flag `image-id`.
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.imageID, "image-id", "MicrosoftWindowsServer:WindowsServer:2019-Datacenter-with-Containers:latest",
		"image-id to be used for node creation, for more info "+
			"https://docs.microsoft.com/bs-latn-ba/azure/virtual-machines/windows/cli-ps-findimage#table-of-commonly-used-windows-images\n")

	// specify the instance flavor for the node to be created, by default "Basic_A1" is considered, but to override
	// pass the value with flag `instance-type`.
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.instanceType, "instance-type", "Standard_B1s",
		"instance-type for node creation, for more info "+
			"https://docs.microsoft.com/en-us/azure/virtual-machines/linux/sizes-general#b-series")

	// provide the value for `ipName` argument if the installer doesn't want to create one, by default it will create
	// one even though we explicitly give `""`.
	cmd.PersistentFlags().StringVar(&azCreateFlagInfo.ipName, "ipName", "",
		"ip resource name for the node to login")

	// provide the value for `nicName` argument if the installer doesn't want to create one, by default it will create
	// one even though we explicitly give `""`. This is implemented considering UPI into perspective
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
			if azCreateFlagInfo.subscriptionID == "" {
				subscriptionID, err := setEnvVariable(azCreateFlagInfo.credentialPath)
				azCreateFlagInfo.subscriptionID = subscriptionID
				if err != nil {
					return fmt.Errorf("eror getting subscription ID, %v", err)
				}
			}
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, azCreateFlagInfo.credentialPath,
				azCreateFlagInfo.subscriptionID, rootInfo.resourceTrackerDir,
				azCreateFlagInfo.imageID, azCreateFlagInfo.instanceType, "")
			if err != nil {
				return fmt.Errorf("error creating azure clients, %v", err)
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
