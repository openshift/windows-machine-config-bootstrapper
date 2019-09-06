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
		logLevel            string
	}
)

// main is the entry point for windows-node-installer.
func main() {
	rootCmd := newRootCmd()

	for _, subCmd := range []*cobra.Command{
		createCmd(),
		destroyCmd(),
	} {
		rootCmd.AddCommand(subCmd)
	}
	err := rootCmd.Execute()
	if err != nil {
		log.Error(err, "error executing window-node-installer")
	}
}

// newRootCmd defines `windows-node-installer` command with persistent flags to fill up information in rootInfo.
// It uses PreRunE to check for whether required flags are provided.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "wni",
		Short: "Creates and destroys Windows instance with an existing OpenShift cluster.",
		PreRunE:func(cmd *cobra.Command, args []string) error {
			return validateRootFlags(cmd)
		},
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return setLogLevel([]byte(rootInfo.logLevel))
		},
	}

	rootCmd.PersistentFlags().StringVar(&rootInfo.kubeconfigPath, "kubeconfig", "",
		"file path to the kubeconfig of the existing OpenShift cluster (required).")

	rootCmd.PersistentFlags().StringVar(&rootInfo.credentialPath, "credentials", "",
		"file path to the cloud provider credentials of the existing OpenShift cluster (required).")

	rootCmd.PersistentFlags().StringVar(&rootInfo.credentialAccountID, "credential-account", "",
		"account name of a credential used to create the OpenShift Cluster specified in the provider's credentials"+
			" file. (required)")

	rootCmd.PersistentFlags().StringVar(&rootInfo.resourceTrackerDir, "dir", ".",
		"directory to save or read window-node-installer.json file from.")

	rootCmd.PersistentFlags().StringVar(&rootInfo.logLevel, "log-level", "info", "log level (e.g. 'info')")
	return rootCmd
}

// validateRootFlags defines required flags for rootCmd and set the global log level.
func validateRootFlags(rootCmd *cobra.Command) error {
	err := rootCmd.MarkPersistentFlagRequired("kubeconfig")
	if err != nil {
		return err
	}
	err = rootCmd.MarkPersistentFlagRequired("credentials")
	if err != nil {
		return err
	}
	err = rootCmd.MarkPersistentFlagRequired("credential-account")
	if err != nil {
		return err
	}
	return nil
}
