package cloudprovider

import (
	"os"
	"testing"
)

// getCWD retuns the current working directory after supressing the path resolution error
func getCWD() string {
	cwd, _ := os.Getwd()
	return cwd
}

// TestMakeValidAbsPath tests if makeValidAbsPath is returning an error or not in various conditions.
// TODO: Add more test cases here
func TestMakeValidAbsPath(t *testing.T) {
	tests := []struct {
		description   string
		inputPath     string
		expectedPath  string
		expectedError bool
	}{
		{
			description:  "Empty path should resolve to current working directory",
			inputPath:    "",
			expectedPath: getCWD() + "/",
		},
		{
			description:   "Non existent file should return an error and should resolve to empty string",
			inputPath:     "/somerandomPath",
			expectedPath:  "",
			expectedError: true,
		},
	}
	for _, test := range tests {
		actualPath, err := makeValidAbsPath(test.inputPath)
		if test.expectedError && err == nil {
			t.Fatal("Expected error but did not get any error")
		}

		if actualPath != test.expectedPath {
			t.Fatalf("Expected path to be resolved to %s but got %s", test.expectedPath, actualPath)
		}
	}

}
