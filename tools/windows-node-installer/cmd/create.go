package main

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/spf13/cobra"
)

var (
	// createFlagInfo contains information for creating an instance.
	createFlagInfo struct {
		imageID      string
		instanceType string
		sshKey       string
	}
)

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
		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, rootInfo.credentialPath,
				rootInfo.credentialAccountID, rootInfo.resourceTrackerDir)
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
