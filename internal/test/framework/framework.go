package framework

import (
	"context"
	"fmt"
	"github.com/google/go-github/v29/github"
	v1 "k8s.io/api/core/v1"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// RetryCount is the amount of times we will retry an api operation
	RetryCount = 20
	// RetryInterval is the interval of time until we retry after a failure
	RetryInterval = 5 * time.Second
	// WindowsLabel represents the node label that need to be applied to the Windows node created
	WindowsLabel = "node.openshift.io/os_id=Windows"
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
	// clusterAddress is the address of the OpenShift cluster e.g. "foo.fah.com".
	// This should not include "https://api-" or a port
	ClusterAddress string
)

// TestFramework holds the info to run the test suite.
type TestFramework struct {
	// WinVms contains the Windows VMs that are created to execute the test suite
	WinVMs []WindowsVM
	// k8sclientset is the kubernetes clientset we will use to query the cluster's status
	K8sclientset *kubernetes.Clientset
	// OSConfigClient is the OpenShift config client, we will use to query the OpenShift api object status
	OSConfigClient *configclient.Clientset
	// noTeardown is an indicator that the user supplied the VMs and they should not be destroyed
	noTeardown bool
	// ClusterVersion is the major.minor.patch version of the OpenShift cluster
	ClusterVersion string
	// LatestRelease is the latest release of the wmcb
	LatestRelease *github.RepositoryRelease
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
	for i := 0; i < len(splitValue); i += 3 {
		cred := types.NewCredentials(splitValue[i], splitValue[i+1], splitValue[i+2])
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
	ClusterAddress = os.Getenv("CLUSTER_ADDR")
	if ClusterAddress == "" {
		return fmt.Errorf("CLUSTER_ADDR environment variable not set")
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
	// Use Windows 2019 server image with containers in us-east1 zone for CI testing.
	// TODO: Move to environment variable that can be fetched from the cloud provider
	// The CI-operator uses AWS region `us-east-1` which has the corresponding image ID: ami-0105f663dc99752af for
	// Microsoft Windows Server 2019 Base with Containers.
	imageID := "ami-0105f663dc99752af"
	// Using an AMD instance type, as the Windows hybrid overlay currently does not work on on machines using
	// the Intel 82599 network driver
	instanceType := "m5a.large"
	if err := initCIvars(); err != nil {
		return fmt.Errorf("unable to initialize CI variables: %v", err)
	}
	f.WinVMs = make([]WindowsVM, vmCount)
	// TODO: make them run in parallel: https://issues.redhat.com/browse/WINC-178
	for i := 0; i < vmCount; i++ {
		var err error
		var creds *types.Credentials
		if credentials != nil {
			creds = credentials[i]
		}
		f.WinVMs[i], err = newWindowsVM(imageID, instanceType, creds, skipVMsetup)
		if err != nil {
			return fmt.Errorf("unable to instantiate Windows VM: %v", err)
		}
	}
	if err := f.getKubeClient(); err != nil {
		return fmt.Errorf("unable to get kube client: %v", err)
	}
	if err := f.getOpenShiftConfigClient(); err != nil {
		return fmt.Errorf("unable to get OpenShift client: %v", err)
	}
	if err := f.getClusterVersion(); err != nil {
		return fmt.Errorf("unable to get OpenShift cluster version: %v", err)
	}
	if err := f.getLatestGithubRelease(); err != nil {
		return fmt.Errorf("unable to get latest github release: %v", err)
	}
	return nil
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

// getClusterVersion gets the OpenShift cluster version in major.minor format. This is being done this way, and not with
// oc get clusterversion, as OpenShift CI doesn't have the actual version attached to its clusters, instead replacing it
// with 0.0.1 and information about the release creation date
func (f *TestFramework) getClusterVersion() error {
	versionInfo, err := f.OSConfigClient.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	// Convert kubernetes version to major.minor format. v1.17.0 -> 1.17
	k8sVersion := strings.TrimLeft(versionInfo.GitVersion, "v")
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

// getOpenShiftConfigClient gets the new OpenShift config v1 client
func (f *TestFramework) getOpenShiftConfigClient() error {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("could not build config from flags: %v", err)
	}
	// Get openshift api config client.
	configClient, err := configclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("could not create config clientset: %v", err)
	}
	f.OSConfigClient = configClient
	return nil
}

// getLatestGithubRelease gets the latest github release for the wmcb repo. This release is specific to the cluster version
func (f *TestFramework) getLatestGithubRelease() error {
	client := github.NewClient(nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()
	releases, _, err := client.Repositories.ListReleases(ctx, "openshift", "windows-machine-config-operator",
		&github.ListOptions{})
	if err != nil {
		return err
	}

	for _, release := range releases {
		if strings.Contains(release.GetName(), f.ClusterVersion) {
			f.LatestRelease = release
			return nil
		}
	}
	return fmt.Errorf("could not fetch latest release")
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
