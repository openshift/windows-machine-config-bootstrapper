package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// rootInfo contains necessary information for creating Cloud instance from cloudprovider package.
	rootInfo struct {
		kubeconfigPath      string
		credentialPath      string
		credentialAccountID string
		resourceTrackerDir  string
	}
	// rootCmd contains the wni root command for the Windows Node Installer
	rootCmd = &cobra.Command{
		Use:   "wni",
		Short: "Creates and destroys Windows instance with an existing OpenShift cluster.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateRootFlags(cmd)
		},
	}
)

// newRootCmd defines `windows-node-installer` command with persistent flags to fill up information in rootInfo.
// It uses PreRunE to check for whether required flags are provided.
func init() {
	rootCmd.PersistentFlags().StringVar(&rootInfo.kubeconfigPath, "kubeconfig", "",
		"file path to the kubeconfig of the existing OpenShift cluster (required)")

	rootCmd.PersistentFlags().StringVar(&rootInfo.resourceTrackerDir, "dir", ".",
		"directory to save or read windows-node-installer.json file from")
}

// validateRootFlags defines required flags for rootCmd
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
