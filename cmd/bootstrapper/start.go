package main

import (
	"flag"

	"github.com/openshift/windows-machine-config-operator/pkg/bootstrapper"
	"github.com/spf13/cobra"
)

var (
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Starts Windows Machine Config Bootstrapper",
		Long:  "",
		Run:   runStartCmd,
	}

	startOpts struct {
		// The location of the ignition file
		ignitionFile string
		// The location where the kubelet.exe has been downloaded to
		kubeletPath string
	}
)

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.PersistentFlags().StringVar(&startOpts.ignitionFile, "ignition-file", "",
		"Ignition file location to bootstrap the windows node")
	startCmd.PersistentFlags().StringVar(&startOpts.kubeletPath, "kubelet-path", "",
		"Kubelet file location to bootstrap the windows node")
}

// runStartCmd starts the windows machine config bootstrapper
func runStartCmd(cmd *cobra.Command, args []string) {
	flag.Set("logtostderr", "true")
	flag.Parse()
	// TODO: add validation for flags
	bootstrapper.NewWinNodeBootstrapper(startOpts.ignitionFile, startOpts.kubeletPath)
}
