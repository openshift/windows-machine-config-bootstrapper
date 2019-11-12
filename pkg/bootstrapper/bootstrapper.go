package bootstrapper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	ignitionv2 "github.com/coreos/ignition/config/v2_2"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/sys/windows/svc"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	kubeletConfig "k8s.io/kubelet/config/v1beta1"

	"golang.org/x/sys/windows/svc/mgr"
)

/*
	Bootstrapper is the entity responsible for bootstrapping a Windows node. The current scope of this component is to
 	perform an one shot configuration of the Windows node to ensure that it can be become a worker node. Following
    are the jobs that the bootstrapper does:
	- Parse the worker ignition file to get the bootstrap kubeconfig
	- Ensures that the kubelet gets the correct kubelet config
	- Run the kubelet as a windows service
	This will be remotely invoked from a Ansible script or can be run locally
*/

const (
	// KubeletServiceName is the name will we run the kubelet Windows service under. It is required to be named "kubelet":
	// https://github.com/kubernetes/kubernetes/blob/v1.16.0/cmd/kubelet/app/init_windows.go#L26
	KubeletServiceName = "kubelet"
	// kubeletSystemdName is the name of the systemd service that the kubelet runs under,
	// this is used to parse the kubelet args
	kubeletSystemdName = "kubelet.service"
	// kubeletPauseContainerImage is the location of the image we will use for the kubelet pause container
	kubeletPauseContainerImage = "mcr.microsoft.com/k8s/core/pause:1.2.0"
	// serviceWaitTime is an arbitrary amount of time to wait for the Windows service API to complete requests
	serviceWaitTime = time.Second * 10
	// certDirectory is where the kubelet will look for certificates
	certDirectory = "c:/var/lib/kubelet/pki/"
	// cloudConfigOption is kubelet CLI option for cloud configuration
	cloudConfigOption = "cloud-config"
	// windowsTaints defines the taints that need to be applied on the Windows nodes.
	/*
			TODO: As of now, this is limited to os=Windows, so every Windows pod in
			OpenShift cluster should have a toleration for this.
			Example toleration in the pod spec:
			tolerations:
			  - key: "os"
		      	operator: "Equal"
		      	value: "Windows"
		      	effect: "NoSchedule"
	*/
	windowsTaints = "os=Windows:NoSchedule"
	// workerLabel contains the label that needs to be applied to the worker nodes in the cluster
	workerLabel = "node-role.kubernetes.io/worker"
)

// These regex are global, so that we only need to compile them once
var (
	// cloudProviderRegex searches for the cloud provider option given to the kubelet
	cloudProviderRegex = regexp.MustCompile(`--cloud-provider=(\w*)`)

	// cloudConfigRegex searches for the cloud config option given to the kubelet. We are assuming that the file has a
	// conf extension.
	cloudConfigRegex = regexp.MustCompile(`--` + cloudConfigOption + `=(\/.*conf)`)

	// verbosityRegex searches for the verbosity option given to the kubelet
	verbosityRegex = regexp.MustCompile(`--v=(\w*)`)

	// nodeLabelRegex searches for all the node labels that needs to be applied to kubelet. Usually labels are
	// comma separated values.
	// Example: --node-labels=node-role.kubernetes.io/worker,node.openshift.io/os_id=${ID}
	nodeLabelRegex = regexp.MustCompile(`--node-labels=(.*)`)
)

// winNodeBootstrapper is responsible for bootstrapping and ensuring kubelet runs as a Windows service
type winNodeBootstrapper struct {
	// kubeconfigPath is the file path of the node bootstrap kubeconfig
	kubeconfigPath string
	// kubeletConfPath is the file path of the kubelet configuration
	kubeletConfPath string
	// ignitionFilePath is the path to the ignition file which is used to set up worker nodes
	// https://github.com/coreos/ignition/blob/spec2x/doc/getting-started.md
	ignitionFilePath string
	//initialKubeletPath is the path to the kubelet that we'll be using to bootstrap this node
	initialKubeletPath string
	// TODO: When more services are added consider decomposing the services to a separate Service struct with common functions
	// kubeletSVC is a pointer to the kubelet Windows service object
	kubeletSVC *mgr.Service
	// svcMgr is used to interact with the Windows service API
	svcMgr *mgr.Mgr
	// installDir is the directory the the kubelet service will be installed
	installDir string
	// kubeletArgs is a map of the variable arguments that will be passed to the kubelet
	kubeletArgs map[string]string
}

