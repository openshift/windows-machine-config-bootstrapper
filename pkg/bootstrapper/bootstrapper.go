package bootstrapper

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	ignitionCfgv24tov31 "github.com/coreos/ign-converter/translate/v24tov31"
	ignitionCfgv2_4 "github.com/coreos/ignition/config/v2_4"
	ignitionCfgv2_4Types "github.com/coreos/ignition/config/v2_4/types"
	ignitionCfgError "github.com/coreos/ignition/v2/config/shared/errors"
	ignitionCfgv3 "github.com/coreos/ignition/v2/config/v3_1"
	ignitionCfgv3Types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/pkg/errors"
	"github.com/vincent-petithory/dataurl"
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
	// kubeletDependentSvc is the name of the service dependent on kubelet Windows service
	kubeletDependentSvc = "hybrid-overlay-node"
	// kubeletSystemdName is the name of the systemd service that the kubelet runs under,
	// this is used to parse the kubelet args
	kubeletSystemdName = "kubelet.service"
	// kubeletPauseContainerImage is the location of the image we will use for the kubelet pause container
	kubeletPauseContainerImage = "mcr.microsoft.com/oss/kubernetes/pause:1.3.0"
	// serviceWaitTime is amount of wait time required for the Windows service API to complete stop requests
	serviceWaitTime = time.Second * 20
	// certDirectory is where the kubelet will look for certificates
	certDirectory = "c:\\var\\lib\\kubelet\\pki\\"
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
	// nodeLabel contains the os specific label that will be applied to the Windows node object. This can be used to
	// identify the nodes managed by WSU and future operators. (We could have gotten this from boostrap kubeconfig too
	// however the label value is resolved on the host side, making it convenient when we run WMCB within a container)
	nodeLabel = "node.openshift.io/os_id=Windows"

	// kubeletExeKey is the map key used for the kubelet.exe in the deconstructed kubelet map
	kubeletExeKey = "kubeletexe"
	// kubeletStandAloneArgsKey is the map key used for standalone kubelet args like --windows-service in the
	//deconstructed map
	kubeletStandAloneArgsKey = "standalone"

	// CNI constants
	// cniDirName is the directory within the install dir where the CNI binaries are placed
	cniDirName = "cni"
	// cniConfigDirName is the directory in the CNI dir where the cni.conf is placed
	cniConfigDirName = cniDirName + "/config/"

	// kubelet CLI options for CNI
	// resolvOption is to specify the resolv.conf
	resolvOption = "--resolv-conf"
	// resolvValue is the default value passed to the resolv option
	resolvValue = "\"\""
	// networkPluginOption is to specify the network plugin type
	networkPluginOption = "--network-plugin"
	// networkPluginValue is the default network plugin that we support
	networkPluginValue = "cni"
	// cniBinDirOption is to specify the CNI binary directory
	cniBinDirOption = "--cni-bin-dir"
	// cniConfDirOption is to specify the CNI conf directory
	cniConfDirOption = "--cni-conf-dir"
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
	// kubeletSVC is a pointer to the kubeletService struct
	kubeletSVC *kubeletService
	// svcMgr is used to interact with the Windows service API
	svcMgr *mgr.Mgr
	// installDir is the directory the the kubelet service will be installed
	installDir string
	// logDir is the directory that captures log outputs of Kubelet
	// TODO: make this directory available in Artifacts
	logDir string
	// kubeletArgs is a map of the variable arguments that will be passed to the kubelet
	kubeletArgs map[string]string
	// cni holds all the CNI specific information
	cni *cniOptions
}

// cniOptions is responsible for reconfiguring the kubelet service with CNI configuration
type cniOptions struct {
	// k8sInstallDir is the main installation directory
	k8sInstallDir string
	// dir is the input dir where the CNI binaries are present
	dir string
	// config is the input CNI configuration file
	config string
	// binDir is the directory where the CNI binaries will be placed
	binDir string
	// confDir is the directory where the CNI config will be placed
	confDir string
}

