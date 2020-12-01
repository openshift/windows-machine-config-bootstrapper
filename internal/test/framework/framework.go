package framework

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v29/github"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	mapi "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	machine "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned/typed/machine/v1beta1"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
)

const (
	// RetryCount is the amount of times we will retry an api operation
	RetryCount = 20
	// RetryInterval is the interval of time until we retry after a failure
	RetryInterval = 5 * time.Second
	// WindowsLabel represents the node label that need to be applied to the Windows node created
	WindowsLabel = "node.openshift.io/os_id=Windows"
	// remoteLogPath is the directory where all the log files related to components that we need are generated on the
	// Windows VM
	remoteLogPath = "C:\\var\\log\\"
	// PrivateKeyPath contains the path to the private key which is used to access the VMs. This would have been mounted
	// as a secret by user
	PrivateKeyPath = "/etc/private-key/private-key.pem"
	// AWSCredentialsPath contains the path to the AWS credentials to interact with AWS cloud provider.
	AWSCredentialsPath = "/etc/aws-creds/credentials"
)

var (
	// artifactDir is the directory CI will read from once the test suite has finished execution
	artifactDir string
)

// TestFramework holds the info to run the test suite.
type TestFramework struct {
	// WinVms contains the Windows VMs that are created to execute the test suite
	WinVMs []TestWindowsVM
	// k8sclientset is the kubernetes clientset we will use to query the cluster's status
	K8sclientset *kubernetes.Clientset
	// OSConfigClient is the OpenShift config client, we will use to query the OpenShift api object status
	OSConfigClient *configclient.Clientset
	// OSOperatorClient is the OpenShift operator client, we will use to interact with OpenShift operator objects
	OSOperatorClient *operatorv1.OperatorV1Client
	// noTeardown is an indicator that the user supplied the VMs and they should not be destroyed
	noTeardown bool
	// ClusterVersion is the major.minor.patch version of the OpenShift cluster
	ClusterVersion string
	// latestRelease is the latest release of the wmcb
	latestRelease *github.RepositoryRelease
	// K8sVersion is the current version of Kuberenetes
	K8sVersion string
	// clusterAddress is the address of the OpenShift cluster e.g. "foo.fah.com".
	// This should not include "https://api-" or a port.
	ClusterAddress string
	// Signer is a signer created from the user's private key
	Signer ssh.Signer
	// client for interacting with machine objects
	machineClient *machine.MachineV1beta1Client
	// machineSet holds the MachineSet configuration used to destroy MachineSets
	machineSet *mapi.MachineSet
}

// Setup creates and initializes a variable amount of Windows VMs. If the array of credentials are passed then it will
// be used in lieu of creating new VMs. If skipVMsetup is true then it will result in the VM setup not being run. These
// two options are mainly used during test development.
func (f *TestFramework) Setup(vmCount int, skipVMSetup bool) error {
	// register the MachineSet to scheme so as to create machine sets
	mapi.AddToScheme(scheme.Scheme)

	// initialize the artifacts directory variable
	artifactDir = os.Getenv("ARTIFACT_DIR")
	var config *rest.Config
	var err error

	// use in-cluster pod permissions
	config, err = rest.InClusterConfig()

	if err != nil {
		return fmt.Errorf("unable to build config from kubeconfig: %s", err)
	}
	if err := f.getKubeClient(config); err != nil {
		return fmt.Errorf("unable to get kube client: %v", err)
	}
	if err := f.getOpenShiftConfigClient(config); err != nil {
		return fmt.Errorf("unable to get OpenShift client: %v", err)
	}
	if err := f.getOpenShiftOperatorClient(config); err != nil {
		return fmt.Errorf("unable to get OpenShift operator client: %v", err)
	}
	if err := f.getClusterAddress(); err != nil {
		return fmt.Errorf("unable to get cluster address: %v", err)
	}
	if err := f.getMachineAPIClient(config); err != nil {
		return fmt.Errorf("unable to get the Kube API client: %v", err)
	}
	if err := f.createSigner(); err != nil {
		return fmt.Errorf("unable to create ssh signer: %v", err)
	}

	if err := f.createUserDataSecret(); err != nil {
		return fmt.Errorf("unable to create user data secret: %v", err)
	}

	f.WinVMs, err = f.newWindowsMachineSet(vmCount, skipVMSetup)
	if err != nil {
		return fmt.Errorf("unable to create windows vm %v", err)
	}
	return nil
}

