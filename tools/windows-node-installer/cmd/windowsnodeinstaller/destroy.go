package main

import (
	"fmt"

	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/spf13/cobra"
)

// destroyCmd defines `destroy` command and destroys resources specified in 'windows-node-installer.json' file.
func destroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy the Windows instances and resources specified in 'windows-node-installer.json' file.",
		Long: "Destroy all resources specified in 'windows-node-installer.json' file in the current or specified" +
			" directory, including instances and security groups. " +
			"The security groups still associated with any existing instances will not be deleted.",
		Args: cobra.ExactArgs(0),

		RunE: func(_ *cobra.Command, _ []string) error {
			cloud, err := cloudprovider.CloudProviderFactory(rootInfo.kubeconfigPath, rootInfo.credentialPath,
				rootInfo.credentialAccountID, rootInfo.resourceTrackerDir)
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
