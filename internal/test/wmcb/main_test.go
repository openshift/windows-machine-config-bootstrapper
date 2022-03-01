package wmcb

import (
	"flag"
	"log"
	"os"
	"testing"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
var (
	// Initialize wmcbFramework which specializes TestFramework by adding some properties specific to WMCB tests
	framework = wmcbFramework{}
	// TODO: expose this to the end user as a command line flag
	// vmCount is the number of VMs the test suite requires
	vmCount       = 1
	dockerRuntime bool
)

func TestMain(m *testing.M) {
	var skipVMSetup bool

	flag.BoolVar(&skipVMSetup, "skipVMSetup", false, "Option to disable setup in the VMs")
	flag.BoolVar(&dockerRuntime, "dockerRuntime", true, "Container runtime to be used is docker")

	flag.Parse()

	err := framework.Setup(vmCount, skipVMSetup)
	if err != nil {
		framework.TearDown()
		log.Fatal(err)
	}
	testStatus := m.Run()
	// Retrieve artifacts after running the test
	framework.RetrieveArtifacts()
	// TODO: Add one more check to remove lingering cloud resources
	framework.TearDown()
	os.Exit(testStatus)
}
