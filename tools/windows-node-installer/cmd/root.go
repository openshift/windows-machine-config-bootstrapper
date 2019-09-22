package cmd

import (
	"fmt"
	"os"

	wmco "github.com/openshift/windows-machine-config-operator/log"
	"github.com/spf13/cobra"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	// rootInfo contains necessary information for creating Cloud instance from cloudprovider package.
	rootInfo struct {
		kubeconfigPath      string
		credentialPath      string
		credentialAccountID string
		resourceTrackerDir  string
		logLevel            string
	}
	// rootCmd contains the wni root command for the Windows Node Installer
	rootCmd = &cobra.Command{
		Use:   "wni",
		Short: "Creates and destroys Windows instance with an existing OpenShift cluster.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateRootFlags(cmd)
		},
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return wmco.SetLogLevel([]byte(rootInfo.logLevel))
		},
	}

	// log is the global logger for the main package.
	log = logger.Log.WithName("windows-node-installer")
)

// newRootCmd defines `windows-node-installer` command with persistent flags to fill up information in rootInfo.
// It uses PreRunE to check for whether required flags are provided.
func init() {
	rootCmd.PersistentFlags().StringVar(&rootInfo.kubeconfigPath, "kubeconfig", "",
		"file path to the kubeconfig of the existing OpenShift cluster (required).")

	rootCmd.PersistentFlags().StringVar(&rootInfo.resourceTrackerDir, "dir", ".",
		"directory to save or read window-node-installer.json file from.")

	rootCmd.PersistentFlags().StringVar(&rootInfo.logLevel, "log-level", "info", "log level (e.g. 'info')")

}

// validateRootFlags defines required flags for rootCmd and set the global log level.
func validateRootFlags(rootCmd *cobra.Command) error {
	err := rootCmd.MarkPersistentFlagRequired("kubeconfig")
	if err != nil {
		return err
	}
	return nil
}

// Execute run the actual wmi command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
