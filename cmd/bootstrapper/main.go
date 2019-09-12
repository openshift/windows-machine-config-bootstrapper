package main

import (
	"flag"

	"github.com/spf13/cobra"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	componentName = "wmcb" // wmcb is the name of the binary
)

var (
	rootCmd = &cobra.Command{
		Use:   componentName,
		Short: "Run Windows machine config bootstrapper",
		Long: "Runs the Machine Config Bootstrapper which is responsible for bootstrapping the windows to ensure that" +
			"the node can join existing OpenShift cluster",
	}
	log = logger.Log.WithName("wmcb")
)

func init() {
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Error(err, "wmcb execution failed")
	}
}
