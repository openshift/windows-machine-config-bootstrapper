package wsu

import (
	"flag"
	e2ef "github.com/openshift/windows-machine-config-bootstrapper/internal/test/framework"
	"log"
	"os"
	"testing"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
var (
	framework = &e2ef.TestFramework{}
	// TODO: expose this to the end user as a command line flag
	// vmCount is the number of VMs the test suite requires
	vmCount = 1
)

func TestMain(m *testing.M) {
	var vmCreds e2ef.Creds
	var skipVMSetup bool

	flag.Var(&vmCreds, "vmCreds", "List of VM credentials")
	flag.BoolVar(&skipVMSetup, "skipVMSetup", false, "Option to disable setup in the VMs")
	flag.Parse()

	err := framework.Setup(vmCount, vmCreds, skipVMSetup)
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
