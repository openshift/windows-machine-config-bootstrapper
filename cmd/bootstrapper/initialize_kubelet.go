package main

import (
	"flag"
	"os"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
	"github.com/spf13/cobra"
)

var (
	initializeKubeletCmd = &cobra.Command{
		Use:   "initialize-kubelet",
		Short: "Initializes the kubelet service on the Windows node",
		Long: "Initializes the kubelet service on the Windows node. " +
			"If this command is run after configure-cni is executed, it will overwrite the CNI options.",
		Run: runInitializeKubeletCmd,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			err := cmd.MarkPersistentFlagRequired("ignition-file")
			if err != nil {
				return err
			}
			err = cmd.MarkPersistentFlagRequired("kubelet-path")
			if err != nil {
				return err
			}
			return nil
		},
	}

	initializeKubeletOpts struct {
		// The location of the ignition file
		ignitionFile string
		// The location where the kubelet.exe has been downloaded to
		kubeletPath string
		// The directory to install the kubelet and related files
		installDir string
		// forceCloudNone ensures the the kubelet `cloud` flag is set to none
		forceCloudNone bool
	}
)

func init() {
	rootCmd.AddCommand(initializeKubeletCmd)
	initializeKubeletCmd.PersistentFlags().StringVar(&initializeKubeletOpts.ignitionFile, "ignition-file", "",
		"Ignition file location to bootstrap the Windows node")
	initializeKubeletCmd.PersistentFlags().StringVar(&initializeKubeletOpts.kubeletPath, "kubelet-path", "",
		"Kubelet file location to bootstrap the Windows node")
	initializeKubeletCmd.PersistentFlags().StringVar(&initializeKubeletOpts.installDir, "install-dir", "c:\\k",
		"Kubelet file location to bootstrap the Windows node. Defaults to C:\\k")
	initializeKubeletCmd.PersistentFlags().BoolVar(&initializeKubeletOpts.forceCloudNone, "force-cloud-none", false,
		"If set, the cluster's platform will be ignored and kubelet will not use any cloud specific features")
}

// runInitializeKubeletCmd starts the Windows Machine Config Bootstrapper
func runInitializeKubeletCmd(cmd *cobra.Command, args []string) {
	flag.Parse()
	// TODO: add validation for flags

	wmcb, err := bootstrapper.NewWinNodeBootstrapper(initializeKubeletOpts.installDir,
		initializeKubeletOpts.ignitionFile, initializeKubeletOpts.kubeletPath, "", "",
		initializeKubeletOpts.forceCloudNone)
	if err != nil {
		log.Error(err, "could not create bootstrapper")
		os.Exit(1)
	}

	err = wmcb.InitializeKubelet()
	if err != nil {
		log.Error(err, "could not run bootstrapper")
		os.Exit(1)
	} else {
		// Send success message to StdOut for WSU to ascertain that bootstrapping was successful
		os.Stdout.WriteString("Bootstrapping completed successfully")
	}

	err = wmcb.Disconnect()
	if err != nil {
		log.Error(err, "can't clean up bootstrapper")
	}
}
