package framework

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v29/github"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// RetryCount is the amount of times we will retry an api operation
	RetryCount = 20
	// RetryInterval is the interval of time until we retry after a failure
	RetryInterval = 5 * time.Second
	// WindowsLabel represents the node label that need to be applied to the Windows node created
	WindowsLabel = "node.openshift.io/os_id=Windows"

	// awsUsername is the default windows username on AWS
	awsUsername = "Administrator"
	// remoteLogPath is the directory where all the log files related to components that we need are generated on the
	// Windows VM
	remoteLogPath = "C:\\var\\log\\"
)

var (
	// kubeconfig is the location of the kubeconfig for the cluster the test suite will run on
	kubeconfig string
	// awsCredentials is the Credentials file for the aws account the cluster will be created with
	awsCredentials string
	// artifactDir is the directory CI will read from once the test suite has finished execution
	artifactDir string
	// privateKeyPath is the path to the key that will be used to retrieve the password of each Windows VM
	privateKeyPath string
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
	// LatestCniPluginsVersion is the latest 0.8.x version of CNI Plugins
	LatestCniPluginsVersion string
	// K8sVersion is the current version of Kuberenetes
	K8sVersion string
	// clusterAddress is the address of the OpenShift cluster e.g. "foo.fah.com".
	// This should not include "https://api-" or a port.
	ClusterAddress string
}

// Creds is used for parsing the vmCreds command line argument
type Creds []*types.Credentials

// Set populates the list of credentials from the vmCreds command line argument
func (c *Creds) Set(value string) error {
	if value == "" {
		return nil
	}

	splitValue := strings.Split(value, ",")
	// Credentials consists of three elements, so this has to be
	if len(splitValue)%3 != 0 {
		return fmt.Errorf("incomplete VM credentials provided")
	}

	// TODO: Add input validation if we want to use this in production
	// TODO: Change username based on cloud provider if this is to be used for clouds other than AWS
	for i := 0; i < len(splitValue); i += 3 {
		cred := types.NewCredentials(splitValue[i], splitValue[i+1], splitValue[i+2], awsUsername)
		*c = append(*c, cred)
	}
	return nil
}

// String returns the string representation of Creds. This is required for Creds to be used with flags.
func (c *Creds) String() string {
	return fmt.Sprintf("%v", *c)
}

// initCIvars gathers the values of the environment variables which configure the test suite
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

