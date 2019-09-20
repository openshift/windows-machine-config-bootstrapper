package main

import (
	"github.com/spf13/cobra"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	// log is the global logger for the main package.
	log = logger.Log.WithName("windows-node-installer")

	// rootInfo contains necessary information for creating Cloud instance from cloudprovider package.
	rootInfo struct {
		kubeconfigPath      string
		credentialPath      string
		credentialAccountID string
		resourceTrackerDir  string
	}
)

// main is the entry point for windows-node-installer.
func main() {
	rootCmd := newRootCmd()
	createCmd := createCmd()
	createCmd.AddCommand(azureCmd())

	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(destroyCmd())
	err := rootCmd.Execute()
	if err != nil {
		log.Error(err, "error executing window-node-installer")
	}
}

// newRootCmd defines `windows-node-installer` command with persistent flags to fill up information in rootInfo.
// It uses PreRunE to check for whether required flags are provided.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "windows-node-installer",
		Short: "Creates and destroys Windows instance with an existing OpenShift cluster.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return requiredRootFlags(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&rootInfo.kubeconfigPath, "kubeconfig", "",
		"file path to the kubeconfig of the existing OpenShift cluster.")

	rootCmd.PersistentFlags().StringVar(&rootInfo.credentialPath, "credentials-dir", "", "file path to the cloud provider credentials of the existing OpenShift cluster.")

	rootCmd.PersistentFlags().StringVar(&rootInfo.credentialAccountID, "credential-account", "",
		"account name of a credential used to create the OpenShift Cluster specified in the provider's credentials"+
			" file.")

	rootCmd.PersistentFlags().StringVar(&rootInfo.resourceTrackerDir, "dir", ".",
		"directory to save or read window-node-installer.json file from.")

	return rootCmd
}

// requiredRootFlags defines required flags for rootCmd.
func requiredRootFlags(rootCmd *cobra.Command) error {
	err := rootCmd.MarkPersistentFlagRequired("kubeconfig")
	if err != nil {
		return err
	}
	err = rootCmd.MarkPersistentFlagRequired("credentials-dir")
	if err != nil {
		return err
	}
	err = rootCmd.MarkPersistentFlagRequired("credential-account")
	if err != nil {
		return err
	}
	return nil
}