// NewWinNodeBootstrapper takes the path to install the kubelet to, and paths to the ignition file and kubelet as inputs,
// and generates the winNodeBootstrapper object
func NewWinNodeBootstrapper(k8sInstallDir, ignitionFile, kubeletPath string) (*winNodeBootstrapper, error) {
	svcMgr, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("could not connect to Windows SCM: %s", err)
	}
	bootstrapper := winNodeBootstrapper{
		kubeconfigPath:     filepath.Join(k8sInstallDir, "kubeconfig"),
		kubeletConfPath:    filepath.Join(k8sInstallDir, "kubelet.conf"),
		ignitionFilePath:   ignitionFile,
		installDir:         k8sInstallDir,
		initialKubeletPath: kubeletPath,
		svcMgr:             svcMgr,
		kubeletArgs:        make(map[string]string),
	}
	// If there is already a kubelet service running, find it
	if ksvc, err := svcMgr.OpenService(KubeletServiceName); err == nil {
		bootstrapper.kubeletSVC = ksvc
	}
	return &bootstrapper, nil
}

// translationFunc is a function that takes a byte array and changes it for use on windows
type translationFunc func(*winNodeBootstrapper, []byte) ([]byte, error)

// fileTranslation indicates where a file should be written and what should be done to the contents
type fileTranslation struct {
	dest string
	translationFunc
}

// prepKubeletConfForWindows adds all Windows specific configuration options we need to the kubelet configuration
// specifically, we change the cgroup driver, CA path, resolv.conf path, and enforce node allocatable.
func prepKubeletConfForWindows(wmcb *winNodeBootstrapper, initialConfig []byte) ([]byte, error) {
	var out []byte
	// Here we parse the initial configuration, which was yaml, into a KubeletConfiguration struct
	b := bufio.NewReader(bytes.NewReader(initialConfig))
	r := yaml.NewYAMLReader(b)
	doc, err := r.Read()
	if err != nil {
		return out, err
	}
	scheme := runtime.NewScheme()
	err = kubeletConfig.AddToScheme(scheme)
	if err != nil {
		return out, err
	}
	d := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	config := kubeletConfig.KubeletConfiguration{}
	_, _, err = d.Decode(doc, nil, &config)
	if err != nil {
		return out, fmt.Errorf("could not decode yaml: %s\n%s", initialConfig, err)
	}

	// Here we edit the config's fields so we can run the kubelet on Windows
	config.CgroupDriver = "cgroupfs"
	config.ResolverConfig = ""
	cgroupsPerQOS := false
	config.CgroupsPerQOS = &cgroupsPerQOS
	config.Authentication.X509.ClientCAFile = filepath.Join(wmcb.installDir, "kubelet-ca.crt")

	// We need to set EnforceNodeAllocatable with an empty slice, "enforceNodeAllocatable:[]"
	// the json tags have the field set as `omitempty`, and the field defaults to enforceNodeAllocatable:["pods"]
	// so we need empty the slice after marshalling
	// Putting a placeholder value here to be removed later in the function
	config.EnforceNodeAllocatable = []string{"THIS_MUST_BE_EMPTY"}

	// Turn the config into a json marshalled []byte
	out, err = json.Marshal(config)
	if err != nil {
		return out, err
	}

	// replacing EnforceNodeAllocatable with an empty slice,
	outString := strings.Replace(string(out), "\"THIS_MUST_BE_EMPTY\"", "", -1)
	return []byte(outString), err
}

