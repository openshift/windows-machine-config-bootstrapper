package wmcb

import (
	"flag"
	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"log"
	"os"
	"testing"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
// TODO: Create a temporary remote directory on the Windows node
var (
	framework = &e2ef.TestFramework{RemoteDir: "C:\\Temp"}
	// binaryToBeTransferred holds the binary that needs to be transferred to the Windows VM
	// TODO: Make this an array later with a comma separated values for more binaries to be transferred
	binaryToBeTransferred = flag.String("binaryToBeTransferred", "",
		"Absolute path of the binary to be transferred")
	// vmCount is the number of VMs the test suite requires
	vmCount = 1
)

func TestMain(m *testing.M) {
	flag.Parse()
	err := framework.Setup(vmCount)
	if err != nil {
		framework.TearDown()
		log.Fatal(err)
	}
	defer framework.TearDown()
	os.Exit(m.Run())
	// TODO: Add one more check to remove lingering cloud resources
}
