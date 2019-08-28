package bootstrapper

/*
	Bootstrapper is the entity responsible for bootstrapping a Windows node. The current scope of this component is to
 	perform an one shot configuration of the Windows node to ensure that it can be become a worker node. Following
    are the jobs that the bootstrapper does:
	- Parse the worker ignition file to get the bootstrap kubeconfig
	- Ensures that the kubelet gets the correct kubelet config
	- Run the kubelet as a windows service
	This will be remotely invoked from a Ansible script or can be run locally
*/

// winNodeBoostrapper is responsible for bootstrapping and ensuring kubelet runs as a windows service
type winNodeBootstrapper struct {
	// kubeconfigPath is the file path of the node bootstrap kubeconfig
	kubeConfigPath string
	// kubeletConf is the file path of the kubelet configuration
	kubeletConfPath string
	// TODO: Add some more fields as deemed necessary. Some examples are monitoring windows service
}

// NewWinNodeBootstrapper takes ignitionFile and kubeletPath as inputs and generates the winNodeBootstrapper object
func NewWinNodeBootstrapper(ignitionFile, kubeletPath string) *winNodeBootstrapper {
	// TODO: Parse the ignition file here and get the kubeconfig and kubeletconf
	return &winNodeBootstrapper{
		kubeConfigPath:  "",
		kubeletConfPath: "",
	}
}

// parseIgnitionFile parses the ignition file and returns the contents of kubeconfig and kubelet config
func parseIgnitionFile() (string, string) {
	// Parses the ignition file
	return "", ""
}

// Initializes the kubelet after copying the kubelet to the desired location
func (wcb *winNodeBootstrapper) initializeKubelet() (bool, error) {
	return false, nil
}

// Start kubelet as a Windows service to ensure that kubelet is automatically started when it fails
func (wcb *winNodeBootstrapper) startKubeletService() bool {
	return false
}

// TODO:  Add other functions as deemed necessary
