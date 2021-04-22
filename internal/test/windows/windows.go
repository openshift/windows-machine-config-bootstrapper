package windows

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/credentials"
)

const (
	// remotePowerShellCmdPrefix holds the PowerShell prefix that needs to be prefixed  for every remote PowerShell
	// command executed on the remote Windows VM
	remotePowerShellCmdPrefix = "powershell.exe -NonInteractive -ExecutionPolicy Bypass "
)

// Windows represents a Windows host.
type Windows struct {
	// Credentials is used for storing the credentials for Windows VMs created
	Credentials *credentials.Credentials
	// SSHClient contains the ssh client information to access the Windows VM via ssh
	SSHClient *ssh.Client
}

// WindowsVM is the interface for interacting with a Windows object created by the cloud provider
type WindowsVM interface {
	// CopyDirectory copies the files from the directory on the local host to the directory on the remote Windows VM
	// This does not copy nested directories
	CopyDirectory(string, string) error
	// CopyFile copies the given file to the remote directory in the Windows VM. The remote directory is created if it
	// does not exist
	CopyFile(string, string) error
	// Run executes the given command remotely on the Windows VM over a ssh connection and returns the combined output
	// of stdout and stderr. If the bool is set, it implies that the cmd is to be execute in PowerShell. This function
	// should be used in scenarios where you want to execute a command that runs in the background. In these cases we
	// have observed that Run() returns before the command completes and as a result killing the process.
	Run(string, bool) (string, error)
	// GetCredentials returns the interface for accessing the VM credentials. It is up to the caller to check if non-nil
	// Credentials are returned before usage.
	GetCredentials() *credentials.Credentials
	// Reinitialize re-initializes the Windows VM. Presently only the ssh client is reinitialized.
	Reinitialize() error
}

func (w *Windows) CopyFile(filePath, remoteDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("CopyFile cannot be called without a SSH client")
	}

	ftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp client initialization failed: %v", err)
	}
	defer ftp.Close()

	log.Printf("Copying %s file to Windows VM: %v", filePath, remoteDir)

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

func (w *Windows) CopyDirectory(localDir string, remoteDir string) error {
	if w.SSHClient == nil {
		return fmt.Errorf("CopyDirectory cannot be called without a SSH client")
	}

	sftp, err := sftp.NewClient(w.SSHClient)
	if err != nil {
		return fmt.Errorf("sftp initialization failed: %v", err)
	}
	defer sftp.Close()

	log.Printf("Copying %s directory to Windows VM: %s", localDir, remoteDir)

	// creating a remote directory to store the files.
	if err = sftp.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("could not create %s: %v", remoteDir, err)
	}

	// Get the list of all files in the directory
	localFiles, err := ioutil.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("error opening local directory %s: %v", localDir, err)
	}

	for _, localFile := range localFiles {
		localPath := filepath.Join(localDir, localFile.Name())
		// transfer only files and not directories
		if localFile.IsDir() {
			log.Printf("Skipping %s directory", localPath)
			continue
		}
		if err = w.CopyFile(localPath, remoteDir); err != nil {
			return fmt.Errorf("error while copying %s file to Windows VM: %v", localPath, err)
		}
	}
	return nil
}

func (w *Windows) Run(cmd string, psCmd bool) (string, error) {
	if w.SSHClient == nil {
		return "", fmt.Errorf("Run cannot be called without a ssh client")
	}

	session, err := w.SSHClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	if psCmd {
		cmd = remotePowerShellCmdPrefix + cmd
	}

	out, err := session.CombinedOutput(cmd)
	return string(out), err
}

func (w *Windows) GetCredentials() *credentials.Credentials {
	return w.Credentials
}

// GetSSHClient gets the ssh client associated with Windows VM created
func (w *Windows) GetSSHClient() error {
	if w.SSHClient != nil {
		// Close the existing client to be on the safe side
		if err := w.SSHClient.Close(); err != nil {
			log.Printf("warning - error closing ssh client connection: %v", err)
		}
	}

	config := &ssh.ClientConfig{
		User:            w.Credentials.UserName(), //TODO: Change this to make sure that this works for Azure.
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(w.Credentials.SSHKey())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", w.Credentials.IPAddress()+":22", config)
	if err != nil {
		return fmt.Errorf("failed to dial to ssh server: %s", err)
	}
	w.SSHClient = sshClient
	return nil
}

func (w *Windows) Reinitialize() error {
	if err := w.GetSSHClient(); err != nil {
		return fmt.Errorf("failed to reinitialize ssh client: %v", err)
	}
	return nil
}

// RetrieveDirectories recursively copies the files and directories from the directory in the remote Windows VM
// to the given directory on the local host.
func (w *Windows) RetrieveDirectories(remoteDir string, localDir string) error {
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
func (w *Windows) copyFileFrom(sftp *sftp.Client, remotePath, localPath string) error {
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
