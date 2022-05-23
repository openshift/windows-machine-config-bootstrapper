package main

import (
	"flag"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
)

var (
	// uninstallKubeletCmd describes the uninstall-kubelet command
	uninstallKubeletCmd = &cobra.Command{
		Use:   "uninstall-kubelet",
		Short: "Stops and removes the kubelet service",
		Long:  "Stops and removes the kubelet service",
		Run:   runUninstallKubeletCmd,
	}
)

func init() {
	rootCmd.AddCommand(uninstallKubeletCmd)
}

// runUninstallKubeletCmd uninstalls kubelet service from the Windows node
func runUninstallKubeletCmd(cmd *cobra.Command, args []string) {
	flag.Parse()
	wmcb, err := bootstrapper.NewWinNodeBootstrapper("", "", "", "", "",
		"")
	if err != nil {
		log.Error(err, "could not create bootstrapper")
		os.Exit(1)
	}

	// uninstall kubelet Windows service
	if err = wmcb.UninstallKubelet(); err != nil {
		log.Error(err, "could not uninstall kubelet")
		os.Exit(1)
	}

	// Send success message to StdOut to ascertain that kubelet removal was successful
	os.Stdout.WriteString("kubelet uninstalled successfully")

	if err = wmcb.Disconnect(); err != nil {
		log.Error(err, "can't clean up bootstrapper")
	}
}