// translateFile decodes an ignition "Storage.Files.Contents.Source" field and transforms it via the function provided.
// if fileTranslateFn is nil, ignitionSource will be decoded, but not transformed
func (wmcb *winNodeBootstrapper) translateFile(ignitionSource string, fileTranslateFn translationFunc) ([]byte, error) {
	contents, err := dataurl.DecodeString(ignitionSource)
	if err != nil {
		return []byte{}, err
	}
	newContents := contents.Data
	if fileTranslateFn != nil {
		newContents, err = fileTranslateFn(wmcb, contents.Data)
		if err != nil {
			return []byte{}, err
		}
	}
	return newContents, err
}

// parseIgnitionFileContents parses the ignition file contents and writes the contents of the described files to the k8s
// installation directory
func (wmcb *winNodeBootstrapper) parseIgnitionFileContents(ignitionFileContents []byte,
	filesToTranslate map[string]fileTranslation) error {
	// Parse configuration file
	configuration, _, err := ignitionv2.Parse(ignitionFileContents)
	if err != nil {
		return err
	}

	// Find the kubelet systemd service specified in the ignition file and grab the variable arguments
	for _, unit := range configuration.Systemd.Units {
		if unit.Name != kubeletSystemdName {
			continue
		}

		results := cloudProviderRegex.FindStringSubmatch(unit.Contents)
		if len(results) == 2 {
			wmcb.kubeletArgs["cloud-provider"] = results[1]
		}

		// Check for the presence of "--cloud-config" option and if it is present append the value to
		// filesToTranslate. This option is only present for Azure and hence we cannot assume it as a file that
		// requires translation across clouds.
		results = cloudConfigRegex.FindStringSubmatch(unit.Contents)
		if len(results) == 2 {
			cloudConfFilename := filepath.Base(results[1])

			// Check if we were able to get a valid filename. Read filepath.Base() godoc for explanation.
			if cloudConfFilename == "." || os.IsPathSeparator(cloudConfFilename[0]) {
				return fmt.Errorf("could not get cloud config filename from %s", results[0])
			}

			filesToTranslate[results[1]] = fileTranslation{
				dest: filepath.Join(wmcb.installDir, cloudConfFilename),
			}

			// Set the --cloud-config option value
			wmcb.kubeletArgs[cloudConfigOption] = path.Join(wmcb.installDir, cloudConfFilename)
		}

		results = verbosityRegex.FindStringSubmatch(unit.Contents)
		if len(results) == 2 {
			wmcb.kubeletArgs["v"] = results[1]
		}

		// Set the worker label
		results = nodeLabelRegex.FindStringSubmatch(unit.Contents)
		if len(results) == 2 {
			// Since labels are comma separated values, split them, as we're only interested in applying the worker
			// label.
			// TODO: Check if we can apply all the labels in future. As of now, we're interested only in the worker
			// label the rest can be ignored
			nodeLabels := strings.Split(results[1], ",")
			for _, nodeLabel := range nodeLabels {
				// Get the worker label, usually it's a standard label
				if strings.Contains(nodeLabel, workerLabel) {
					wmcb.kubeletArgs["node-labels"] = nodeLabel
				}
			}
		}
	}

	// For each new file in the ignition file check if is a file we are interested in, if so, decode, transform,
	// and write it to the destination path
	for _, ignFile := range configuration.Storage.Files {
		if filePair, ok := filesToTranslate[ignFile.Node.Path]; ok {
			newContents, err := wmcb.translateFile(ignFile.Contents.Source, filePair.translationFunc)
			if err != nil {
				return fmt.Errorf("could not process %s: %s", ignFile.Node.Path, err)
			}
			if err = ioutil.WriteFile(filePair.dest, newContents, 0644); err != nil {
				return fmt.Errorf("could not write to %s: %s", filePair.dest, err)
			}
		}
	}

	return nil
}

