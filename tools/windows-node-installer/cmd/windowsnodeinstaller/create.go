package main

import (
	"github.com/spf13/cobra"
)

var (
	// createInfo contains all generic information for creating an instance.
	createInfo struct {
		imageID      string
		instanceType string
		sshKey       string
	}
)

// createCmd defines `create` command and creates a Windows instance with persistent flags to fill up information in
// createInfo. It uses PreRunE to check for whether required flags are provided.
func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Windows instance on the same provider as the existing OpenShift Cluster.",
		Long: "creates a Windows instance under the same virtual network (AWS-VCP, Azure-Vnet, " +
			"and etc.) used by a given OpenShift cluster running on the selected provider. " +
			"The created instance would be ready to join the OpenShift Cluster as a worker node.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return requiredCreateFlags(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&createInfo.imageID, "image-id", "",
		"ami ID of a base image for the instance (i.e."+
			": ami-06a4e829b8bbad61e for Microsoft Windows Server 2019 Base image).")
	cmd.PersistentFlags().StringVar(&createInfo.instanceType, "instance-type", "",
		"name of a type of instance (i.e.: m4.large, t2.micro, etc).")
	cmd.PersistentFlags().StringVar(&createInfo.sshKey, "ssh-key", "",
		"name of existing ssh key on cloud provider for accessing the instance after it is created.")
	return cmd
}

// requiredCreateFlags defines required flags for createInfo.
func requiredCreateFlags(installerCmd *cobra.Command) error {
	err := installerCmd.MarkPersistentFlagRequired("image-id")
	if err != nil {
		return err
	}
	err = installerCmd.MarkPersistentFlagRequired("instance-type")
	if err != nil {
		return err
	}
	err = installerCmd.MarkPersistentFlagRequired("ssh-key")
	if err != nil {
		return err
	}
	installerCmd.AddCommand(azureCmd())
	return nil
}
