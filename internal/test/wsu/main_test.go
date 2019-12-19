package wsu

import (
	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"log"
	"os"
	"testing"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
// TODO: Create a temporary remote directory on the Windows node
var (
	framework = &e2ef.TestFramework{RemoteDir: "C:\\Temp"}
	// vmCount is the number of VMs the test suite requires. Using multiple to cover each playbook configuration option.
	vmCount = 2
)

func TestMain(m *testing.M) {
	err := framework.Setup(vmCount)
	if err != nil {
		framework.TearDown()
		log.Fatal(err)
	}
	defer framework.TearDown()
	os.Exit(m.Run())
	// TODO: Add one more check to remove lingering cloud resources
}
