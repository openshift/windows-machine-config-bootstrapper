package e2e_test

import (
	"flag"
	"os"
	"testing"

	"github.com/spf13/pflag"
)

func TestMain(m *testing.M) {
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	// TODO: Create windows node
	// TODO: Defer destroy windows node
	os.Exit(m.Run())

}