// getMachineAPIClient setups the kube api client to interact with Kubernetes API servers
func (f *TestFramework) getMachineAPIClient(config *restclient.Config) error {
	var err error
	f.machineClient, err = machine.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to instiantiate the machine api client: %v", err)
	}
	return nil
}

// getKubeClient setups the kubeclient that can be used across all the test suites.
func (f *TestFramework) getKubeClient(config *restclient.Config) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create k8s clientset: %v", err)
	}
	f.K8sclientset = clientset
	return nil
}

// getOpenShiftConfigClient gets the new OpenShift config v1 client
func (f *TestFramework) getOpenShiftConfigClient(config *restclient.Config) error {
	// Get openshift api config client.
	configClient, err := configclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create config clientset: %v", err)
	}
	f.OSConfigClient = configClient
	return nil
}

// getOpenShiftConfigClient gets a new OpenShift operator v1 client
func (f *TestFramework) getOpenShiftOperatorClient(config *restclient.Config) error {
	// Get openshift operator client
	operatorClient, err := operatorv1.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create operator clientset: %v", err)
	}
	f.OSOperatorClient = operatorClient
	return nil
}

// getClusterAddress returns the cluster address associated with the API server endpoint of the cluster the tests are
// are running against. For example: the kubernetes API server endpoint https://api-int.abc.devcluster.openshift.com:6443
func (f *TestFramework) getClusterAddress() error {
	// test pod runs on the host network and we are using API server internal endpoint to discover the kube API server
	host, err := f.OSConfigClient.ConfigV1().Infrastructures().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to get cluster infrastructure resource: %v", err)
	}
	// get API server internal url of format https://api-int.abc.devcluster.openshift.com:6443
	if host.Status.APIServerInternalURL == "" {
		return fmt.Errorf("could not get host url for the kubernetes api server")
	}

	clusterEndPoint, err := url.Parse(host.Status.APIServerInternalURL)
	if err != nil {
		return fmt.Errorf("unable to parse the API server endpoint: %v", err)
	}

	hostName := clusterEndPoint.Hostname()
	log.Printf("using hostname: %s", hostName)
	if !strings.HasPrefix(hostName, "api-int.") {
		return fmt.Errorf("API server has invalid format: expected hostname to start with `api-int.`")
	}

	f.ClusterAddress = hostName
	return nil
}

// GetClusterVersion gets the OpenShift cluster version in major.minor format. This is being done this way, and not with
// oc get clusterversion, as OpenShift CI doesn't have the actual version attached to its clusters, instead replacing it
// with 0.0.1 and information about the release creation date
func (f *TestFramework) GetClusterVersion() error {
	versionInfo, err := f.OSConfigClient.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("error while retrieving k8s version : %v", err)
	}
	//set k8s version in the TestFrameWork
	f.K8sVersion = versionInfo.GitVersion
	// trim everything after minor version, historically has been separated by either '+' or '-'
	f.K8sVersion = strings.Split(f.K8sVersion, "-")[0]
	f.K8sVersion = strings.Split(f.K8sVersion, "+")[0]
	// Convert kubernetes version to major.minor format. v1.17.0 -> 1.17
	k8sVersion := strings.TrimLeft(f.K8sVersion, "v")
	k8sSemver := strings.Split(k8sVersion, ".")
	k8sMinorVersion, err := strconv.Atoi(k8sSemver[1])
	if err != nil {
		return fmt.Errorf("could not conver k8s minor version %s to int", err)
	}

	// Map kubernetes version to OpenShift version
	openshiftVersion, err := k8sVersionToOpenShiftVersion(k8sMinorVersion)
	if err != nil {
		return fmt.Errorf("could not map kubernetes version to an OpenShift version: %s", err)
	}

	f.ClusterVersion = openshiftVersion
	return nil
}

