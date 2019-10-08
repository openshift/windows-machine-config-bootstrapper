package resource

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"sort"
	"strings"
)

// installerInfo is directly written to `windows-node-installer.json` file.
// It contains IDs of created instances and security groups.
type installerInfo struct {
	InstanceIDs      []string `json:"InstanceIDs"`
	SecurityGroupIDs []string `json:"SecurityGroupIDs"`
}

// installerInfoFileName is the file name of installer info.
const installerInfoFileName = "windows-node-installer.json"

// AppendInstallerInfo appends instance id and security group to a json file and return error if file write fails.
func AppendInstallerInfo(instanceIDs, sgIDs []string, filePath string) error {
	info := installerInfo{InstanceIDs: instanceIDs, SecurityGroupIDs: sgIDs}

	pastInfo, err := ReadInstallerInfo(filePath)
	if err == nil {
		info.InstanceIDs, err = writeToInfo(pastInfo.InstanceIDs, info.InstanceIDs, true)
		if err != nil {
			return err
		}
		info.SecurityGroupIDs, err = writeToInfo(pastInfo.SecurityGroupIDs, info.SecurityGroupIDs, true)
		if err != nil {
			return err
		}
	}

	newInfo, err := json.Marshal(info)
	if err != nil {
		return err
	}

	// Write file with permission read and write for the owner and read for all other users.
	err = ioutil.WriteFile(filePath, newInfo, 0644)
	if err != nil {
		return err
	}
	return nil
}

// ReadInstallerInfo reads instance id and security group from a json file and return error if file read fails.
func ReadInstallerInfo(filePath string) (*installerInfo, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var info installerInfo
	err = json.Unmarshal(content, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// RemoveInstallerInfo removes instance id and security group from a json file and return error if removal fails.
func RemoveInstallerInfo(instanceIDs, sgIDs []string, filePath string) error {
	newInfo := installerInfo{InstanceIDs: instanceIDs, SecurityGroupIDs: sgIDs}

	pastInfo, err := ReadInstallerInfo(filePath)
	if err != nil {
		return err
	}
	newInfo.InstanceIDs, err = writeToInfo(pastInfo.InstanceIDs, newInfo.InstanceIDs, false)
	if err != nil {
		return err
	}
	newInfo.SecurityGroupIDs, err = writeToInfo(pastInfo.SecurityGroupIDs, newInfo.SecurityGroupIDs, false)
	if err != nil {
		return err
	}

	// Delete file if it is empty.
	if reflect.DeepEqual(newInfo, installerInfo{[]string{}, []string{}}) {
		return os.Remove(filePath)
	}

	// Remove info from existing json file.
	newMarshalledInfo, err := json.Marshal(&newInfo)
	if err != nil {
		return err
	}

	// Rewrite the json file
	err = ioutil.WriteFile(filePath, newMarshalledInfo, 0644)
	if err != nil {
		return err
	}
	return nil
}

// doesPathExist checks if file or directory path exist and return bool indication or checking error.
func doesDirPathExist(dirPath string) (bool, error) {
	if pathInfo, err := os.Stat(dirPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} else if !pathInfo.IsDir() {
		return false, fmt.Errorf("input directoy path is not a directoy")
	} else {
		return true, nil
	}
}

// writeToInfo adds or deletes newInfo from info with isAddToInfo flag indicating add (true) or delete (false).
// Function does not allow duplicated addition or deletion of a non-existing information.
// The function returns a sorted slice of strings or an error.
func writeToInfo(info, newInfo []string, isAddToInfo bool) ([]string, error) {
	sort.Strings(info)

	for _, newInformation := range newInfo {
		if newInformation == "" {
			continue
		}
		// Using binary search through sorted strings.
		index := sort.SearchStrings(info, newInformation)

		if isAddToInfo && (len(info) <= index || info[index] != newInformation) {
			// Insert newInformation into info at a sorted location without duplication.
			info = append(info[:index], append([]string{newInformation}, info[index:]...)...)

		} else if !isAddToInfo && len(info) > index && info[index] == newInformation {
			// Delete newInformation from info and we do not expect duplicates.
			info = append(info[:index], info[index+1:]...)

		} else if isAddToInfo && len(info) > index && info[index] == newInformation {
			return nil, fmt.Errorf("%s already exist", newInformation)

		} else if !isAddToInfo && (len(info) <= index || info[index] != newInformation) {
			return nil, fmt.Errorf("%s is not found", newInformation)
		}
	}
	return info, nil
}

// MakeFilePath checks if dir/file path is valid and adds the installerInfoFileName after if path is a directory.
func MakeFilePath(dirPath string) (string, error) {
	pathExist, err := doesDirPathExist(dirPath)
	if err != nil {
		return "", err
	}
	if !pathExist {
		return "", fmt.Errorf("directory path does not exist")
	}

	pathSeparator := string(os.PathSeparator)
	if dirPath[len(dirPath)-1:] != pathSeparator {
		return strings.Join([]string{dirPath, installerInfoFileName}, pathSeparator), err
	}
	return strings.Join([]string{dirPath, installerInfoFileName}, ""), err
}

// StoreCredentialData stores the access related information in a file created in resourceTrackerDir
func StoreCredentialData(filePath, fileData string) (err error) {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte(fileData))
	if err != nil {
		return err
	}
	return
}

// DeleteCredentialData deletes the file created to stores access related information.
func DeleteCredentialData(filePath string) (err error) {
	err = os.Remove(filePath)
	return
}