// Setup creates and initializes a variable amount of Windows VMs. If the array of credentials are passed then it will
// be used in lieu of creating new VMs. If skipVMsetup is true then it will result in the VM setup not being run. These
// two options are mainly used during test development.
func (f *TestFramework) Setup(vmCount int, credentials []*types.Credentials, skipVMsetup bool) error {
	if credentials != nil {
		if len(credentials) != vmCount {
			return fmt.Errorf("vmCount %d does not match length %d of credentials", vmCount, len(credentials))
		}
		f.noTeardown = true
	}

	if err := initCIvars(); err != nil {
		return fmt.Errorf("unable to initialize CI variables: %v", err)
	}

	f.WinVMs = make([]TestWindowsVM, vmCount)
	// Using an AMD instance type, as the Windows hybrid overlay currently does not work on on machines using
	// the Intel 82599 network driver
	instanceType := "m5a.large"
	// TODO: make them run in parallel: https://issues.redhat.com/browse/WINC-178
	for i := 0; i < vmCount; i++ {
		var err error
		var creds *types.Credentials
		if credentials != nil {
			creds = credentials[i]
		}
		// Pass an empty imageID so that WNI will use the latest Windows image
		f.WinVMs[i], err = newWindowsVM("", instanceType, creds, skipVMsetup)
		if err != nil {
			return fmt.Errorf("unable to instantiate Windows VM: %v", err)
		}
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
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
	if err := f.getClusterAddress(config); err != nil {
		return fmt.Errorf("unable to get cluster address: %v", err)
	}
	if err := f.getLatestCniPluginsVersion(); err != nil {
		return fmt.Errorf("unable to get latest 0.8.x version of CNI Plugins: %v", err)
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
// are running against. For example: the kubernetes API server endpoint https://api.abc.devcluster.openshift.com:6443
// gets converted to abc.devcluster.openshift.com
func (f *TestFramework) getClusterAddress(config *restclient.Config) error {
	if config.Host == "" {
		return fmt.Errorf("API server has empty host name")
	}

	clusterEndPoint, err := url.Parse(config.Host)
	if err != nil {
		return fmt.Errorf("unable to parse the API server endpoint: %v", err)
	}

	hostName := clusterEndPoint.Hostname()
	if !strings.HasPrefix(hostName, "api.") {
		return fmt.Errorf("API server has invalid format: expected hostname to start with `api.`")
	}

	// Replace `api.` with empty string for the first occurrence.
	f.ClusterAddress = strings.Replace(hostName, "api.", "", 1)
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

// getLatestCniPluginsVersion returns the latest 0.8.x version of CNI plugins in 'v<major>.<minor>.<patch>' format
func (f *TestFramework) getLatestCniPluginsVersion() error {
	releases, err := f.getGithubReleases("containernetworking", "plugins")
	if err != nil {
		return err
	}
	// Iterating over releases to fetch versions from tag names
	var cniPluginsVersions = []string{}
	for _, release := range releases {
		cniPluginsVersions = append(cniPluginsVersions, release.GetTagName())
	}
	// Sorting versions in reverse order so as to get latest version first
	sort.Sort(sort.Reverse(sort.StringSlice(cniPluginsVersions)))
	// Iterating over versions to find first 0.8.x version which is not a release candidate
	for _, cniPluginsVersion := range cniPluginsVersions {
		if strings.HasPrefix(cniPluginsVersion, "v0.8.") && !strings.Contains(cniPluginsVersion, "rc") {
			f.LatestCniPluginsVersion = cniPluginsVersion
			return nil
		}
	}
	return fmt.Errorf("could not fetch latest 0.8.x version of CNI Plugins")
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

// GetNode returns a pointer to the node object associated with the external IP provided
func (f *TestFramework) GetNode(externalIP string) (*v1.Node, error) {
	var matchedNode *v1.Node

	nodes, err := f.K8sclientset.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get list of nodes")
	}
	if len(nodes.Items) == 0 {
		return nil, fmt.Errorf("no nodes found")
	}

	// Find the node that has the given IP
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == "ExternalIP" && address.Address == externalIP {
				matchedNode = &node
				break
			}
		}
		if matchedNode != nil {
			break
		}
	}
	if matchedNode == nil {
		return nil, fmt.Errorf("could not find node with IP: %s", externalIP)
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

// GetNode uses external IP and finds out the name associated with the node
func (f *TestFramework) GetNodeName(externalIP string) (string, error) {
	node, err := f.GetNode(externalIP)
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

		instanceID := vm.GetCredentials().GetInstanceId()
		if len(instanceID) == 0 {
			log.Printf("no instance id provided for vm %d", i)
			continue
		}

		externalIP := vm.GetCredentials().GetIPAddress()
		if len(externalIP) == 0 {
			log.Printf("no external ip address found for the vm with instance ID %s", instanceID)
			continue
		}

		nodeName, err := f.GetNodeName(externalIP)
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
		pods, err := f.K8sclientset.CoreV1().Pods("openshift-ovn-kubernetes").List(metav1.ListOptions{})
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

	for _, vm := range f.WinVMs {
		if vm == nil {
			continue
		}
		if err := vm.Destroy(); err != nil {
			log.Printf("failed tearing down the Windows VM %v with error: %v", vm, err)
		} else {
			// WNI will delete all the VMs in windows-node-installer.json so we need this to succeed only once
			return
		}
	}
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
