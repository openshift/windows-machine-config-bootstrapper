package framework

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	"github.com/pkg/sftp"
)

const (
	// sshKey is the key that will be used to access created Windows VMs
	sshKey = "libra"
)

// cloudProvider holds the information related to cloud provider
// TODO: Move this to proper location which can destroy the VM that got created.
//		https://issues.redhat.com/browse/WINC-245
var cloudProvider cloudprovider.Cloud

// testWindowsVM holds the information related to the test Windows VM. This should hold the specialized information
// related to test suite.
type testWindowsVM struct {
	*types.Windows
	// buildWMCB indicates if WSU should build WMCB and use it
	// TODO This is a WSU specific property and should be moved to wsu_test -> https://issues.redhat.com/browse/WINC-249
	buildWMCB bool
}

// TestWindowsVM is the interface for interacting with a Windows VM in the test framework. This will hold the
// specialized information related to test suite
type TestWindowsVM interface {
	// RetrieveDirectories recursively copies the files and directories from the directory in the remote Windows VM
	// to the given directory on the local host.
	RetrieveDirectories(string, string) error
	// Destroy destroys the Windows VM
	// TODO: Remove this and move it to framework or other higher level object capable of doing deletions.
	//		jira: https://issues.redhat.com/browse/WINC-243
	Destroy() error
	// BuildWMCB returns the value of buildWMCB. It can be used by WSU to decide if it should build WMCB before using it
	BuildWMCB() bool
	// SetBuildWMCB sets the value of buildWMCB. Setting buildWMCB to true would indicate WSU will build WMCB instead of
	// downloading the latest as per the cluster version. False by default
	SetBuildWMCB(bool)
	// Compose the Windows VM we have from WNI
	types.WindowsVM
}

// newWindowsVM creates and sets up a Windows VM in the cloud and returns the WindowsVM interface that can be used to
// interact with the VM. If credentials are passed then it is assumed that VM already exists in the cloud and those
// credentials will be used to interact with the VM. If no error is returned then it is guaranteed that the VM was
// created and can be interacted with. If skipSetup is true, then configuration steps are skipped.
func newWindowsVM(imageID, instanceType string, credentials *types.Credentials, skipSetup bool) (TestWindowsVM, error) {
	w := &testWindowsVM{}
	var err error

	cloudProvider, err = cloudprovider.CloudProviderFactory(kubeconfig, awsCredentials, "default", artifactDir,
		imageID, instanceType, sshKey, privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("error instantiating cloud provider %v", err)
	}

	if credentials == nil {
		windowsVM, err := cloudProvider.CreateWindowsVM()
		if err != nil {
			return nil, fmt.Errorf("error creating Windows VM: %v", err)
		}
		// TypeAssert to the WindowsVM struct we want
		winVM, ok := windowsVM.(*types.Windows)
		if !ok {
			return nil, fmt.Errorf("error asserting Windows VM: %v", err)
		}
		w.Windows = winVM
	} else {
		//TODO: Add username as well, as it will change depending on cloud provider
		if credentials.GetIPAddress() == "" || credentials.GetPassword() == "" {
			return nil, fmt.Errorf("password or IP address not specified in credentials")
		}
		w.Credentials = credentials
	}

	if err := w.SetupWinRMClient(); err != nil {
		return w, fmt.Errorf("failed to setup winRM client for the Windows VM: %v", err)
	}
	// Wait for some time before starting configuring of ssh server. This is to let sshd service be available
	// in the list of services
	// TODO: Parse the output of the `Get-Service sshd, ssh-agent` on the Windows node to check if the windows nodes
	// has those services present
	if !skipSetup {
		time.Sleep(time.Minute)
		if err := w.ConfigureOpenSSHServer(); err != nil {
			return w, fmt.Errorf("failed to configure OpenSSHServer on the Windows VM: %v", err)
		}
	}
	if err := w.GetSSHClient(); err != nil {
		return w, fmt.Errorf("failed to get ssh client for the Windows VM created: %v", err)
	}

	return w, nil
}

func (w *testWindowsVM) RetrieveDirectories(remoteDir string, localDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("cannot retrieve remote directory without a ssh client")
	}

	// creating a local directory to store the files and directories from remote directory.
	err := os.MkdirAll(localDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not create %s: %v", localDir, err)
	}

	sftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp initialization failed: %v", err)
	}
	defer sftp.Close()

	// Get the list of all files in the directory
	remoteFiles, err := sftp.ReadDir(remoteDir)
	if err != nil {
		return fmt.Errorf("error opening remote directory %s: %v", remoteDir, err)
	}

	for _, remoteFile := range remoteFiles {
		remotePath := filepath.Join(remoteDir, remoteFile.Name())
		localPath := filepath.Join(localDir, remoteFile.Name())
		// check if it is a directory, call itself again
		if remoteFile.IsDir() {
			if err = w.RetrieveDirectories(remotePath, localPath); err != nil {
				log.Printf("error while retrieving %s directory from Windows : %v", remotePath, err)
			}
		} else {
			// logging errors as a best effort to retrieve files from remote directory
			if err = w.copyFileFrom(sftp, remotePath, localPath); err != nil {
				log.Printf("error while retrieving %s file from Windows : %v", remotePath, err)
			}
		}
	}
	return nil
}

// copyFileFrom copies a file from remote directory to the local directory.
func (w *testWindowsVM) copyFileFrom(sftp *sftp.Client, remotePath, localPath string) error {
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("error creating file locally: %v", err)
	}
	// TODO: Check if there is some performance implication of multiple Open calls.
	remoteFile, err := sftp.Open(remotePath)
	if err != nil {
		return fmt.Errorf("error while opening remote file on the Windows VM: %v", err)
	}
	// logging the errors instead of returning to allow closing of files
	_, err = io.Copy(localFile, remoteFile)
	if err != nil {
		log.Printf("error retrieving file %v from Windows VM: %v", localPath, err)
	}
	// flush memory
	if err = localFile.Sync(); err != nil {
		log.Printf("error flusing memory: %v", err)
	}
	if err := remoteFile.Close(); err != nil {
		log.Printf("error closing file on the remote host %s", localPath)
	}
	if err := localFile.Close(); err != nil {
		log.Printf("error closing file %s locally", localPath)
	}
	return nil
}

func (w *testWindowsVM) Destroy() error {
	// There is no VM to destroy
	if cloudProvider == nil || w.Windows == nil || w.GetCredentials() == nil {
		return nil
	}
	return cloudProvider.DestroyWindowsVMs()
}

func (w *testWindowsVM) BuildWMCB() bool {
	return w.buildWMCB
}

func (w *testWindowsVM) SetBuildWMCB(buildWMCB bool) {
	w.buildWMCB = buildWMCB
}