// initializeKubeletFiles initializes the files required by the kubelet
func (wmcb *winNodeBootstrapper) initializeKubeletFiles() error {
	filesToTranslate := map[string]fileTranslation{
		"/etc/kubernetes/kubelet.conf": {
			dest:            wmcb.kubeletConfPath,
			translationFunc: prepKubeletConfForWindows,
		},
		"/etc/kubernetes/kubeconfig": {
			dest: filepath.Join(wmcb.installDir, "bootstrap-kubeconfig"),
		},
		"/etc/kubernetes/kubelet-ca.crt": {
			dest: filepath.Join(wmcb.installDir, "kubelet-ca.crt"),
		},
	}
	err := os.MkdirAll(wmcb.installDir, os.ModeDir)
	if err != nil {
		return fmt.Errorf("could not make install directory: %s", err)
	}
	if wmcb.initialKubeletPath != "" {
		err = copyFile(wmcb.initialKubeletPath, filepath.Join(wmcb.installDir, "kubelet.exe"))
		if err != nil {
			return fmt.Errorf("could not copy kubelet: %s", err)
		}
	}
	// Populate destination directory with the files we need
	if wmcb.ignitionFilePath != "" {
		ignitionFileContents, err := ioutil.ReadFile(wmcb.ignitionFilePath)
		if err != nil {
			return fmt.Errorf("could not read ignition file: %s", err)
		}

		err = wmcb.parseIgnitionFileContents(ignitionFileContents, filesToTranslate)
		if err != nil {
			return fmt.Errorf("could not parse ignition file: %s", err)
		}
	}
	return nil
}

// createKubeletService creates a new kubelet service to our specifications
func (wmcb *winNodeBootstrapper) createKubeletService() error {
	var err error
	kubeletArgs := []string{
		"--config=" + wmcb.kubeletConfPath,
		"--bootstrap-kubeconfig=" + filepath.Join(wmcb.installDir, "bootstrap-kubeconfig"),
		"--kubeconfig=" + wmcb.kubeconfigPath,
		"--pod-infra-container-image=" + kubeletPauseContainerImage,
		"--cert-dir=" + certDirectory,
		"--windows-service",
		"--logtostderr=false",
		"--log-file=" + filepath.Join(wmcb.installDir, "kubelet.log"),
		// Registers the Kubelet with Windows specific taints so that linux pods won't get scheduled onto
		// Windows nodes.
		// TODO: Write a `against the cluster` e2e test which checks for the Windows node object created
		// and check for taint.
		"--register-with-taints=" + windowsTaints,
		// TODO: Uncomment this when we have a CNI solution
		/*
			network-plugin=cni",
			cni-bin-dir=" + filepath.Join(k8sInstallDir, "cni"),
			cni-conf-dir=" + filepath.Join(k8sInstallDir, "cni"),
		*/
	}
	if cloudProvider, ok := wmcb.kubeletArgs["cloud-provider"]; ok {
		kubeletArgs = append(kubeletArgs, "--cloud-provider="+cloudProvider)
	}
	if v, ok := wmcb.kubeletArgs["v"]; ok {
		kubeletArgs = append(kubeletArgs, "--v="+v)
	}
	if cloudConfigValue, ok := wmcb.kubeletArgs[cloudConfigOption]; ok {
		kubeletArgs = append(kubeletArgs, "--"+cloudConfigOption+"="+cloudConfigValue)
	}
	if nodeWorkerLabel, ok := wmcb.kubeletArgs["node-labels"]; ok {
		kubeletArgs = append(kubeletArgs, "--"+"node-labels"+"="+nodeWorkerLabel)
	}

	// Mostly default values here
	c := mgr.Config{
		ServiceType: 0,
		// StartAutomatic will start the service again if the node restarts
		StartType:    mgr.StartAutomatic,
		ErrorControl: 0,
		// Path to kubelet.exe
		BinaryPathName:   filepath.Join(wmcb.installDir, "kubelet.exe"),
		LoadOrderGroup:   "",
		TagId:            0,
		Dependencies:     nil,
		ServiceStartName: "",
		DisplayName:      "",
		Password:         "",
		Description:      "OpenShift Kubelet",
	}
	wmcb.kubeletSVC, err = wmcb.svcMgr.CreateService(KubeletServiceName, filepath.Join(wmcb.installDir, "kubelet.exe"), c, kubeletArgs...)
	if err != nil {
		return err
	}
	err = wmcb.kubeletSVC.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5},
	}, 600)
	if err != nil {
		return err
	}
	return nil
}

