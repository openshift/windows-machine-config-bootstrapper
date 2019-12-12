package framework

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/masterzen/winrm"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"golang.org/x/crypto/ssh"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

var (
	kubeconfig     string
	awsCredentials string
	artifactDir    string
	privateKeyPath string
)

type windowsVM struct {
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
}

// TestFramework holds the info to run the test suite.
// This is not clean
type TestFramework struct {
	RemoteDir string
	WinVMs    []windowsVM
	// k8sclientset is the kubernetes clientset we will use to query the cluster's status
	K8sclientset *kubernetes.Clientset
}

func initCIvars() error {
	kubeconfig = os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return fmt.Errorf("KUBECONFIG environment variable not set")
	}
	awsCredentials = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if awsCredentials == "" {
		return fmt.Errorf("AWS_SHARED_CREDENTIALS_FILE environment variable not set")
	}
	artifactDir = os.Getenv("ARTIFACT_DIR")
	if awsCredentials == "" {
		return fmt.Errorf("ARTIFACT_DIR environment variable not set")
	}
	privateKeyPath = os.Getenv("KUBE_SSH_KEY_PATH")
	if privateKeyPath == "" {
		return fmt.Errorf("KUBE_SSH_KEY_PATH environment variable not set")
	}
	return nil
}

// Setup sets up the Windows node so that it can join the existing OpenShift cluster
// TODO: move this to return error and do assertions there
func (f *TestFramework) Setup(nrVMs int) {

	if err := initCIvars(); err != nil {
		log.Fatalf("failed to initialize CI variables with error: %v", err)
	}

	f.WinVMs = make([]windowsVM, nrVMs)
	// TODO: make them run in parallel
	for _, vm := range f.WinVMs{
		if err := vm.setup(); err != nil {
			log.Fatalf("failed to create Windows VM with error: %v", err)
		}
		vm.RemoteDir = f.RemoteDir
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

// TearDown tears down the set up done for test suite
func (f *TestFramework) TearDown() {
	for _, vm := range f.WinVMs {
		if err := vm.cloudProvider.DestroyWindowsVMs(); err != nil {
			log.Fatalf("failed tearing down the Windows VM with error: %v", err)
		}
	}
}

func (w *windowsVM) setup() error {
	if err := w.create(); err != nil {
		return fmt.Errorf("failed to create Windows VM: %v", err)
	}
	// TODO: Add some options to skip certain parts of the test
	if err := w.setupWinRMClient(); err != nil {
		return fmt.Errorf("failed to setup winRM client for the Windows VM: %v", err)
	}
	// Wait for some time before starting configuring of ssh server. This is to let sshd service be available
	// in the list of services
	// TODO: Parse the output of the `Get-Service sshd, ssh-agent` on the Windows node to check if the windows nodes
	// has those services present
	time.Sleep(time.Minute)
	if err := w.configureOpenSSHServer(); err != nil {
		return fmt.Errorf("failed to configure OpenSSHServer on the Windows VM: %v", err)
	}
	if err := w.createRemoteDir(); err != nil {
		return fmt.Errorf("failed to create remote dir with error: %v", err)
	}
	if err := w.getSSHClient(); err != nil {
		return fmt.Errorf("failed to get ssh client for the Windows VM created: %v", err)
	}

	return nil
}

// createWindowsVM spins up the Windows VM in the given cloud provider and gives us the credentials to access the
// windows VM created
func (w *windowsVM) create() error {
	// NOTE: if we ever need to create VMs of different types, then imageID and instanceType should move to the
	// windowsVM struct
	// Use Windows 2019 server image with containers in us-east1 zone for CI testing.
	// TODO: Move to environment variable that can be fetched from the cloud provider
	// The CI-operator uses AWS region `us-east-1` which has the corresponding image ID: ami-0b8d82dea356226d3 for
	// Microsoft Windows Server 2019 Base with Containers.
	imageID := "ami-0b8d82dea356226d3"
	// Using an AMD instance type, as the Windows hybrid overlay currently does not work on on machines using
	// the Intel 82599 network driver
	instanceType := "m5a.large"
	sshKey := "libra"
	cloud, err := cloudprovider.CloudProviderFactory(kubeconfig, awsCredentials, "default", artifactDir,
		imageID, instanceType, sshKey, privateKeyPath)
	if err != nil {
		return fmt.Errorf("error instantiating cloud provider %v", err)
	}
	w.cloudProvider = cloud
	credentials, err := cloud.CreateWindowsVM()
	if err != nil {
		return fmt.Errorf("error creating Windows VM: %v", err)
	}
	w.Credentials = credentials
	return nil
}

// setupWinRMClient sets up the winrm client to be used while accessing Windows node
func (w *windowsVM) setupWinRMClient() error {
	host := w.Credentials.GetIPAddress()
	password := w.Credentials.GetPassword()

	// Connect to the bootstrapped host. Timeout is high as the Windows Server image is slow to download
	endpoint := winrm.NewEndpoint(host, winRMPort, true, true,
		nil, nil, nil, time.Minute*10)
	winrmClient, err := winrm.NewClient(endpoint, user, password)
	if err != nil {
		return fmt.Errorf("failed to set up winrm client with error: %v", err)
	}
	w.WinrmClient = winrmClient
	return nil
}

// configureOpenSSHServer configures the OpenSSH server using WinRM client installed on the Windows VM.
// The OpenSSH server is installed as part of WNI tool's CreateVM method.
func (w *windowsVM) configureOpenSSHServer() error {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	// This dependency is needed for the subsequent module installation we're doing. This version of NuGet
	// needed for OpenSSH server 0.0.1
	installDependentPackages := "Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Force"
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+installDependentPackages,
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to install dependent packages for OpenSSH server with error %v", err)
	}
	// Configure OpenSSH for all users.
	// TODO: Limit this to Administrator.
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"Install-Module -Force OpenSSHUtils -Scope AllUsers",
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to configure OpenSSHUtils for all users: %v", err)
	}
	// Setup ssh-agent Windows Service.
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"Set-Service -Name ssh-agent -StartupType ‘Automatic’",
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to set up ssh-agent Windows Service: %v", err)
	}
	// Setup sshd Windows service
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"Set-Service -Name sshd -StartupType ‘Automatic’",
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to set up sshd Windows Service: %v", err)
	}
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"Start-Service ssh-agent",
		stdout, stderr); err != nil {
		return fmt.Errorf("start ssh-agent failed: %v", err)
	}
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"Start-Service sshd",
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to start sshd: %v", err)
	}
	return nil
}

// createRemoteDir creates a directory on the Windows VM to which file can be transferred
func (w *windowsVM) createRemoteDir() error {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	// Create a directory on the Windows node where the file has to be transferred
	if _, err := w.WinrmClient.Run(remotePowerShellCmdPrefix+"mkdir"+" "+w.RemoteDir,
		stdout, stderr); err != nil {
		return fmt.Errorf("failed to created a temporary dir on the remote Windows node with %v", err)
	}
	return nil
}

// getSSHClient gets the ssh client associated with Windows VM created
func (w *windowsVM) getSSHClient() error {
	config := &ssh.ClientConfig{
		User:            "Administrator",
		Auth:            []ssh.AuthMethod{ssh.Password(w.Credentials.GetPassword())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", w.Credentials.GetIPAddress()+":22", config)
	if err != nil {
		return fmt.Errorf("failed to dial to ssh server: %s", err)
	}
	w.SSHClient = sshClient
	return nil
}
