package e2e_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// TODO: Create windows node
	// TODO: Defer destroy windows node
	os.Exit(m.Run())

}