// startKubeletService starts the kubelet as a Windows service
func (wmcb *winNodeBootstrapper) startKubeletService() error {
	if wmcb.kubeletSVC == nil {
		return fmt.Errorf("no kubelet service")
	}
	err := wmcb.kubeletSVC.Start()
	if err != nil {
		return err
	}
	return nil
}

// removeKubeletService deletes the kubelet service via the Windows service API
func (wmcb *winNodeBootstrapper) removeKubeletService() error {
	if wmcb.kubeletSVC == nil {
		return nil
	}
	return wmcb.kubeletSVC.Delete()
}

// controlService sends a signal to the service and waits until it changes state in response to the signal
func (wmcb *winNodeBootstrapper) controlService(cmd svc.Cmd, desiredState svc.State) error {
	status, err := wmcb.kubeletSVC.Control(cmd)
	if err != nil {
		return err
	}
	// Most of the rest of the function borrowed from the package (golang.org/x/sys/windows/svc/mgr) example
	// Arbitrary wait time
	timeout := time.Now().Add(serviceWaitTime)
	for status.State != desiredState {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", desiredState)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = wmcb.kubeletSVC.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}
	return nil
}

// stopKubeletService stops the kubelet via the Windows service API
func (wmcb *winNodeBootstrapper) stopKubeletService() error {
	if wmcb.kubeletSVC == nil {
		return nil
	}
	return wmcb.controlService(svc.Stop, svc.Stopped)
}

// TODO: Remove OVN service as well
// StopAndRemoveServices stops and removes the kubelet service
func (wmcb *winNodeBootstrapper) StopAndRemoveServices() error {
	if wmcb.kubeletSVC == nil {
		return nil
	}
	wmcb.stopKubeletService()
	return wmcb.removeKubeletService()

}

// refreshServiceManager will disconnect and reconnect from the Windows service API. In order to complete certain
// operations, there must be zero handlers to the API present on the system.
func (wmcb *winNodeBootstrapper) refreshServiceManager() error {
	var err error
	if err = wmcb.Disconnect(); err != nil {
		return err
	}
	// We need to give Windows time to clean up the services we've marked for deletion
	time.Sleep(serviceWaitTime)
	wmcb.svcMgr, err = mgr.Connect()
	return err
}

// InitializeKubelet performs the initial kubelet configuration. It sets up the install directory, creates the kubelet
// service, and then starts the kubelet service
func (wmcb *winNodeBootstrapper) InitializeKubelet() error {
	var err error
	if wmcb.kubeletSVC != nil {
		// if the kubelet service exists, we silently remove it and continue, to preserve idempotency
		err = wmcb.StopAndRemoveServices()
		if err != nil {
			return err
		}
		// We need to refresh the service to allow the service to be removed by Windows
		err = wmcb.refreshServiceManager()
		if err != nil {
			return err
		}
	}
	err = wmcb.initializeKubeletFiles()
	if err != nil {
		return err
	}
	err = wmcb.createKubeletService()
	if err != nil {
		return err
	}
	err = wmcb.startKubeletService()
	if err != nil {
		return err
	}
	return nil
}

// Disconnect removes all connections to the Windows service manager api, and allows services to be deleted
func (wmcb *winNodeBootstrapper) Disconnect() error {
	if wmcb.kubeletSVC != nil {
		err := wmcb.kubeletSVC.Close()
		if err != nil {
			return err
		}
	}
	err := wmcb.svcMgr.Disconnect()
	wmcb.svcMgr = nil
	return err
}

func copyFile(src, dest string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	to, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	return err
}
