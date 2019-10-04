package cloudprovider

import (
	"io/ioutil"
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
	// create Temporary file for testing
	file, _ := ioutil.TempFile("/tmp", "test-")
	fileName := file.Name()
	defer os.Remove(fileName)
	// create Temporary directory for testing
	dir, _ := ioutil.TempDir("/tmp", "test-")
	defer os.Remove(dir)

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
		{
			description:   "a temporary file which exists on machine should not get a trailing /",
			inputPath:     fileName,
			expectedPath:  fileName,
			expectedError: false,
		},
		{
			description:   "a temporary dir which exists on machine should get a trailing /",
			inputPath:     dir,
			expectedPath:  dir + "/",
			expectedError: false,
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
