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
	// CopyFileTo copies the given file to the remote directory in the Windows VM. The remote directory is created if it
	// does not exist
	CopyFileTo(string, string) error
	// RetrieveFiles retrieves the list of file from the directory in the remote Windows VM to the local host. As of
	// now, we're limiting every file in the remote directory to be written to single directory on the local host
	RetrieveFiles(string, string) error
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

func (w *testWindowsVM) CopyFileTo(filePath, remoteDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("CopyFileTo cannot be called without a SSH client")
	}

	ftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp client initialization failed: %v", err)
	}
	defer ftp.Close()

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening %s file to be transferred: %v", filePath, err)
	}
	defer f.Close()

	if err = ftp.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("error creating remote directory %s: %v", remoteDir, err)
	}

	remoteFile := remoteDir + "\\" + filepath.Base(filePath)
	dstFile, err := ftp.Create(remoteFile)
	if err != nil {
		return fmt.Errorf("error initializing %s file on Windows VMs: %v", remoteFile, err)
	}

	_, err = io.Copy(dstFile, f)
	if err != nil {
		return fmt.Errorf("error copying %s to the Windows VM: %v", filePath, err)
	}

	// Forcefully close it so that we can execute the binary later
	dstFile.Close()
	return nil
}

// RetrieveFiles retrieves list of files from remote directory to the local directory.
// The implementation can be changed if the use-case arises. As of now, we're doing a best effort
// to collect every log possible. If a retrieval of file fails, we would proceed with retrieval
// of other log files.
func (w *testWindowsVM) RetrieveFiles(remoteDir, localDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("RetrieveFile cannot be called without a ssh client")
	}

	// Create local dir
	err := os.MkdirAll(localDir, os.ModePerm)
	if err != nil {
		log.Printf("could not create %s: %s", localDir, err)
	}

	sftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp initialization failed: %v", err)
	}
	defer sftp.Close()

	// Get the list of all files in the directory
	remoteFiles, err := sftp.ReadDir(remoteDir)
	if err != nil {
		return fmt.Errorf("error opening remote file: %v", err)
	}

	for _, remoteFile := range remoteFiles {
		// Assumption: We ignore the directories here the reason being RetrieveFiles should just retrieve files
		// in a directory, if this is directory, we should have called RetrieveFiles on this directory
		if remoteFile.IsDir() {
			continue
		}
		fileName := remoteFile.Name()
		dstFile, err := os.Create(filepath.Join(localDir, fileName))
		if err != nil {
			log.Printf("error creating file locally: %v", err)
			continue
		}
		// TODO: Check if there is some performance implication of multiple Open calls.
		srcFile, err := sftp.Open(remoteDir + "\\" + fileName)

		if err != nil {
			log.Printf("error while opening remote directory on the Windows VM: %v", err)
			continue
		}
		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			log.Printf("error retrieving file %v from Windows VM: %v", fileName, err)
			continue
		}
		// flush memory
		if err = dstFile.Sync(); err != nil {
			log.Printf("error flusing memory: %v", err)
			continue
		}
		if err := srcFile.Close(); err != nil {
			log.Printf("error closing file on the remote host %s", fileName)
			continue
		}
		if err := dstFile.Close(); err != nil {
			log.Printf("error closing file %s locally", fileName)
			continue
		}
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
