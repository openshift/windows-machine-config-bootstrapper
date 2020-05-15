package main

import (
	"flag"

	"github.com/spf13/cobra"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	// Controller-runtime's zap package redirects logs to StdErr by default. Functionality to set up the destination of
	// logs would require bumping up the version of controller-runtime to at least 0.4.0, which is dependent on
	// https://issues.redhat.com/browse/WINC-347
	// Here we set up the logger that sends logs to StdErr. Info level logs should be bubbled up to StdOut instead
	// WMCO interprets logs in StdErr as an indication that bootstrapping failed
	logger.SetLogger(zap.New())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Error(err, "wmcb execution failed")
	}
}