// NewWinNodeBootstrapper takes the dir to install the kubelet to, and paths to the ignition and kubelet files along
// with the CNI options as inputs, and generates the winNodeBootstrapper object. The CNI options are populated only in
// the configure-cni command.
func NewWinNodeBootstrapper(k8sInstallDir, ignitionFile, kubeletPath string, cniDir string,
	cniConfig string) (*winNodeBootstrapper, error) {
	// Check if cniDir or cniConfig is empty when the other is not
	if (cniDir == "" && cniConfig != "") || (cniDir != "" && cniConfig == "") {
		return nil, fmt.Errorf("both cniDir and cniConfig need to be populated")
	}

	svcMgr, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("could not connect to Windows SCM: %s", err)
	}
	bootstrapper := winNodeBootstrapper{
		kubeconfigPath:     filepath.Join(k8sInstallDir, "kubeconfig"),
		kubeletConfPath:    filepath.Join(k8sInstallDir, "kubelet.conf"),
		ignitionFilePath:   ignitionFile,
		installDir:         k8sInstallDir,
		logDir:             "C:\\var\\log\\kubelet",
		initialKubeletPath: kubeletPath,
		svcMgr:             svcMgr,
		kubeletArgs:        make(map[string]string),
	}
	// populate the CNI struct if CNI options are present
	if cniDir != "" && cniConfig != "" {
		bootstrapper.cni, err = newCNIOptions(k8sInstallDir, cniDir, cniConfig)
		if err != nil {
			return nil, fmt.Errorf("could not initialize cniOptions: %v", err)
		}
	}

	// If there is already a kubelet service running, find and assign it
	bootstrapper.kubeletSVC, err = assignExistingKubelet(svcMgr)
	if err != nil {
		return nil, fmt.Errorf("could not assign existing kubelet service: %v", err)
	}
	return &bootstrapper, nil
}

// assignExistingKubelet finds the existing kubelet service from the Windows Service Manager,
// assigns its value to the kubeletService struct and returns it.
func assignExistingKubelet(svcMgr *mgr.Mgr) (*kubeletService, error) {
	ksvc, err := svcMgr.OpenService(KubeletServiceName)
	if err != nil {
		// Do not return error if the service is not installed.
		if !strings.Contains(err.Error(), "service does not exist") {
			return nil, fmt.Errorf("error getting existing kubelet service %v", err)
		}
		return nil, nil
	}
	dependents, err := updateKubeletDependents(svcMgr)
	if err != nil {
		return nil, fmt.Errorf("error updating kubelet dependents field %v", err)
	}
	kubeletSVC, err := newKubeletService(ksvc, dependents)
	if err != nil {
		return nil, fmt.Errorf("could not initialize struct kubeletService: %v", err)
	}
	return kubeletSVC, nil
}

// newCNIOptions takes the paths to the kubelet installation and the CNI files as input and returns the cniOptions
// object
func newCNIOptions(k8sInstallDir, dir, config string) (*cniOptions, error) {
	if err := checkCNIInputs(k8sInstallDir, dir, config); err != nil {
		return nil, err
	}

	return &cniOptions{
		k8sInstallDir: k8sInstallDir,
		dir:           dir,
		config:        config,
		binDir:        filepath.Join(k8sInstallDir, cniDirName),
		confDir:       filepath.Join(k8sInstallDir, cniConfigDirName),
	}, nil
}

// translationFunc is a function that takes a byte array and changes it for use on windows
type translationFunc func(*winNodeBootstrapper, []byte) ([]byte, error)

// fileTranslation indicates where a file should be written and what should be done to the contents
type fileTranslation struct {
	dest string
	translationFunc
}

// kubeletConf defines fields of kubelet.conf file that are defined by WMCB variables
type kubeletConf struct {
	// ClientCAFile specifies location to client certificate
	ClientCAFile string
}