// getGithubReleases gets all the github releases for a given owner and repo
func (f *TestFramework) getGithubReleases(owner string, repo string) ([]*github.RepositoryRelease, error) {
	// Initializing a client for using the Github API
	client := github.NewClient(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()
	releases, _, err := client.Repositories.ListReleases(ctx, owner, repo,
		&github.ListOptions{})
	return releases, err
}

// GetLatestWMCBRelease gets the latest github release for the WMCB repo. This release is specific to the cluster
// version
func (f *TestFramework) GetLatestWMCBRelease() error {
	releases, err := f.getGithubReleases("openshift", "windows-machine-config-bootstrapper")
	if err != nil {
		return err
	}

	for _, release := range releases {
		if strings.Contains(release.GetName(), f.ClusterVersion) {
			f.latestRelease = release
			return nil
		}
	}
	return fmt.Errorf("could not fetch latest release")
}

// GetLatestReleaseArtifactURL returns the URL of the releases artifact matching the given name
func (f *TestFramework) GetReleaseArtifactURL(artifactName string) (string, error) {
	for _, asset := range f.latestRelease.Assets {
		if asset.GetName() == artifactName {
			return asset.GetBrowserDownloadURL(), nil
		}
	}
	return "", fmt.Errorf("no artifact with name %s", artifactName)
}

// GetReleaseArtifactSHA returns the SHA256 of the release artifact specified, given the body of the release
func (f *TestFramework) GetReleaseArtifactSHA(artifactName string) (string, error) {
	// The release body looks like:
	// f819c2df76bc89fe0bd1311eea7dae2a11c40bc26b48b85fd4718e286b0a257e  wmcb.exe
	// 92c24c250ef81b565e2f59916f722d8fcb0a5ec1821899d590b781e3c883a7d3  wni
	// a476f9b7e8b223f5d2efb2066ae48be514d57b66e04038308d1787a770784084  hybrid-overlay.exe
	lines := strings.Split(f.latestRelease.GetBody(), "\r\n")
	for _, line := range lines {
		// Get the line that ends with the artifact name
		if strings.HasSuffix(line, " "+artifactName) {
			return strings.Split(line, " ")[0], nil
		}
	}
	return "", fmt.Errorf("no artifact with name %s", artifactName)
}

// GetNode returns a pointer to the node object associated with the internal IP provided
func (f *TestFramework) GetNode(internalIP string) (*v1.Node, error) {
	var matchedNode *v1.Node

	nodes, err := f.K8sclientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get list of nodes: %v", err)
	}
	if len(nodes.Items) == 0 {
		return nil, fmt.Errorf("no nodes found")
	}

	// Find the node that has the given IP
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == "InternalIP" && address.Address == internalIP {
				matchedNode = &node
				break
			}
		}
		if matchedNode != nil {
			break
		}
	}
	if matchedNode == nil {
		return nil, fmt.Errorf("could not find node with IP: %s", internalIP)
	}
	return matchedNode, nil
}

// WriteToArtifactDir will write contents to $ARTIFACT_DIR/subDirName/filename. If subDirName is empty, contents
// will be written to $ARTIFACT_DIR/filename
func (f *TestFramework) WriteToArtifactDir(contents []byte, subDirName, filename string) error {
	path := filepath.Join(artifactDir, subDirName, filename)
	dir, _ := filepath.Split(path)
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not create %s: %s", dir, err)
	}
	return ioutil.WriteFile(path, contents, os.ModePerm)
}

// GetNode uses internal IP and finds out the name associated with the node
func (f *TestFramework) GetNodeName(internalIP string) (string, error) {
	node, err := f.GetNode(internalIP)
	if err != nil {
		return "", fmt.Errorf("error while getting required kubernetes node object: %v", err)
	}
	return node.Name, nil
}

