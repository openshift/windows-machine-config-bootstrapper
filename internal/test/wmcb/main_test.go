package wmcb

import (
	"log"
	"os"
	"testing"

	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
var (
	framework = &e2ef.TestFramework{}
	// TODO: expose this to the end user as a command line flag
	// vmCount is the number of VMs the test suite requires
	vmCount = 1
)

func TestMain(m *testing.M) {
	err := framework.Setup(vmCount)
	if err != nil {
		framework.TearDown()
		log.Fatal(err)
	}
	testStatus := m.Run()
	// TODO: Add one more check to remove lingering cloud resources
	framework.TearDown()
	os.Exit(testStatus)
}
