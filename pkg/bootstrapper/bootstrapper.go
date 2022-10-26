package bootstrapper

import (
	_ "embed"
	"fmt"
	"io"
	"io/ioutil"
	"net"
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

	"github.com/openshift/windows-machine-config-bootstrapper/pkg/cloud"
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
	// KubeletDefaultVerbosity is the recommended default log level for kubelet. See https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md
	KubeletDefaultVerbosity = "3"
	// kubeletDependentSvc is the name of the service dependent on kubelet Windows service
	kubeletDependentSvc = "hybrid-overlay-node"
	// kubeletSystemdName is the name of the systemd service that the kubelet runs under,
	// this is used to parse the kubelet args
	kubeletSystemdName = "kubelet.service"
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
	// managedServicePrefix indicates that the service being described is managed by OpenShift. This ensures that all
	// services created as part of Node configuration can be searched for by checking their description for this string
	managedServicePrefix = "OpenShift managed"
	// containerdEndpointValue is the default value for containerd endpoint required to be updated in kubelet arguments
	containerdEndpointValue = "npipe://./pipe/containerd-containerd"
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

//go:embed templates/kubelet_config.json
var baseConfig string

// winNodeBootstrapper is responsible for bootstrapping and ensuring kubelet runs as a Windows service
type winNodeBootstrapper struct {
	// kubeconfigPath is the file path of the node bootstrap kubeconfig
	kubeconfigPath string
	// kubeletConfPath is the file path of the kubelet configuration
	kubeletConfPath string
	// kubeletVerbosity represents the log level for kubelet
	kubeletVerbosity string
	// ignitionFilePath is the path to the ignition file which is used to set up worker nodes
	// https://github.com/coreos/ignition/blob/spec2x/doc/getting-started.md
	ignitionFilePath string
	// initialKubeletPath is the path to the kubelet that we'll be using to bootstrap this node
	initialKubeletPath string
	// nodeIP is the IP that should be used as the node object's IP. If unset, kubelet will determine the IP itself.
	nodeIP string
	// clusterDNS is the IP address of the DNS server used for all containers
	clusterDNS string
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
	// kubeletArgs is a slice of the arguments that will be passed to the kubelet
	kubeletArgs []string
	// platformType contains type of the platform where the cluster is deployed
	platformType string
}

// NewWinNodeBootstrapper takes the dir to install the kubelet to, the verbosity and paths to the ignition and kubelet
// files, an optional node IP, an optional clusterDNS, along with the CNI options as inputs, and generates the
// winNodeBootstrapper object. The CNI options are populated only in the configure-cni command. The inputs to
// NewWinNodeBootstrapper are ignored while using the uninstall kubelet functionality.
func NewWinNodeBootstrapper(k8sInstallDir, ignitionFile, kubeletPath, kubeletVerbosity, nodeIP, clusterDNS,
	platformType string) (*winNodeBootstrapper, error) {
	// If nodeIP is set, ensure that it is a valid IP
	if nodeIP != "" {
		if parsed := net.ParseIP(nodeIP); parsed == nil {
			return nil, fmt.Errorf("nodeIP value %s is not a valid IP format", nodeIP)
		}
	}

	// If clusterDNS is set, ensure that it is a valid IP
	if clusterDNS != "" {
		if parsed := net.ParseIP(clusterDNS); parsed == nil {
			return nil, fmt.Errorf("clusterDNS value %s is not a valid IP format", clusterDNS)
		}
	}

	svcMgr, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("could not connect to Windows SCM: %s", err)
	}
	bootstrapper := winNodeBootstrapper{
		kubeconfigPath:     filepath.Join(k8sInstallDir, "kubeconfig"),
		kubeletConfPath:    filepath.Join(k8sInstallDir, "kubelet.conf"),
		kubeletVerbosity:   kubeletVerbosity,
		ignitionFilePath:   ignitionFile,
		installDir:         k8sInstallDir,
		logDir:             "C:\\var\\log\\kubelet",
		initialKubeletPath: kubeletPath,
		svcMgr:             svcMgr,
		nodeIP:             nodeIP,
		clusterDNS:         clusterDNS,
		platformType:       platformType,
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
	// ClusterDNS is the IP address of the DNS server used for all containers
	ClusterDNS string
}

// createKubeletConf creates config file for kubelet, with Windows specific configuration
// Add values in kubelet_config.json files, for additional static fields.
// Add fields in kubeletConf struct for variable fields
func (wmcb *winNodeBootstrapper) createKubeletConf() ([]byte, error) {
	kubeletConfTmpl := template.New("kubeletconf")

	// Parse the template
	kubeletConfTmpl, err := kubeletConfTmpl.Parse(baseConfig)
	if err != nil {
		return nil, err
	}
	// Fill up the config file, using kubeletConf struct
	variableFields := kubeletConf{
		ClientCAFile: strings.Join(append(strings.Split(wmcb.installDir, `\`), `kubelet-ca.crt`), `\\`),
	}
	// check clusterDNS
	if wmcb.clusterDNS != "" {
		// surround with double-quotes for valid JSON format
		variableFields.ClusterDNS = "\"" + wmcb.clusterDNS + "\""
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

// parseIgnitionFileContents parses the ignition file contents gathering the required kubelet args, and writing
// the contents of the described files to the k8s installation directory
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
	var kubeletUnit *ignitionCfgv3Types.Unit
	for _, unit := range configuration.Systemd.Units {
		if unit.Name == kubeletSystemdName {
			kubeletUnit = &unit
			break
		}
	}
	if kubeletUnit == nil {
		return errors.Errorf("ignition missing kubelet systemd unit file")
	}
	args, err := wmcb.parseKubeletArgs(*kubeletUnit)
	if err != nil {
		return errors.Wrap(err, "error parsing kubelet systemd unit args")
	}

	// TODO: This is being done because this function is trying to handle both file creation and kubelet arg parsing.
	//       The cloud-config file translation is dependent on the file path given by the ignition file, but for the
	//       kubelet args we want to alter that path. Because of how this function is structured right now, this is
	//       resulting in the altering of the path to be delayed, in order to not have to parse the kubelet unit again.
	//       This should not have been done this way, the setting of the args map should be contained to
	//       parseKubeletArgs. This should be corrected as part of https://issues.redhat.com/browse/WINC-670
	// Check for the presence of "--cloud-config" option and if it is present append the value to
	// filesToTranslate. This option is only present for Azure and hence we cannot assume it as a file that
	// requires translation across clouds.
	if cloudConfigPath, ok := args[cloudConfigOption]; ok {
		cloudConfigFilename := filepath.Base(cloudConfigPath)
		// Check if we were able to get a valid filename. Read filepath.Base() godoc for explanation.
		if cloudConfigFilename == "." || os.IsPathSeparator(cloudConfigFilename[0]) {
			return fmt.Errorf("could not get cloud config filename from %s", cloudConfigPath)
		}
		// As the cloud-config option is a path it must be changed to point to the local file
		localCloudConfigDestination := filepath.Join(wmcb.installDir, cloudConfigFilename)
		args[cloudConfigOption] = localCloudConfigDestination

		// Ensure that we create the cloud-config file
		filesToTranslate[cloudConfigPath] = fileTranslation{
			dest: localCloudConfigDestination,
		}
	}

	// Generate the full list of kubelet arguments from the arguments present in the ignition file
	wmcb.kubeletArgs, err = wmcb.generateInitialKubeletArgs(args)
	if err != nil {
		return fmt.Errorf("cannot generate initial kubelet args: %w", err)
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

// parseKubeletArgs returns args we are interested in from the kubelet systemd unit file
func (wmcb *winNodeBootstrapper) parseKubeletArgs(unit ignitionCfgv3Types.Unit) (map[string]string, error) {
	if unit.Contents == nil {
		return nil, fmt.Errorf("could not process %s: Unit is empty", unit.Name)
	}

	kubeletArgs := make(map[string]string)
	results := cloudProviderRegex.FindStringSubmatch(*unit.Contents)
	if len(results) == 2 {
		kubeletArgs["cloud-provider"] = results[1]
	}

	// Check for the presence of "--cloud-config" option and if it is present append the value to
	// filesToTranslate. This option is only present for Azure and hence we cannot assume it as a file that
	// requires translation across clouds.
	results = cloudConfigRegex.FindStringSubmatch(*unit.Contents)
	if len(results) == 2 {
		kubeletArgs[cloudConfigOption] = results[1]
	}

	results = verbosityRegex.FindStringSubmatch(*unit.Contents)
	if len(results) == 2 {
		kubeletArgs["v"] = results[1]
	}
	return kubeletArgs, nil
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

// generateInitialKubeletArgs returns the kubelet args required during initial kubelet start up. args should be a map
// of the variable options passed along to WMCB via the ignition file.
func (wmcb *winNodeBootstrapper) generateInitialKubeletArgs(args map[string]string) ([]string, error) {
	// If initialize-kubelet is run after configure-cni, the kubelet args will be overwritten and the CNI
	// configuration will be lost. The assumption is that every time initialize-kubelet is run, configure-cni needs to
	// be run again. WMCO ensures that the initialize-kubelet is run successfully before configure-cni and we don't
	// expect users to execute WMCB directly.
	kubeletArgs := []string{
		"--config=" + wmcb.kubeletConfPath,
		"--bootstrap-kubeconfig=" + filepath.Join(wmcb.installDir, "bootstrap-kubeconfig"),
		"--kubeconfig=" + wmcb.kubeconfigPath,
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
		"--container-runtime=remote",
		"--container-runtime-endpoint=" + containerdEndpointValue,
		"--resolv-conf=",
	}
	if cloudProvider, ok := args["cloud-provider"]; ok {
		kubeletArgs = append(kubeletArgs, "--cloud-provider="+cloudProvider)
	}
	// set default verbosity for kubelet
	kubeletVerbosity := KubeletDefaultVerbosity
	// set the verbosity value from the program argument if any, this takes precedence
	if wmcb.kubeletVerbosity != "" {
		kubeletVerbosity = wmcb.kubeletVerbosity
	} else {
		// otherwise look for verbosity value in kubelet systemd unit file configuration
		if v, ok := args["v"]; ok && v != "" {
			// and update, if found
			kubeletVerbosity = v
		}
	}
	kubeletArgs = append(kubeletArgs, "--v="+kubeletVerbosity)

	if cloudConfigValue, ok := args[cloudConfigOption]; ok {
		kubeletArgs = append(kubeletArgs, "--"+cloudConfigOption+"="+cloudConfigValue)
	}
	if nodeWorkerLabel, ok := args["node-labels"]; ok {
		kubeletArgs = append(kubeletArgs, "--"+"node-labels"+"="+nodeWorkerLabel)
	}
	if wmcb.nodeIP != "" {
		kubeletArgs = append(kubeletArgs, "--node-ip="+wmcb.nodeIP)
	}

	hostname, err := cloud.GetKubeletHostnameOverride(wmcb.platformType)
	if err != nil {
		return nil, err
	}
	if hostname != "" {
		kubeletArgs = append(kubeletArgs, "--hostname-override="+hostname)
	}

	return kubeletArgs, nil
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
		// set dependency on containerd
		Dependencies:     []string{"containerd"},
		ServiceStartName: "",
		DisplayName:      "",
		Password:         "",
		Description:      fmt.Sprintf("%s kubelet", managedServicePrefix),
	}

	if wmcb.kubeletSVC == nil {
		if err := wmcb.createKubeletService(c); err != nil {
			return fmt.Errorf("failed to create kubelet service : %v ", err)
		}
	} else {
		if err := wmcb.updateKubeletService(c, wmcb.kubeletArgs); err != nil {
			return fmt.Errorf("failed to update kubelet service : %v ", err)
		}
	}

	if err := wmcb.kubeletSVC.setRecoveryActions(); err != nil {
		return fmt.Errorf("failed to set recovery actions for Windows service %s : %v", KubeletServiceName, err)
	}
	return nil
}

// createKubeletService creates a new kubelet service to our specifications
func (wmcb *winNodeBootstrapper) createKubeletService(c mgr.Config) error {
	ksvc, err := wmcb.svcMgr.CreateService(KubeletServiceName, filepath.Join(wmcb.installDir, "kubelet.exe"), c,
		wmcb.kubeletArgs...)
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

// Disconnect removes all connections to the Windows service manager api, and allows services to be deleted
func (wmcb *winNodeBootstrapper) Disconnect() error {
	if err := wmcb.kubeletSVC.disconnect(); err != nil {
		return err
	}
	err := wmcb.svcMgr.Disconnect()
	wmcb.svcMgr = nil
	return err
}

// UninstallKubelet uninstalls the kubelet service from Windows node
func (wmcb *winNodeBootstrapper) UninstallKubelet() error {
	if wmcb.kubeletSVC == nil {
		return fmt.Errorf("kubelet service is not present")
	}
	// Stop and remove kubelet service if it is in Running state.
	err := wmcb.kubeletSVC.stopAndRemove()
	if err != nil {
		return fmt.Errorf("failed to stop and remove kubelet service: %v", err)
	}
	return nil
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
