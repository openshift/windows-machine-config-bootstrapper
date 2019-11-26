package wsu

import (
	e2ef "github.com/openshift/windows-machine-config-operator/internal/test/framework"
	"os"
	"testing"
)

// framework holds the instantiation of test suite being executed. As of now, temp dir is hardcoded.
// TODO: Create a temporary remote directory on the Windows node
var framework = &e2ef.TestFramework{RemoteDir: "C:\\Temp"}

func TestMain(m *testing.M) {
	framework.Setup()
	testStatus := m.Run()
	// TODO: Add one more check to remove lingering cloud resources
	framework.TearDown()
	os.Exit(testStatus)
}