// RetrieveArtifacts should retrieve artifacts related the test run. Ideally this should retrieve all the logs related
// to the Windows VM. This shouldn't return an error but should print the failures as log collection is nice to have
// rather than a must have.
// TODO: Think about how we can retrieve stdout from ansible out within this function
func (f *TestFramework) RetrieveArtifacts() {
	for i, vm := range f.WinVMs {
		if vm == nil {
			continue
		}
		if vm.GetCredentials() == nil {
			log.Printf("no credentials provided for vm %d ", i)
			continue
		}

		instanceID := vm.GetCredentials().InstanceId()
		if len(instanceID) == 0 {
			log.Printf("no instance id provided for vm %d", i)
			continue
		}

		internalIP := vm.GetCredentials().IPAddress()
		if len(internalIP) == 0 {
			log.Printf("no internal ip address found for the vm with instance ID %s", instanceID)
			continue
		}

		nodeName, err := f.GetNodeName(internalIP)
		if err != nil {
			log.Printf("error while getting node name associated with the vm %s: %v", instanceID, err)
		}

		// We want a format like "nodes/ip-10-0-141-99.ec2.internal/logs/wsu/kubelet"
		localKubeletLogPath := filepath.Join(artifactDir, "nodes", nodeName, "logs")

		// Let's reinitialize the ssh client as hybrid overlay is known to cause ssh connections to be dropped
		// TODO: Reduce the usage of Reinitialize as much as possible, this is to ensure that when we move to operator
		// 		model, the reconnectivity should be handled automatically.
		if err := vm.Reinitialize(); err != nil {
			log.Printf("failed re-initializing ssh connectivity with on vm %s: %v", instanceID, err)
		}
		// TODO: Make this a map["'"artifact_that_we_want_to_pull"]="log_file.name" to only capture
		//  the logs we are interested in, to avoid capturing every directory in c:\\k\\log
		// Retrieve directories copies the directories from remote VM to the Artifacts directory
		if err := vm.RetrieveDirectories(remoteLogPath, localKubeletLogPath); err != nil {
			log.Printf("failed retrieving log directories on vm %s: %v", instanceID, err)
			continue
		}
	}
}

// waitUntilHybridOverlayReady returns once OVN is ready again, after applying the hybrid overlay patch.
func (f *TestFramework) waitUntilHybridOverlayReady() error {
	// This is being done after we wait for the master nodes, as we need to make sure the patch is being acted on,
	// and we are not checking the pods before they begin to restart with the hybrid overlay changes
	if err := f.waitUntilOVNPodsReady(); err != nil {
		return fmt.Errorf("error waiting for all pods in the OVN namespace to be ready: %s", err)
	}

	return nil
}

// waitUntilOVNPodsReady returns when either all pods in the openshift-ovn-kubernetes namespace are ready, or the
// timeout limit has been reached.
func (f *TestFramework) waitUntilOVNPodsReady() error {
	for i := 0; i < RetryCount; i++ {
		pods, err := f.K8sclientset.CoreV1().Pods("openshift-ovn-kubernetes").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("could not get pods: %s", err)
		}

		allReady := true
		for _, pod := range pods.Items {
			podReady := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == v1.PodReady {
					podReady = true
					break
				}
			}
			if !podReady {
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}
		time.Sleep(RetryInterval)
	}
	return fmt.Errorf("timed out waiting for pods in namespace \"openshift-ovn-kubernetes\" to be ready")

}

// TearDown destroys the resources created by the Setup function
func (f *TestFramework) TearDown() {
	if f.noTeardown || f.WinVMs == nil {
		return
	}

	if err := f.DestroyMachineSet(); err != nil {
		log.Printf("failed to delete MachineSets with error: %v", err)
	}
	return
}

// k8sVersionToOpenShiftVersion converts a Kubernetes minor version to an OpenShift version in format
// "major.minor". This function works under the assumption that for OpenShift 4, every OpenShift minor version increase
// corresponds with a kubernetes minor version increase
func k8sVersionToOpenShiftVersion(k8sMinorVersion int) (string, error) {
	openshiftMajorVersion := "4"
	baseKubernetesMinorVersion := 16
	baseOpenShiftMinorVersion := 3

	if k8sMinorVersion < baseKubernetesMinorVersion {
		return "", fmt.Errorf("kubernetes minor version %d is not supported", k8sMinorVersion)
	}

	// find how many minor versions past OpenShift 4.3 we are
	versionIncrements := k8sMinorVersion - baseKubernetesMinorVersion
	openShiftMinorVersion := strconv.Itoa(versionIncrements + baseOpenShiftMinorVersion)
	return openshiftMajorVersion + "." + openShiftMinorVersion, nil
}

