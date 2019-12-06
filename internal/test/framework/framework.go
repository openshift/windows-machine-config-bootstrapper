package framework

import (
	"fmt"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"time"

	"github.com/masterzen/winrm"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"golang.org/x/crypto/ssh"
	"k8s.io/client-go/kubernetes"
)

const (
	// user for the Windows node created.
	// TODO: remove this hardcoding to any user.
	user = "Administrator"
	// winrm port to be used
	winRMPort = 5986
	// remotePowerShellCmdPrefix holds the powershell prefix that needs to be prefixed to every command run on the
	// remote powershell session opened
	remotePowerShellCmdPrefix = "powershell.exe -NonInteractive -ExecutionPolicy Bypass "
)

// TestFramework holds the info to run the test suite.
// This is not clean
type TestFramework struct {
	// Credentials to access the Windows VM created
	Credentials *types.Credentials
	// WinrmClient to access the Windows VM created
	WinrmClient *winrm.Client
	// remoteDir is the directory to which files will be transferred to, on the Windows VM
	RemoteDir string
	// SSHClient contains the ssh client information to access the Windows VM via ssh
	SSHClient *ssh.Client
	// CloudProvider holds the information related to cloud provider
	cloudProvider cloudprovider.Cloud
	// k8sclientset is the kubernetes clientset we will use to query the cluster's status
	K8sclientset *kubernetes.Clientset
}

// Setup sets up the Windows node so that it can join the existing OpenShift cluster
// TODO: move this to return error and do assertions there
func (f *TestFramework) Setup() {
	if err := f.createWindowsVM(); err != nil {
		log.Fatalf("failed to create Windows VM: %v", err)
	}
	// TODO: Add some options to skip certain parts of the test
	if err := f.setupWinRMClient(); err != nil {
		log.Fatalf("failed to setup winRM client for the Windows VM: %v", err)
	}
	// Wait for some time before starting configuring of ssh server. This is to let sshd service be available
	// in the list of services
	// TODO: Parse the output of the `Get-Service sshd, ssh-agent` on the Windows node to check if the windows nodes
	// has those services present
	time.Sleep(time.Minute)
	if err := f.configureOpenSSHServer(); err != nil {
		log.Fatalf("failed to configure OpenSSHServer on the Windows VM: %v", err)
	}
	if err := f.createRemoteDir(); err != nil {
		log.Fatalf("failed to create remote dir with error: %v", err)
	}
	if err := f.getSSHClient(); err != nil {
		log.Fatalf("failed to get ssh client for the Windows VM created: %v", err)
	}
	if err := f.getKubeClient(); err != nil {
		log.Fatalf("failed to get kube client with error: %v", err)
	}
}

// getKubeClient setups the kubeclient that can be used across all the test suites.
func (f *TestFramework) getKubeClient() error {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("could not build config from flags: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create k8s clientset: %v", err)
	}
	f.K8sclientset = clientset
	return nil
}

// createWindowsVM spins up the Windows VM in the given cloud provider and gives us the credentials to access the
// windows VM created
func (f *TestFramework) createWindowsVM() error {
	// TODO: Add validation for the environment variables to see if they're properly populated or not
	kubeconfig := os.Getenv("KUBECONFIG")
	awsCredentials := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	artifactDir := os.Getenv("ARTIFACT_DIR")
	privateKeyPath := os.Getenv("KUBE_SSH_KEY_PATH")
	// Use Windows 2019 server image with containers in us-east1 zone for CI testing.
	// TODO: Move to environment variable that can be fetched from the cloud provider
	imageID := "ami-0b8d82dea356226d3"
	instanceType := "m5a.large"
	sshKey := "libra"
	cloud, err := cloudprovider.CloudProviderFactory(kubeconfig, awsCredentials, "default", artifactDir,
		imageID, instanceType, sshKey, privateKeyPath)
	if err != nil {
		return fmt.Errorf("error instantiating cloud provider %v", err)
	}
	f.cloudProvider = cloud
	credentials, err := cloud.CreateWindowsVM()
	if err != nil {
		return fmt.Errorf("error creating Windows VM: %v", err)
	}
	f.Credentials = credentials
	return nil
}

// setupWinRMClient sets up the winrm client to be used while accessing Windows node
func (f *TestFramework) setupWinRMClient() error {
	host := f.Credentials.GetIPAddress()
	password := f.Credentials.GetPassword()

	// Connect to the bootstrapped host. Timeout is high as the Windows Server image is slow to download
	endpoint := winrm.NewEndpoint(host, winRMPort, true, true,
		nil, nil, nil, time.Minute*10)
	winrmClient, err := winrm.NewClient(endpoint, user, password)
	if err != nil {
		return fmt.Errorf("failed to set up winrm client with error: %v", err)
	}
	f.WinrmClient = winrmClient
	return nil
}

// configureOpenSSHServer configures the OpenSSH server using WinRM client installed on the Windows VM.
// The OpenSSH server is installed as part of WNI tool's CreateVM method.
func (f *TestFramework) configureOpenSSHServer() error {
	// This dependency is needed for the subsequent module installation we're doing. This version of NuGet
	// needed for OpenSSH server 0.0.1
	installDependentPackages := "Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Force"
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+installDependentPackages,
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to install dependent packages for OpenSSH server with error %v", err)
	}
	// Configure OpenSSH for all users.
	// TODO: Limit this to Administrator.
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"Install-Module -Force OpenSSHUtils -Scope AllUsers",
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to configure OpenSSHUtils for all users: %v", err)
	}
	// Setup ssh-agent Windows Service.
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"Set-Service -Name ssh-agent -StartupType ‘Automatic’",
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to set up ssh-agent Windows Service: %v", err)
	}
	// Setup sshd Windows service
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"Set-Service -Name sshd -StartupType ‘Automatic’",
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to set up sshd Windows Service: %v", err)
	}
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"Start-Service ssh-agent",
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("start ssh-agent failed: %v", err)
	}
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"Start-Service sshd",
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to start sshd: %v", err)
	}
	return nil
}

// createRemoteDir creates a directory on the Windows VM to which file can be transferred
func (f *TestFramework) createRemoteDir() error {
	// Create a directory on the Windows node where the file has to be transferred
	if _, err := f.WinrmClient.Run(remotePowerShellCmdPrefix+"mkdir"+" "+f.RemoteDir,
		os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("failed to created a temporary dir on the remote Windows node with %v", err)
	}
	return nil
}

// getSSHClient gets the ssh client associated with Windows VM created
func (f *TestFramework) getSSHClient() error {
	config := &ssh.ClientConfig{
		User:            "Administrator",
		Auth:            []ssh.AuthMethod{ssh.Password(f.Credentials.GetPassword())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", f.Credentials.GetIPAddress()+":22", config)
	if err != nil {
		return fmt.Errorf("failed to dial to ssh server: %s", err)
	}
	f.SSHClient = sshClient
	return nil
}

// TearDown tears down the set up done for test suite
func (f *TestFramework) TearDown() {
	if err := f.cloudProvider.DestroyWindowsVMs(); err != nil {
		log.Fatalf("failed tearing down the Windows VM with error: %v", err)
	}
}