// createKubeletConf creates config file for kubelet, with Windows specific configuration
// Add values in kubelet_config.json files, for additional static fields.
// Add fields in kubeletConf struct for variable fields
func (wmcb *winNodeBootstrapper) createKubeletConf() ([]byte, error) {
	// get config file content using bindata.go
	content, err := Asset("templates/kubelet_config.json")

	if err != nil {
		return nil, fmt.Errorf("error reading kubelet config template: %v", err)
	}
	kubeletConfTmpl := template.New("kubeletconf")

	// Parse the template
	kubeletConfTmpl, err = kubeletConfTmpl.Parse(string(content))
	if err != nil {
		return nil, err
	}
	// Fill up the config file, using kubeletConf struct
	variableFields := kubeletConf{
		ClientCAFile: strings.Join(append(strings.Split(wmcb.installDir, `\`), `kubelet-ca.crt`), `\\`),
	}
	// Create kubelet.conf file
	kubeletConfPath := filepath.Join(wmcb.installDir, "kubelet.conf")
	kubeletConfFile, err := os.Create(kubeletConfPath)
	if err != nil {
		return nil, fmt.Errorf("error creating %s: %v", kubeletConfPath, err)
	}
	err = kubeletConfTmpl.Execute(kubeletConfFile, variableFields)
	if err != nil {
		return nil, fmt.Errorf("error writing data to %v file: %v", kubeletConfPath, err)
	}

	kubeletConfData, err := ioutil.ReadFile(kubeletConfFile.Name())
	if err != nil {
		return nil, fmt.Errorf("error reading data from %v file: %v", kubeletConfPath, err)
	}
	return kubeletConfData, nil
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

// convertIgnition2to3 takes an ignition spec v2.4 config and returns a v3.1 config
func convertIgnition2to3(ign2config ignitionCfgv2_4Types.Config) (ignitionCfgv3Types.Config, error) {
	// only support writing to root file system
	fsMap := map[string]string{
		"root": "/",
	}
	dedupedIgn2config, err := ignitionCfgv24tov31.RemoveDuplicateFilesAndUnits(ign2config)
	if err != nil {
		return ignitionCfgv3Types.Config{}, errors.Errorf("unable to deduplicate Ignition spec v2 config: %v", err)
	}
	ign3_1config, err := ignitionCfgv24tov31.Translate(dedupedIgn2config, fsMap)
	if err != nil {
		return ignitionCfgv3Types.Config{}, errors.Errorf("unable to convert Ignition spec v2 config to v3: %v", err)
	}

	return ign3_1config, nil
}

// parseIgnitionFileContents parses the ignition file contents and writes the contents of the described files to the k8s
// installation directory
func (wmcb *winNodeBootstrapper) parseIgnitionFileContents(ignitionFileContents []byte,
	filesToTranslate map[string]fileTranslation) error {
	// Parse raw file contents for Ignition spec v3.1 config
	configuration, report, err := ignitionCfgv3.Parse(ignitionFileContents)
	if err != nil && err.Error() == ignitionCfgError.ErrUnknownVersion.Error() {
		// the Ignition config spec v2.4 parser supports parsing all spec versions up to 2.4
		configV2, reportV2, errV2 := ignitionCfgv2_4.Parse(ignitionFileContents)
		if errV2 != nil || reportV2.IsFatal() {
			return errors.Errorf("failed to parse Ign spec v2 config: %v\nReport: %v", errV2, reportV2)
		}
		configuration, err = convertIgnition2to3(configV2)
		if err != nil {
			return err
		}
	} else if err != nil || report.IsFatal() {
		return errors.Errorf("failed to parse Ign spec v3.1 config: %v\nReport: %v", err, report)
	}

	// Find the kubelet systemd service specified in the ignition file and grab the variable arguments
	// TODO: Refactor this to handle environment variables in argument values
	for _, unit := range configuration.Systemd.Units {
		if unit.Name != kubeletSystemdName {
			continue
		}

		if unit.Contents == nil {
			return fmt.Errorf("could not process %s: Unit is empty", unit.Name)
		}

		results := cloudProviderRegex.FindStringSubmatch(*unit.Contents)
		if len(results) == 2 {
			wmcb.kubeletArgs["cloud-provider"] = results[1]
		}

		// Check for the presence of "--cloud-config" option and if it is present append the value to
		// filesToTranslate. This option is only present for Azure and hence we cannot assume it as a file that
		// requires translation across clouds.
		results = cloudConfigRegex.FindStringSubmatch(*unit.Contents)
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
			wmcb.kubeletArgs[cloudConfigOption] = filepath.Join(wmcb.installDir, cloudConfFilename)
		}

		results = verbosityRegex.FindStringSubmatch(*unit.Contents)
		if len(results) == 2 {
			wmcb.kubeletArgs["v"] = results[1]
		}
	}

	// In case the verbosity argument is missing, use a default value
	if wmcb.kubeletArgs["v"] == "" {
		wmcb.kubeletArgs["v"] = "3"
	}

	// For each new file in the ignition file check if is a file we are interested in, if so, decode, transform,
	// and write it to the destination path
	for _, ignFile := range configuration.Storage.Files {
		if filePair, ok := filesToTranslate[ignFile.Node.Path]; ok {
			if ignFile.Contents.Source == nil {
				return fmt.Errorf("could not process %s: File is empty", ignFile.Node.Path)
			}

			newContents, err := wmcb.translateFile(*ignFile.Contents.Source, filePair.translationFunc)
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
		"/etc/kubernetes/kubeconfig": {
			dest: filepath.Join(wmcb.installDir, "bootstrap-kubeconfig"),
		},
		"/etc/kubernetes/kubelet-ca.crt": {
			dest: filepath.Join(wmcb.installDir, "kubelet-ca.crt"),
		},
	}

	// Create the manifest directory needed by kubelet for the static pods, we shouldn't override if the pod manifest
	// directory already exists
	podManifestDirectory := filepath.Join(wmcb.installDir, "etc", "kubernetes", "manifests")
	if _, err := os.Stat(podManifestDirectory); os.IsNotExist(err) {
		err := os.MkdirAll(podManifestDirectory, os.ModeDir)
		if err != nil {
			return fmt.Errorf("could not make pod manifest directory: %s", err)
		}
	}

	err := os.MkdirAll(wmcb.installDir, os.ModeDir)
	if err != nil {
		return fmt.Errorf("could not make install directory: %s", err)
	}

	_, err = wmcb.createKubeletConf()
	if err != nil {
		return fmt.Errorf("error creating kubelet configuration %v", err)
	}

	if wmcb.initialKubeletPath != "" {
		err = copyFile(wmcb.initialKubeletPath, filepath.Join(wmcb.installDir, "kubelet.exe"))
		if err != nil {
			return fmt.Errorf("could not copy kubelet: %s", err)
		}
	}

	// Create log directory
	err = os.MkdirAll(wmcb.logDir, os.ModeDir)
	if err != nil {
		return fmt.Errorf("could not make %s directory: %v", wmcb.logDir, err)
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

// getInitialKubeletArgs returns the kubelet args required during initial kubelet start up.
// This function assumes that wmcb.kubeletArgs are populated.
func (wmcb *winNodeBootstrapper) getInitialKubeletArgs() []string {
	// If initialize-kubelet is run after configure-cni, the kubelet args will be overwritten and the CNI
	// configuration will be lost. The assumption is that every time initialize-kubelet is run, configure-cni needs to
	// be run again. WMCO ensures that the initialize-kubelet is run successfully before configure-cni and we don't
	// expect users to execute WMCB directly.
	kubeletArgs := []string{
		"--config=" + wmcb.kubeletConfPath,
		"--bootstrap-kubeconfig=" + filepath.Join(wmcb.installDir, "bootstrap-kubeconfig"),
		"--kubeconfig=" + wmcb.kubeconfigPath,
		"--pod-infra-container-image=" + kubeletPauseContainerImage,
		"--cert-dir=" + certDirectory,
		"--windows-service",
		"--logtostderr=false",
		"--log-file=" + filepath.Join(wmcb.logDir, "kubelet.log"),
		// Registers the Kubelet with Windows specific taints so that linux pods won't get scheduled onto
		// Windows nodes.
		// TODO: Write a `against the cluster` e2e test which checks for the Windows node object created
		// and check for taint.
		"--register-with-taints=" + windowsTaints,
		// Label that WMCB uses
		"--node-labels=" + nodeLabel,
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
	return kubeletArgs
}

// ensureKubeletService creates a new kubelet service to our specifications if it is not already present, else
// it updates the existing kubelet service with our specifications.
func (wmcb *winNodeBootstrapper) ensureKubeletService() error {
	// Mostly default values here
	c := mgr.Config{
		ServiceType: 0,
		// StartAutomatic will start the service again if the node restarts
		StartType:      mgr.StartAutomatic,
		ErrorControl:   0,
		LoadOrderGroup: "",
		TagId:          0,
		// set dependency on docker
		Dependencies:     []string{"docker"},
		ServiceStartName: "",
		DisplayName:      "",
		Password:         "",
		Description:      "OpenShift Kubelet",
	}
	// Get kubelet args
	kubeletArgs := wmcb.getInitialKubeletArgs()

	if wmcb.kubeletSVC == nil {
		if err := wmcb.createKubeletService(c, kubeletArgs); err != nil {
			return fmt.Errorf("failed to create kubelet service : %v ", err)
		}
	} else {
		if err := wmcb.updateKubeletService(c, kubeletArgs); err != nil {
			return fmt.Errorf("failed to update kubelet service : %v ", err)
		}
	}

	if err := wmcb.kubeletSVC.setRecoveryActions(); err != nil {
		return fmt.Errorf("failed to set recovery actions for Windows service %s : %v", KubeletServiceName, err)
	}
	return nil
}

// createKubeletService creates a new kubelet service to our specifications
func (wmcb *winNodeBootstrapper) createKubeletService(c mgr.Config, kubeletArgs []string) error {
	ksvc, err := wmcb.svcMgr.CreateService(KubeletServiceName, filepath.Join(wmcb.installDir, "kubelet.exe"), c, kubeletArgs...)
	if err != nil {
		return err
	}

	wmcb.kubeletSVC, err = newKubeletService(ksvc, nil)
	if err != nil {
		return fmt.Errorf("could not initialize struct kubeletService: %v", err)
	}
	return nil
}

// updateKubeletService updates an existing kubelet service with our specifications
func (wmcb *winNodeBootstrapper) updateKubeletService(config mgr.Config, kubeletArgs []string) error {
	// Get existing config
	existingConfig, err := wmcb.kubeletSVC.config()
	if err != nil {
		return fmt.Errorf("no existing config found")
	}

	// Stop the kubelet service as there could be open file handles from kubelet.exe on the plugin files
	if err := wmcb.kubeletSVC.stop(); err != nil {
		return fmt.Errorf("unable to stop kubelet service: %v", err)
	}
	// Populate existing config with non default values from desired config.
	existingConfig.Dependencies = config.Dependencies
	existingConfig.DisplayName = config.DisplayName
	existingConfig.StartType = config.StartType

	// Create kubelet command to populate config.BinaryPathName
	// Add a space after kubelet.exe followed by the stand alone args
	kubeletcmd := filepath.Join(wmcb.installDir, "kubelet.exe") + " "
	// Add rest of the args
	for _, args := range kubeletArgs {
		kubeletcmd += args + " "
	}
	existingConfig.BinaryPathName = strings.TrimSpace(kubeletcmd)

	// Update service config and restart
	if err := wmcb.kubeletSVC.refresh(existingConfig); err != nil {
		return fmt.Errorf("unable to refresh kubelet service: %v", err)
	}

	// Update dependents field if there is any change
	dependents, err := updateKubeletDependents(wmcb.svcMgr)
	if err != nil {
		return fmt.Errorf("error updating kubelet dependents field %v", err)
	}
	wmcb.kubeletSVC.dependents = dependents

	return nil
}

// InitializeKubelet performs the initial kubelet configuration. It sets up the install directory, creates the kubelet
// service, and then starts the kubelet service
func (wmcb *winNodeBootstrapper) InitializeKubelet() error {
	var err error

	if wmcb.kubeletSVC != nil {
		// Stop kubelet service if it is in Running state. This is required to access kubelet files
		// without getting 'The process cannot access the file because it is being used by another process.' error
		err := wmcb.kubeletSVC.stop()
		if err != nil {
			return fmt.Errorf("failed to stop kubelet service: %v", err)
		}
	}

	err = wmcb.initializeKubeletFiles()
	if err != nil {
		return fmt.Errorf("failed to initialize kubelet: %v", err)
	}

	err = wmcb.ensureKubeletService()
	if err != nil {
		return fmt.Errorf("failed to ensure that kubelet windows service is present: %v", err)
	}
	err = wmcb.kubeletSVC.start()
	if err != nil {
		return fmt.Errorf("failed to start kubelet windows service: %v", err)
	}
	return nil
}

// Configure configures the kubelet service for plugins like CNI
func (wmcb *winNodeBootstrapper) Configure() error {
	// TODO: add && wmcb.csi == null check here when we add CSI support
	if wmcb.cni == nil {
		return fmt.Errorf("cannot configure without required plugin inputs")
	}

	// We cannot proceed if the kubelet service is not present on the system as we need to update it with the plugin
	// configuration
	if wmcb.kubeletSVC == nil {
		return fmt.Errorf("kubelet service is not present")
	}

	// Stop the kubelet service as there could be open file handles from kubelet.exe on the plugin files
	if err := wmcb.kubeletSVC.stop(); err != nil {
		return fmt.Errorf("unable to stop kubelet service: %v", err)
	}

	config, err := wmcb.kubeletSVC.config()
	if err != nil {
		return fmt.Errorf("error getting kubelet service config: %v", err)
	}

	// TODO: add wmcb.cni != null check here when we add CSI support as this function will be called in both cases
	if err = wmcb.cni.configure(&config.BinaryPathName); err != nil {
		return fmt.Errorf("error configuring kubelet service for CNI: %v", err)
	}

	if err = wmcb.kubeletSVC.refresh(config); err != nil {
		return fmt.Errorf("unable to refresh kubelet service: %v", err)
	}

	return nil
}

// Disconnect removes all connections to the Windows service manager api, and allows services to be deleted
func (wmcb *winNodeBootstrapper) Disconnect() error {
	if err := wmcb.kubeletSVC.disconnect(); err != nil {
		return err
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

// checkCNIInputs checks if there are any issues with the CNI inputs to WMCB and returns an error if there is
func checkCNIInputs(k8sInstallDir string, cniDir string, cniConfig string) error {
	// Check if there are any issues accessing the installation directory. We don't want to proceed on any error as it
	// could cause issues further down the line when copying the files.
	if _, err := os.Stat(k8sInstallDir); err != nil {
		return fmt.Errorf("error accessing install directory %s: %v", k8sInstallDir, err)
	}

	// Check if there are any issues accessing the CNI dir. We don't want to proceed on any error as it could cause
	// issues further down the line when copying the files.
	cniPathInfo, err := os.Stat(cniDir)
	if err != nil {
		return fmt.Errorf("error accessing CNI dir %s: %v", cniDir, err)
	}
	if !cniPathInfo.IsDir() {
		return fmt.Errorf("CNI dir cannot be a file")
	}

	// Check if there are files present in the CNI directory
	files, err := ioutil.ReadDir(cniDir)
	if err != nil {
		return fmt.Errorf("error reading CNI dir %s: %v", cniDir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no files present in CNI dir %s", cniDir)
	}

	// Check if there are any issues accessing the CNI configuration file. We don't want to proceed on any error as it
	// could cause issues further down the line when copying the files.
	cniConfigInfo, err := os.Stat(cniConfig)
	if err != nil {
		return fmt.Errorf("error accessing CNI config %s: %v", cniConfig, err)
	}
	if cniConfigInfo.IsDir() {
		return fmt.Errorf("CNI config cannot be a directory")
	}

	return nil
}

// copyFiles() copies the CNI binaries and config to the installation directory
func (cni *cniOptions) copyFiles() error {
	// Read C:\source\cni\
	files, err := ioutil.ReadDir(cni.dir)
	if err != nil {
		return fmt.Errorf("error reading CNI dir %s: %v", cni.dir, err)
	}

	// Copy the CNI binaries from the input CNI dir to the CNI installation directory
	for _, file := range files {
		// Ignore directories for now. If we find that there are CNI packages with nested directories, we can update
		// this to loop to be recursive.
		if file.IsDir() {
			continue
		}

		// C:\source\cni\filename
		src := filepath.Join(cni.dir, file.Name())
		// C:\k\cni\filename
		dest := filepath.Join(cni.binDir, file.Name())
		if err = copyFile(src, dest); err != nil {
			return fmt.Errorf("error copying %s --> %s: %v", src, dest, err)
		}
	}

	// Copy the CNI config to the CNI configuration directory. Example: C:\k\cni\config\cni.conf
	cniConfigDest := filepath.Join(cni.confDir, filepath.Base(cni.config))
	if err = copyFile(cni.config, cniConfigDest); err != nil {
		return fmt.Errorf("error copying CNI config %s --> %s: %v", cni.config, cniConfigDest, err)
	}
	return nil
}

// ensureDirIsPresent ensures that CNI parent and child directories are present on the system
func (cni *cniOptions) ensureDirIsPresent() error {
	// By checking for the config directory, we can ensure both parent and child directories are present
	configDir := filepath.Join(cni.k8sInstallDir, cniConfigDirName)
	if _, err := os.Stat(configDir); err != nil {
		if os.IsNotExist(err) {
			// 0700 == Only user has access
			if err = os.MkdirAll(configDir, 0700); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// deconstructKubeletCmd deconstructs the kubelet command into a map. For arguments like "--config=c:\\k\\kubelet.conf"
// will result in a key "--config" with value "c:\\k\\kubelet in the map. Standalone args like "--windows-service" will
// be stored against a special kubeletStandAloneArgsKey as a string. The kubelet.exe will also be in stored against a
// special kubeletExeKey.
func deconstructKubeletCmd(kubeletCmd *string) (map[string]string, error) {
	if kubeletCmd == nil {
		return nil, fmt.Errorf("nil kubelet cmd passed")
	}

	kubeletArgs := strings.Split(*kubeletCmd, " ")
	kubeletKeyValueArgs := make(map[string]string)

	// Index 0 of kubeletArgs will hold the kubelet.exe. Return an error if it does not.
	if !strings.Contains(kubeletArgs[0], "kubelet.exe") {
		return nil, fmt.Errorf("kubelet command does not start with kubelet.exe")
	}
	kubeletKeyValueArgs[kubeletExeKey] = kubeletArgs[0]

	// We start at index 1 as we want to ignore kubelet.exe
	for _, option := range kubeletArgs[1:] {
		// Args like --config=c:\\k\\kubelet.conf will be split on "=" and stored as key value pairs of the map.
		//Stand alone args like --windows-service will be stored as a string against a special key
		if strings.Contains(option, "=") {
			kv := strings.Split(option, "=")
			kubeletKeyValueArgs[kv[0]] = kv[1]
			// This is to account for args like --register-with-taints=os=Windows:NoSchedule
			if len(kv) > 2 {
				for _, val := range kv[2:] {
					kubeletKeyValueArgs[kv[0]] += "=" + val
				}
			}
		} else {
			kubeletKeyValueArgs[kubeletStandAloneArgsKey] += option + " "
		}
	}

	// Remove the trailing space
	if standaloneArgs, found := kubeletKeyValueArgs[kubeletStandAloneArgsKey]; found {
		kubeletKeyValueArgs[kubeletStandAloneArgsKey] = strings.TrimSpace(standaloneArgs)
	}

	return kubeletKeyValueArgs, nil
}

// reconstructKubeletCmd takes map of CLI options and combines into a kubelet command that can be used in the Windows
// service
func reconstructKubeletCmd(kubeletKeyValueArgs map[string]string) (string, error) {
	if kubeletKeyValueArgs == nil {
		return "", fmt.Errorf("nil map passed")
	}

	kubeletCmd, found := kubeletKeyValueArgs[kubeletExeKey]
	if !found {
		return "", fmt.Errorf("%s key not found in the map", kubeletExeKey)
	}
	// Add a space after kubelet.exe followed by the stand alone args
	kubeletCmd += " " + kubeletKeyValueArgs[kubeletStandAloneArgsKey] + " "

	// Add rest of the key value args
	for key, value := range kubeletKeyValueArgs {
		if key == kubeletExeKey || key == kubeletStandAloneArgsKey {
			continue
		}
		kubeletCmd += key + "=" + value + " "
	}

	// Remove the trailing space
	kubeletCmd = strings.TrimSpace(kubeletCmd)

	return kubeletCmd, nil
}

// updateKubeletArgs updates the given kubelet command with the CNI args.
// Example: --resolv-conf="" --network-plugin=cni --cni-bin-dir=C:\k\cni --cni-conf-dir=c:\k\cni\config
func (cni *cniOptions) updateKubeletArgs(kubeletCmd *string) error {
	if kubeletCmd == nil {
		return fmt.Errorf("nil kubelet cmd passed")
	}

	kubeletKeyValueArgs, err := deconstructKubeletCmd(kubeletCmd)
	if err != nil {
		return fmt.Errorf("unable to deconstruct kubelet command %s: %v", *kubeletCmd, err)
	}

	// Add or replace the CNI CLI args
	kubeletKeyValueArgs[resolvOption] = resolvValue
	kubeletKeyValueArgs[networkPluginOption] = networkPluginValue
	kubeletKeyValueArgs[cniBinDirOption] = cni.binDir
	kubeletKeyValueArgs[cniConfDirOption] = cni.confDir

	if *kubeletCmd, err = reconstructKubeletCmd(kubeletKeyValueArgs); err != nil {
		return fmt.Errorf("unable to reconstruct kubelet command %v: %v", kubeletKeyValueArgs, err)
	}

	return nil
}

// Configure performs the CNI configuration. It sets up the CNI directories and updates the kubelet command with the CNI
// arguments. Updating and restarting the kubelet service is outside of its purview.
func (cni *cniOptions) configure(kubeletCmd *string) error {
	if err := cni.ensureDirIsPresent(); err != nil {
		return fmt.Errorf("unable to create CNI directory %s: %v", filepath.Join(cni.dir, cniConfigDirName), err)
	}

	if err := cni.copyFiles(); err != nil {
		return fmt.Errorf("unable to copy CNI files: %v", err)
	}

	if err := cni.updateKubeletArgs(kubeletCmd); err != nil {
		return fmt.Errorf("unable to update the kubelet arguments: %v", err)
	}

	return nil
}

// updateKubeletDependents updates the dependents field of the kubeletService struct
// to reflect current list of dependent services. This function assumes that the kubelet service is running
func updateKubeletDependents(svcMgr *mgr.Mgr) ([]*mgr.Service, error) {
	var dependents []*mgr.Service
	// If there is already a kubelet service running, find it
	dependentSvc, err := svcMgr.OpenService(kubeletDependentSvc)
	if err != nil {
		// Do not return error if the services are not installed.
		if !strings.Contains(err.Error(), "service does not exist") {
			return nil, fmt.Errorf("error getting dependent services for kubelet %v", err)
		}
	}
	if dependentSvc != nil {
		dependents = append(dependents, dependentSvc)
	}
	return dependents, nil
}
