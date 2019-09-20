package resource

import (
	"encoding/json"
	"github.com/coreos/etcd/pkg/fileutil"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test cases including just security group ID, instance ID, both, or none as input.
var (
	infoList = []installerInfo{
		{
			[]string{},
			[]string{"sg-1234567890"},
		},
		{
			[]string{"i-1234567890"},
			[]string{},
		},
		{
			[]string{"i-0e8e5e1766c3cc636"},
			[]string{"sg-005998c6a70973fab"},
		},
		{
			[]string{},
			[]string{},
		},
	}
	expectedInfo = installerInfo{
		[]string{"i-0e8e5e1766c3cc636", "i-1234567890"},
		[]string{"sg-005998c6a70973fab", "sg-1234567890"},
	}
	sampleFilePath = "./data/sample-windows-node-installer.json"
)

// TestAppendInstallerInfo write to a temp file and append all information from infoList and is compared with the
// expectedInfo.
func TestAppendInstallerInfo(t *testing.T) {
	tmpFile, err := ioutil.TempFile("."+string(os.PathSeparator), "*.json")
	assert.NoError(t, err, "error making temp file in the current folder")
	filePath := tmpFile.Name()
	defer os.Remove(filePath)

	for _, info := range infoList {
		err := AppendInstallerInfo(info.InstanceIDs, info.SecurityGroupIDs, filePath)
		assert.NoError(t, err)
	}

	readInfo, err := ioutil.ReadFile(filePath)
	assert.NoError(t, err)
	expectedInfoByte, err := json.Marshal(expectedInfo)
	assert.NoError(t, err)
	assert.Equal(t, expectedInfoByte, readInfo)
}

// TestReadInstallerInfo reads from 'sample-windows-node-installer.json' file and compare its value with expectedInfo.
func TestReadInstallerInfo(t *testing.T) {
	readInfo, err := ReadInstallerInfo(sampleFilePath)
	assert.NoError(t, err)
	assert.Equal(t, expectedInfo, *readInfo)
}

// TestRemoveInstallerInfo creates a temp file from expectedInfo which is the sum of infoList, removes the first
// and second entries, and compares the result with just the third entry which should be the only information left.
// It then removes the third entry which should be left with an empty file where the removeInstallerInfo would clean
// up by deleting the file.
func TestRemoveInstallerInfo(t *testing.T) {
	tmpFile, err := ioutil.TempFile("."+string(os.PathSeparator), "*.json")
	assert.NoError(t, err, "error making temp file in the current folder")
	filePath := tmpFile.Name()

	expectedInfoByte, err := json.Marshal(expectedInfo)
	assert.NoError(t, err)
	err = ioutil.WriteFile(filePath, expectedInfoByte, 0644)
	assert.NoError(t, err)

	err = RemoveInstallerInfo(infoList[0].InstanceIDs, infoList[0].SecurityGroupIDs, filePath)
	assert.NoError(t, err)
	err = RemoveInstallerInfo(infoList[1].InstanceIDs, infoList[1].SecurityGroupIDs, filePath)
	assert.NoError(t, err)

	readInfo, err := ioutil.ReadFile(filePath)
	assert.NoError(t, err)
	someInfoByte, err := json.Marshal(&infoList[2])
	assert.NoError(t, err)
	assert.Equal(t, someInfoByte, readInfo)

	err = RemoveInstallerInfo(infoList[2].InstanceIDs, infoList[2].SecurityGroupIDs, filePath)
	assert.NoError(t, err)
	assert.False(t, fileutil.Exist(filePath))
}

// TestMakeFilePath tests against dir path ending with\without path separator and fails if they do not produce the
// correct file path.
func TestMakeFilePath(t *testing.T) {
	pathSeparator := string(os.PathSeparator)

	filePath, err := MakeFilePath(strings.Join([]string{".", "tmp"}, pathSeparator))
	assert.Error(t, err, "MakeFilePath should throw error if dir dose not exist")

	dirName, err := ioutil.TempDir(".", "tmp")
	assert.NoError(t, err, "error making temp directory in the current folder")
	defer os.Remove(dirName)

	expectedPath := strings.Join([]string{".", dirName, "windows-node-installer.json"}, pathSeparator)

	filePath, err = MakeFilePath(strings.Join([]string{".", dirName}, pathSeparator))
	assert.NoError(t, err)
	assert.Equal(t, expectedPath, filePath)

	filePath, err = MakeFilePath(strings.Join([]string{".", dirName, ""}, pathSeparator))
	assert.NoError(t, err)
	assert.Equal(t, expectedPath, filePath)
}
