package main

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	azure "github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider/azure"
	"github.com/spf13/cobra"
)

var (
	// azureInfo contains all the information that the user want to provide during
	// the node installation i.e this would be useful during the UPI based install method.
	azureInfo struct {
		// Provide the IP Address that tobe attached to the instance.
		ipName string
		// Provide the NIC Name that tobe attached to the instance.
		nicName string
	}
)

// createCmd defines `create` command and creates a Windows instance with persistent flags to fill up information in
// createInfo. It uses PreRunE to check for whether required flags are provided.
func azureCmd() *cobra.Command {
	azureCmd := &cobra.Command{
		Use:   "azure",
		Short: "Takes azure specific resource names from user",
		Long: "performs a Windows instance create/delete operation by considering " +
			"the user input. windowsnodeinstaller first looks for the data provided by " +
			"the user if not provided then it creates for you.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return requiredAzureFlags(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, rootInfo.credentialPath,
				rootInfo.credentialAccountID, rootInfo.resourceTrackerDir)
			if err != nil {
				return fmt.Errorf("error creating cloud provider clients, %v", err)
			}

			// Type assertion to provide access to AzureProvider
			// so that we could pass azure specific info taken from the user.
			var az *azure.AzureProvider = cloud.(*azure.AzureProvider)
			az, ok := cloud.(*azure.AzureProvider)
			if ok {
				az.AzureUserInput.NicName = azureInfo.nicName
				az.AzureUserInput.IpName = azureInfo.ipName
			}

			err = cloud.CreateWindowsVM(createInfo.imageID, createInfo.instanceType, createInfo.sshKey)

			if err != nil {
				return fmt.Errorf("error creating Windows Instance, %v", err)
			}
			return nil
		},
	}

	azureCmd.PersistentFlags().StringVar(&azureInfo.ipName, "ipName", "",
		"ip name of the node to ssh.")
	azureCmd.PersistentFlags().StringVar(&azureInfo.nicName, "nicName", "",
		"nic name for the node.")

	return azureCmd
}

// requiredazureFlags defines required flags for createInfo.
func requiredAzureFlags(installerCmd *cobra.Command) error {
	err := installerCmd.MarkPersistentFlagRequired("ipName")
	if err != nil {
		return err
	}
	err = installerCmd.MarkPersistentFlagRequired("nicName")
	if err != nil {
		return err
	}
	return nil
}
