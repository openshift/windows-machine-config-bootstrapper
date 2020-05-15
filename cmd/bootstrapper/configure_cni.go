package main

import (
	"flag"
	"os"

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/bootstrapper"
	"github.com/spf13/cobra"
)

var (
	// configureCNICmd describes the configure-cni command
	configureCNICmd = &cobra.Command{
		Use:   "configure-cni",
		Short: "Configures CNI on the Windows node",
		Long: "Configures CNI on the Windows node. " +
			"This command needs to be executed every time initialize-kubelet is executed.",
		Run: runConfigureCNICmd,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			err := cmd.MarkPersistentFlagRequired("cni-dir")
			if err != nil {
				return err
			}
			err = cmd.MarkPersistentFlagRequired("cni-config")
			if err != nil {
				return err
			}
			return nil
		},
	}

	// configureCNIOpts holds the configure-cni CLI options
	configureCNIOpts struct {
		// dir is the location where the CNI binaries are present
		dir string
		// config is the location of the CNI configuration
		config string
		// installDir is the main installation directory
		installDir string
	}
)

func init() {
	rootCmd.AddCommand(configureCNICmd)
	configureCNICmd.PersistentFlags().StringVar(&configureCNIOpts.installDir, "install-dir", "c:\\k",
		"Installation directory. Defaults to C:\\k")
	configureCNICmd.PersistentFlags().StringVar(&configureCNIOpts.dir, "cni-dir", "",
		"The location of the CNI binaries")
	configureCNICmd.PersistentFlags().StringVar(&configureCNIOpts.config, "cni-config", "",
		"The location of the CNI configuration file")
}

// runConfigureCNICmd configures the CNI on the Windows node
func runConfigureCNICmd(cmd *cobra.Command, args []string) {
	flag.Parse()

	wmcb, err := bootstrapper.NewWinNodeBootstrapper(configureCNIOpts.installDir, "", "", configureCNIOpts.dir,
		configureCNIOpts.config)
	if err != nil {
		log.Error(err, "could not create bootstrapper")
		os.Exit(1)
	}

	err = wmcb.Configure()
	if err != nil {
		log.Error(err, "could not configure CNI")
		os.Exit(1)
	}
	// Send success message to StdOut for WSU to ascertain that CNI configuration was successful
	os.Stdout.WriteString("CNI configuration completed successfully")

	err = wmcb.Disconnect()
	if err != nil {
		log.Error(err, "can't clean up bootstrapper")
	}
}