// createSigner creates a signer using the private key from the PrivateKeyPath
func (f *TestFramework) createSigner() error {
	privateKeyBytes, err := ioutil.ReadFile(PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to find private key from path: %v, err: %v", PrivateKeyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return fmt.Errorf("unable to parse private key: %v, err: %v", err, PrivateKeyPath)
	}
	f.Signer = signer
	return nil
}

// createUserDataSecret creates a secret 'windows-user-data' in 'openshift-machine-api'
// namespace. This secret will be used to inject cloud provider user data for creating
// windows machines
func (f *TestFramework) createUserDataSecret() error {
	if f.Signer == nil {
		return fmt.Errorf("failed to retrieve signer for private key: %v", PrivateKeyPath)
	}

	pubKeyBytes := ssh.MarshalAuthorizedKey(f.Signer.PublicKey())
	if pubKeyBytes == nil {
		return fmt.Errorf("failed to retrieve public key using signer for private key: %v", PrivateKeyPath)
	}

	// sshd service is started to create the default sshd_config file. This file is modified
	// for enabling publicKey auth and the service is restarted for the changes to take effect.
	userDataSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "windows-user-data",
			Namespace: "openshift-machine-api",
		},
		Data: map[string][]byte{
			"userData": []byte(`<powershell>
			Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
			$firewallRuleName = "ContainerLogsPort"
			$containerLogsPort = "10250"
			New-NetFirewallRule -DisplayName $firewallRuleName -Direction Inbound -Action Allow -Protocol TCP -LocalPort $containerLogsPort -EdgeTraversalPolicy Allow
			Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Force
			Install-Module -Force OpenSSHUtils
			Set-Service -Name ssh-agent -StartupType ‘Automatic’
			Set-Service -Name sshd -StartupType ‘Automatic’
			Start-Service ssh-agent
			Start-Service sshd
			$pubKeyConf = (Get-Content -path C:\ProgramData\ssh\sshd_config) -replace '#PubkeyAuthentication yes','PubkeyAuthentication yes'
			$pubKeyConf | Set-Content -Path C:\ProgramData\ssh\sshd_config
 			$passwordConf = (Get-Content -path C:\ProgramData\ssh\sshd_config) -replace '#PasswordAuthentication yes','PasswordAuthentication yes'
			$passwordConf | Set-Content -Path C:\ProgramData\ssh\sshd_config
			$authFileConf = (Get-Content -path C:\ProgramData\ssh\sshd_config) -replace 'AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys','#AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys'
			$authFileConf | Set-Content -Path C:\ProgramData\ssh\sshd_config
			$pubKeyLocationConf = (Get-Content -path C:\ProgramData\ssh\sshd_config) -replace 'Match Group administrators','#Match Group administrators'
			$pubKeyLocationConf | Set-Content -Path C:\ProgramData\ssh\sshd_config
			Restart-Service sshd
			New-item -Path $env:USERPROFILE -Name .ssh -ItemType Directory -force
			echo "` + string(pubKeyBytes[:]) + `"| Out-File $env:USERPROFILE\.ssh\authorized_keys -Encoding ascii
			</powershell>
			<persist>true</persist>`),
		},
	}

	// check if the userDataSecret already exists
	_, err := f.K8sclientset.CoreV1().Secrets(userDataSecret.Namespace).Get(context.TODO(), userDataSecret.Name, metav1.GetOptions{})
	if err != nil {
		if k8sapierrors.IsNotFound(err) {
			log.Print("Creating a new Secret", "Secret.Namespace", userDataSecret.Namespace, "Secret.Name", userDataSecret.Name)
			_, err = f.K8sclientset.CoreV1().Secrets(userDataSecret.Namespace).Create(context.TODO(), userDataSecret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("error creating windows user data secret: %v", err)
			}
			return nil
		}
		return fmt.Errorf("error creating windows user data secret: %v", err)
	}
	return nil
}
