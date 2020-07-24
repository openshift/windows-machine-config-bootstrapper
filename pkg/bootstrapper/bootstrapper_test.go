package bootstrapper

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//cniTest holds the location of the directories and files required for running some of the CNI tests
type cniTestOptions struct {
	// k8sInstallDir is the main installation directory
	k8sInstallDir string
	// dir is the input dir where the CNI binaries are present
	dir string
	// config is the input CNI configuration file
	config string
	// exe is a dummy CNI executable
	exe string
	// cni is the common cniOptions used across the tests
	cni *cniOptions
}

var cniTest cniTestOptions

func initCNITestFramework() error {
	err := createFilesAndDirsRequiredForTests()
	if err != nil {
		return fmt.Errorf("error creating temp directories and files: %v", err)
	}

	cniTest.cni, err = newCNIOptions(cniTest.k8sInstallDir, cniTest.dir, cniTest.config)
	if err != nil {
		return fmt.Errorf("error initializing CNI options: %v", err)
	}

	return nil
}

func createFilesAndDirsRequiredForTests() error {
	// Create a temp directory with wmcb prefix
	installDir, err := ioutil.TempDir("", "wmcb")
	if err != nil {
		return fmt.Errorf("error creating temp directory: %v", err)
	}
	cniTest.k8sInstallDir = installDir

	// Create a temp directory with cni prefix
	cniDir, err := ioutil.TempDir("", "cni")
	if err != nil {
		return fmt.Errorf("error creating temp CNI directory: %v", err)
	}
	cniTest.dir = cniDir

	// Create temp CNI file
	cniExe, err := ioutil.TempFile(cniDir, "cni.exe")
	if err != nil {
		return fmt.Errorf("error creating CNI exe: %v", err)
	}
	cniTest.exe = cniExe.Name()

	// Create temp CNI config dir
	cniConfigPath, err := ioutil.TempDir(cniDir, "cni")
	if err != nil {
		return fmt.Errorf("error creating temp CNI config directory: %v", err)
	}

	// Create temp CNI config file
	cniConfig, err := ioutil.TempFile(cniConfigPath, "cni.conf")
	if err != nil {
		return fmt.Errorf("error creating CNI config: %v", err)
	}
	cniTest.config = cniConfig.Name()
	return nil
}

// TestTranslateFile tests decoding and transforming ignition file sources
func TestTranslateFile(t *testing.T) {
	type args struct {
		input  string
		lambda translationFunc
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "No translation function",
			args: args{
				input:  "data:,-----BEGIN%20CERTIFICATE-----%0AMIIDEDCCAfigAwIBAgIIKH9ePWRYTs8wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX%0ADTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy%0Ab290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5%0A%2Bw0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5%0AEw3Ovuaz%2FKcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ%0Ad%2FuZnRIT7dsR%2BVdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM%0AkP%2Fy73wzKEMtWPjNA%2BJaJdNf4n7Mh57fwO9IMrmMQWZP7d%2B8kFMnfQygXPopqFQR%0ADhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E%2BwCZ7HQV8J5V6WlO8wR0R%0AC8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%0A%2FzAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N%2Bd%2BAxxOkwDQYJKoZIhvcNAQELBQAD%0AggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42%0A%2BSKzN63i%2FSreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7%2BfWZmD4VBrUqI%0AFbrOEMZbc9vPxvpbN%2FinzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn%2Fx%0AK58cZ7SBbK9oyk%2FCF2St%2F9TR7unuNFDq1TPsjSKxJMC%2FsTyEcW6ABCOjcqu94eWt%0AUHfH1Be25D8kcN0%2FtdrJt4NgawQINUr0QIkSsY%2B3hh8AUHSvyCbiiCrt%2Fn7jjF7G%0ArqLuyNO%2BhCh%2FZclPL%2BUiGJH1dlQ%3D%0A-----END%20CERTIFICATE-----",
				lambda: nil,
			},
			want: []byte(`-----BEGIN CERTIFICATE-----
MIIDEDCCAfigAwIBAgIIKH9ePWRYTs8wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE
CxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX
DTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy
b290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5
+w0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5
Ew3Ovuaz/KcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ
d/uZnRIT7dsR+VdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM
kP/y73wzKEMtWPjNA+JaJdNf4n7Mh57fwO9IMrmMQWZP7d+8kFMnfQygXPopqFQR
DhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E+wCZ7HQV8J5V6WlO8wR0R
C8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N+d+AxxOkwDQYJKoZIhvcNAQELBQAD
ggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42
+SKzN63i/SreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7+fWZmD4VBrUqI
FbrOEMZbc9vPxvpbN/inzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn/x
K58cZ7SBbK9oyk/CF2St/9TR7unuNFDq1TPsjSKxJMC/sTyEcW6ABCOjcqu94eWt
UHfH1Be25D8kcN0/tdrJt4NgawQINUr0QIkSsY+3hh8AUHSvyCbiiCrt/n7jjF7G
rqLuyNO+hCh/ZclPL+UiGJH1dlQ=
-----END CERTIFICATE-----`),
		},
		{
			name: "Using translation function",
			args: args{
				input: "data:,-----BEGIN%20CERTIFICATE-----%0AMIIDEDCCAfigAwIBAgIIKH9ePWRYTs9wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE%0ACxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX%0ADTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy%0Ab290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5%0A%2Bw0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5%0AEw3Ovuaz%2FKcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ%0Ad%2FuZnRIT7dsR%2BVdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM%0AkP%2Fy73wzKEMtWPjNA%2BJaJdNf4n7Mh57fwO9IMrmMQWZP7d%2B8kFMnfQygXPopqFQR%0ADhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E%2BwCZ7HQV8J5V6WlO8wR0R%0AC8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH%2FBAUwAwEB%0A%2FzAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N%2Bd%2BAxxOkwDQYJKoZIhvcNAQELBQAD%0AggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42%0A%2BSKzN63i%2FSreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7%2BfWZmD4VBrUqI%0AFbrOEMZbc9vPxvpbN%2FinzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn%2Fx%0AK58cZ7SBbK9oyk%2FCF2St%2F9TR7unuNFDq1TPsjSKxJMC%2FsTyEcW6ABCOjcqu94eWt%0AUHfH1Be25D8kcN0%2FtdrJt4NgawQINUr0QIkSsY%2B3hh8AUHSvyCbiiCrt%2Fn7jjF7G%0ArqLuyNO%2BhCh%2FZclPL%2BUiGJH1dlQ%3D%0A-----END%20CERTIFICATE-----",
				lambda: func(bs *winNodeBootstrapper, in []byte) ([]byte, error) {
					return []byte(string(in) + "suffix"), nil
				},
			},
			want: []byte(`-----BEGIN CERTIFICATE-----
MIIDEDCCAfigAwIBAgIIKH9ePWRYTs9wDQYJKoZIhvcNAQELBQAwJjESMBAGA1UE
CxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdyb290LWNhMB4XDTE5MDkxMDE0MjkzMloX
DTI5MDkwNzE0MjkzMlowJjESMBAGA1UECxMJb3BlbnNoaWZ0MRAwDgYDVQQDEwdy
b290LWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4FcWyu6Nsdb5
+w0r1101FTPjw2W392K8mJgm8tI852WxnVdC41vpkpreNZhHpef2LYemRbX3LVv5
Ew3Ovuaz/KcsVASg5MpP0XgzFUhHT1UgAdFvh08GtUGZedXb9di66TJHnYoVrSsJ
d/uZnRIT7dsR+VdmMhB0N2vcBsLOilG3XaR24h3UmeB8cqkKxzmaG2dKf1Z1MiyM
kP/y73wzKEMtWPjNA+JaJdNf4n7Mh57fwO9IMrmMQWZP7d+8kFMnfQygXPopqFQR
DhOjG1D52hzExHWD08ShnossHJWt9ETo2eb9D1djf3E+wCZ7HQV8J5V6WlO8wR0R
C8fjKImLjQIDAQABo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB
/zAdBgNVHQ4EFgQUUEKZ3tCtmqwA26fFx0N+d+AxxOkwDQYJKoZIhvcNAQELBQAD
ggEBAAqAeBN7G5S1hsDiNd2lZwI5eNuGGk5T5tOEwCIuKHaSxnwkmn7qKymjsm42
+SKzN63i/SreK8CONW6Xp8kUNQW3J6iziRQD11uR8jZVoezqCW7+fWZmD4VBrUqI
FbrOEMZbc9vPxvpbN/inzKJoSLUGTtzN7CjsLmf4XdTFtEr9qBPpOFb0i3gaYn/x
K58cZ7SBbK9oyk/CF2St/9TR7unuNFDq1TPsjSKxJMC/sTyEcW6ABCOjcqu94eWt
UHfH1Be25D8kcN0/tdrJt4NgawQINUr0QIkSsY+3hh8AUHSvyCbiiCrt/n7jjF7G
rqLuyNO+hCh/ZclPL+UiGJH1dlQ=
-----END CERTIFICATE-----suffix`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := winNodeBootstrapper{installDir: filepath.Base("tmp")}
			got, err := bs.translateFile(tt.args.input, tt.args.lambda)
			assert.NoError(t, err)
			assert.Equalf(t, tt.want, got, "got = %v, want %v", string(got), string(tt.want))
		})
	}
}

// TestCreateKubeletConf tests that we are creating the kubelet configuration in a way that allows it to run on windows
func TestCreateKubeletConf(t *testing.T) {
	type args struct {
		in []byte
	}
	instDir := `C:\k`
	err := os.MkdirAll(instDir, 0755)
	require.NoError(t, err, "error creating install directory")

	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "Base case",
			want: []byte(`{"kind":"KubeletConfiguration","apiVersion":"kubelet.config.k8s.io/v1beta1","rotateCertificates":true,"serverTLSBootstrap":true,"authentication":{"x509":{"clientCAFile":"C:\\k\\kubelet-ca.crt "},"anonymous":{"enabled":false}},"clusterDomain":"cluster.local","clusterDNS":["172.30.0.10"],"cgroupsPerQOS":false,"runtimeRequestTimeout":"10m0s","maxPods":250,"kubeAPIQPS":50,"kubeAPIBurst":100,"serializeImagePulls":false,"featureGates":{"LegacyNodeRoleBehavior":false,"NodeDisruptionExclusion":true,"RotateKubeletServerCertificate":true,"SCTPSupport":true,"ServiceNodeExclusion":true,"SupportPodPidsLimit":true},"containerLogMaxSize":"50Mi","systemReserved":{"cpu":"500m","ephemeral-storage":"1Gi","memory":"1Gi"},"enforceNodeAllocatable":[]}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := winNodeBootstrapper{installDir: instDir}
			got, err := bs.createKubeletConf()
			assert.NoError(t, err)
			assert.Equalf(t, tt.want, got, "got = %v, want %v", string(got), string(tt.want))
		})
	}
}

// TestCloudConfExtraction tests if parseIgnitionFileContents can extract the cloud.conf present in a worker ignition
// file contents and the resulting file is in the expected format with a set of key value pairs.
// It also confirms the "--cloud-config" option constructed by WMCB is as expected. Example cloud.conf:
// {
//	"cloud": "AzurePublicCloud",
//	"tenantId": "1234a1b2-a1bc-123a-123a-ab1c2de3afgh",
//	"aadClientId": "",
//	"aadClientSecret": "",
//	"aadClientCertPath": "",
//	"aadClientCertPassword": "",
//	"useManagedIdentityExtension": true,
//	"userAssignedIdentityID": "",
//	"subscriptionId": "1a123456-12ab-123a-1234-abc1d1ab01c0",
//	"resourceGroup": "winc-test-rg",
//	"location": "centralus",
//	"vnetName": "winc-test-vnet",
//	"vnetResourceGroup": "winc-test-rg",
//	"subnetName": "winc-test-node-subnet",
//	"securityGroupName": "winc-test-node-nsg",
//	"routeTableName": "winc-test-node-routetable",
//	"primaryAvailabilitySetName": "",
//	"vmType": "",
//	"primaryScaleSetName": "",
//	"cloudProviderBackoff": true,
//	"cloudProviderBackoffRetries": 0,
//	"cloudProviderBackoffExponent": 0,
//	"cloudProviderBackoffDuration": 6,
//	"cloudProviderBackoffJitter": 0,
//	"cloudProviderRateLimit": true,
//	"cloudProviderRateLimitQPS": 6,
//	"cloudProviderRateLimitBucket": 10,
//	"cloudProviderRateLimitQPSWrite": 6,
//	"cloudProviderRateLimitBucketWrite": 10,
//	"useInstanceMetadata": true,
//	"loadBalancerSku": "standard",
//	"excludeMasterFromStandardLB": null,
//	"disableOutboundSNAT": null,
//	"maximumLoadBalancerRuleCount": 0
//}
func TestCloudConfExtraction(t *testing.T) {
	// ignitionContents is the actual worker ignition contents from an azure cluster with dummy credentials and
	// resources
	ignitionContents := `{"ignition":{"config":{},"security":{"tls":{}},"timeouts":{},"version":"2.2.0"},"networkd":{},"passwd":{"users":[{"name":"core","sshAuthorizedKeys":["ssh-rsa dummy"]}]},"storage":{"files":[{"filesystem":"root","path":"/etc/kubernetes/cloud.conf","contents":{"source":"data:,%7B%0A%09%22cloud%22%3A%20%22AzurePublicCloud%22%2C%0A%09%22tenantId%22%3A%20%221234a1b2-a1bc-123a-123a-ab1c2de3afgh%22%2C%0A%09%22aadClientId%22%3A%20%22%22%2C%0A%09%22aadClientSecret%22%3A%20%22%22%2C%0A%09%22aadClientCertPath%22%3A%20%22%22%2C%0A%09%22aadClientCertPassword%22%3A%20%22%22%2C%0A%09%22useManagedIdentityExtension%22%3A%20true%2C%0A%09%22userAssignedIdentityID%22%3A%20%22%22%2C%0A%09%22subscriptionId%22%3A%20%221a123456-12ab-123a-1234-abc1d1ab01c0%22%2C%0A%09%22resourceGroup%22%3A%20%22winc-test-vnet%22%2C%0A%09%22location%22%3A%20%22centralus%22%2C%0A%09%22vnetName%22%3A%20%22winc-test-vnet%22%2C%0A%09%22vnetResourceGroup%22%3A%20%22winc-test-rg%22%2C%0A%09%22subnetName%22%3A%20%22winc-test-node-subnet%22%2C%0A%09%22securityGroupName%22%3A%20%22winc-test-node-nsg%22%2C%0A%09%22routeTableName%22%3A%20%22winc-test-node-routetable%22%2C%0A%09%22primaryAvailabilitySetName%22%3A%20%22%22%2C%0A%09%22vmType%22%3A%20%22%22%2C%0A%09%22primaryScaleSetName%22%3A%20%22%22%2C%0A%09%22cloudProviderBackoff%22%3A%20true%2C%0A%09%22cloudProviderBackoffRetries%22%3A%200%2C%0A%09%22cloudProviderBackoffExponent%22%3A%200%2C%0A%09%22cloudProviderBackoffDuration%22%3A%206%2C%0A%09%22cloudProviderBackoffJitter%22%3A%200%2C%0A%09%22cloudProviderRateLimit%22%3A%20true%2C%0A%09%22cloudProviderRateLimitQPS%22%3A%206%2C%0A%09%22cloudProviderRateLimitBucket%22%3A%2010%2C%0A%09%22cloudProviderRateLimitQPSWrite%22%3A%206%2C%0A%09%22cloudProviderRateLimitBucketWrite%22%3A%2010%2C%0A%09%22useInstanceMetadata%22%3A%20true%2C%0A%09%22loadBalancerSku%22%3A%20%22standard%22%2C%0A%09%22excludeMasterFromStandardLB%22%3A%20null%2C%0A%09%22disableOutboundSNAT%22%3A%20null%2C%0A%09%22maximumLoadBalancerRuleCount%22%3A%200%0A%7D","verification":{}},"mode":420}]},"systemd":{"units":[{"contents":"[Unit]\nDescription=Kubernetes Kubelet\nWants=rpc-statd.service crio.service\nAfter=crio.service\n\n[Service]\nType=notify\nExecStartPre=/bin/mkdir --parents /etc/kubernetes/manifests\nExecStartPre=/bin/rm -f /var/lib/kubelet/cpu_manager_state\nEnvironmentFile=/etc/os-release\nEnvironmentFile=-/etc/kubernetes/kubelet-workaround\nEnvironmentFile=-/etc/kubernetes/kubelet-env\n\nExecStart=/usr/bin/hyperkube \\\n    kubelet \\\n      --config=/etc/kubernetes/kubelet.conf \\\n      --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \\\n      --kubeconfig=/var/lib/kubelet/kubeconfig \\\n      --container-runtime=remote \\\n      --container-runtime-endpoint=/var/run/crio/crio.sock \\\n      --node-labels=node-role.kubernetes.io/worker,node.openshift.io/os_id=${ID} \\\n      --minimum-container-ttl-duration=6m0s \\\n      --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \\\n      --cloud-provider=azure \\\n      --cloud-config=/etc/kubernetes/cloud.conf \\\n      --v=3\n\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n","enabled":true,"name":"kubelet.service"}]}}`

	// Create a temp directory with wmcb prefix
	dir, err := ioutil.TempDir("", "wmcb")
	require.NoError(t, err, "error creating temp directory")
	// Ignore the return error as there is not much we can do if the temporary directory is not deleted
	defer os.RemoveAll(dir)

	wnb := winNodeBootstrapper{
		installDir:  dir,
		kubeletArgs: make(map[string]string),
	}

	err = wnb.parseIgnitionFileContents([]byte(ignitionContents), map[string]fileTranslation{})
	assert.NoError(t, err, "error parsing ignition file contents")
	assert.FileExists(t, filepath.Join(dir, "cloud.conf"), "cloud.conf was not created")

	confContents, err := ioutil.ReadFile(filepath.Join(dir, "cloud.conf"))
	assert.NoError(t, err, "error reading cloud.conf")

	conf := string(confContents)
	// Check if the file beings with { and ends with }
	assert.True(t, strings.HasPrefix(conf, "{"))
	assert.True(t, strings.HasSuffix(conf, "}"))

	// Replace the beginning {\n\t, \n}, with ""
	conf = strings.Replace(conf, "{\n\t", "", -1)
	conf = strings.Replace(conf, "\n}", "", -1)

	// Split the conf items into an array. Each element will now contain "key: value"
	confItems := strings.Split(conf, ",\n\t")

	// Expected key value pairs from ignitionContents
	confExpected := map[string]string{
		"cloud":             "AzurePublicCloud",
		"tenantId":          "1234a1b2-a1bc-123a-123a-ab1c2de3afgh",
		"subscriptionId":    "1a123456-12ab-123a-1234-abc1d1ab01c0",
		"resourceGroup":     "winc-test-rg",
		"location":          "centralus",
		"vnetName":          "winc-test-vnet",
		"vnetResourceGroup": "winc-test-rg",
		"subnetName":        "winc-test-node-subnet",
		"securityGroupName": "winc-test-node-nsg",
		"routeTableName":    "winc-test-node-routetable",
	}

	for _, confItem := range confItems {
		// keyValue will have two elements, 0 being the key and 1 the value
		keyValue := strings.Split(confItem, ":")
		assert.True(t, len(keyValue) == 2)

		// Check if the key needs to be compared
		value, present := confExpected[keyValue[0]]
		if !present {
			continue
		}

		// Assert that the key value from the file matches the value in the ignition contents
		assert.Equal(t, confExpected[keyValue[0]], value)
	}

	// Check that the --cloud-conf option value is present in the kubelet args and matches tempdir + /cloud.conf
	cloudConfigOptValue, present := wnb.kubeletArgs["cloud-config"]
	assert.True(t, present, "cloud-config option is not present in kubelet args")
	assert.Equal(t, filepath.Join(dir, "cloud.conf"), cloudConfigOptValue,
		"unexpected --cloud-config value %s", cloudConfigOptValue)
	assert.Contains(t, cloudConfigOptValue, string(os.PathSeparator), "Path not correctly set for cloud-config")
}

// TestCloudConfNotPresent tests that parseIgnitionFileContents will only create a cloud.conf file and add the
// "--cloud-config" option to the kubelet args, if the cloud.conf file is present in the ignition file.
func TestCloudConfNotPresent(t *testing.T) {
	// ignitionContents is the actual worker ignition contents from an azure cluster with dummy credentials and
	// resources
	ignitionContents := `{"ignition":{"config":{},"security":{"tls":{}},"timeouts":{},"version":"2.2.0"},"networkd":{},"passwd":{"users":[{"name":"core","sshAuthorizedKeys":["ssh-rsa dummy"]}]},"storage":{"files":[]},"systemd":{"units":[{"contents":"[Unit]\nDescription=Kubernetes Kubelet\nWants=rpc-statd.service crio.service\nAfter=crio.service\n\n[Service]\nType=notify\nExecStartPre=/bin/mkdir --parents /etc/kubernetes/manifests\nExecStartPre=/bin/rm -f /var/lib/kubelet/cpu_manager_state\nEnvironmentFile=/etc/os-release\nEnvironmentFile=-/etc/kubernetes/kubelet-workaround\nEnvironmentFile=-/etc/kubernetes/kubelet-env\n\nExecStart=/usr/bin/hyperkube \\\n    kubelet \\\n      --config=/etc/kubernetes/kubelet.conf \\\n      --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \\\n      --kubeconfig=/var/lib/kubelet/kubeconfig \\\n      --container-runtime=remote \\\n      --container-runtime-endpoint=/var/run/crio/crio.sock \\\n      --node-labels=node-role.kubernetes.io/worker,node.openshift.io/os_id=${ID} \\\n      --minimum-container-ttl-duration=6m0s \\\n      --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \\\n      --cloud-provider=aws \\\n      --v=3\n\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n","enabled":true,"name":"kubelet.service"}]}}`

	// Create a temp directory with wmcb prefix
	dir, err := ioutil.TempDir("", "wmcb")
	require.NoError(t, err, "error creating temp directory")
	// Ignore the return error as there is not much we can do if the temporary directory is not deleted
	defer os.RemoveAll(dir)

	wnb := winNodeBootstrapper{
		installDir:  dir,
		kubeletArgs: make(map[string]string),
	}

	err = wnb.parseIgnitionFileContents([]byte(ignitionContents), map[string]fileTranslation{})
	assert.NoError(t, err, "error parsing ignition file contents")

	_, err = os.Stat(filepath.Join(dir, "cloud.conf"))
	assert.Error(t, err, "cloud.conf was created")

	// Check that the --cloud-conf option value is not present in the kubelet args
	_, present := wnb.kubeletArgs["cloud-config"]
	assert.False(t, present, "cloud-config option is not present in kubelet args")
}

// TestCloudConfInvalidNames tests that an error is thrown when an ignition file has an invalid "--cloud-config"
// kubelet argument
func TestCloudConfInvalidNames(t *testing.T) {
	// ignitionContents is the actual worker ignition contents from an azure cluster with dummy credentials and
	// resources. The "--cloud-config=/" option is incorrect here.
	ignitionContents := `{"ignition":{"config":{},"security":{"tls":{}},"timeouts":{},"version":"2.2.0"},"networkd":{},"passwd":{"users":[{"name":"core","sshAuthorizedKeys":["ssh-rsa dummy"]}]},"storage":{"files":[{"filesystem":"root","path":"/etc/kubernetes/cloud.conf","contents":{"source":"data:,not needed","verification":{}},"mode":420}]},"systemd":{"units":[{"contents":"[Unit]\nDescription=Kubernetes Kubelet\nWants=rpc-statd.service crio.service\nAfter=crio.service\n\n[Service]\nType=notify\nExecStartPre=/bin/mkdir --parents /etc/kubernetes/manifests\nExecStartPre=/bin/rm -f /var/lib/kubelet/cpu_manager_state\nEnvironmentFile=/etc/os-release\nEnvironmentFile=-/etc/kubernetes/kubelet-workaround\nEnvironmentFile=-/etc/kubernetes/kubelet-env\n\nExecStart=/usr/bin/hyperkube \\\n    kubelet \\\n      --config=/etc/kubernetes/kubelet.conf \\\n      --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \\\n      --kubeconfig=/var/lib/kubelet/kubeconfig \\\n      --container-runtime=remote \\\n      --container-runtime-endpoint=/var/run/crio/crio.sock \\\n      --node-labels=node-role.kubernetes.io/worker,node.openshift.io/os_id=${ID} \\\n      --minimum-container-ttl-duration=6m0s \\\n      --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \\\n      --cloud-provider=azure \\\n      --cloud-config=/ \\\n      --v=3\n\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n","enabled":true,"name":"kubelet.service"}]}}`

	wnb := winNodeBootstrapper{
		installDir:  "/",
		kubeletArgs: make(map[string]string),
	}
	err := wnb.parseIgnitionFileContents([]byte(ignitionContents), map[string]fileTranslation{})
	assert.Error(t, err, "error not thrown on encountering invalid --cloud-config option")
}

// TestNewWinNodeBootstrapperWithInvalidCNIInputs tests if NewWinNodeBootstrapper returns the expected error on passing
// invalid CNI inputs
func TestNewWinNodeBootstrapperWithInvalidCNIInputs(t *testing.T) {
	_, err := NewWinNodeBootstrapper("", "", "", "C:\\something", "")
	require.Error(t, err, "no error thrown when cniDir is not empty and cniConfig is empty")
	assert.Contains(t, err.Error(), "both cniDir and cniConfig need to be populated", "incorrect error thrown")

	_, err = NewWinNodeBootstrapper("", "", "", "", "C:\\something")
	require.Error(t, err, "no error thrown when cniDir is empty and cniConfig not empty")
	assert.Contains(t, err.Error(), "both cniDir and cniConfig need to be populated", "incorrect error thrown")
}

// TestWinNodeBootstrapperConfigureWithInvalidInputs tests if Configure returns the expected error when CNI inputs
// are not present
func TestWinNodeBootstrapperConfigureWithInvalidInputs(t *testing.T) {
	wnb, err := NewWinNodeBootstrapper("", "", "", "", "")
	require.NoError(t, err, "error instantiating bootstrapper")
	err = wnb.Configure()
	require.Error(t, err, "no error thrown when Configure is called with no CNI inputs")
	assert.Contains(t, err.Error(), "cannot configure without required plugin inputs")
}

// TestDeconstructKubeletCmd tests deconstructKubeletCmd() with valid and invalid inputs
func TestDeconstructKubeletCmd(t *testing.T) {
	t.Run("nil kubelet command", func(t *testing.T) {
		_, err := deconstructKubeletCmd(nil)
		require.Errorf(t, err, "no error returned on passing nil kubelet command")
		assert.Contains(t, err.Error(), "nil kubelet cmd passed")
	})

	t.Run("command not starting with kubelet.exe", func(t *testing.T) {
		kubeletCmd := "--config=c:\\k\\kubelet.conf"
		_, err := deconstructKubeletCmd(&kubeletCmd)
		require.Errorf(t, err, "no error returned on passing kubelet command not starting with kubelet.exe")
		assert.Contains(t, err.Error(), "kubelet command does not start with kubelet.exe")
	})

	t.Run("expected keys in output map", testDeconstructKubeletCmdExpectedKeyValue)
}

// testDeconstructKubeletCmdExpectedKeyValue tests if deconstructKubeletCmd() returns a map with the expected keys and
// values
func testDeconstructKubeletCmdExpectedKeyValue(t *testing.T) {
	kubeletCmd := "c:\\k\\kubelet.exe --config=c:\\k\\kubelet.conf --windows-service --register-with-taints=os=Windows:NoSchedule"
	kubeletKeyValueArgs, err := deconstructKubeletCmd(&kubeletCmd)
	require.NoError(t, err, "error deconstructing kubelet command %s", kubeletCmd)

	kubeletExe, found := kubeletKeyValueArgs["kubeletexe"]
	assert.True(t, found, "kubeletexe key was not found")
	assert.Equal(t, "c:\\k\\kubelet.exe", kubeletExe)

	standalone, found := kubeletKeyValueArgs["standalone"]
	assert.True(t, found, "standalone key was not found")
	assert.Equal(t, "--windows-service", standalone)

	config, found := kubeletKeyValueArgs["--config"]
	assert.True(t, found, "--config key was not found")
	assert.Equal(t, "c:\\k\\kubelet.conf", config)

	taints, found := kubeletKeyValueArgs["--register-with-taints"]
	assert.True(t, found, "--register-with-taints key was not found")
	assert.Equal(t, "os=Windows:NoSchedule", taints)
}

// TestReconstructKubeletCmd tests reconstructKubeletCmd() with valid and invalid inputs
func TestReconstructKubeletCmd(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		_, err := reconstructKubeletCmd(nil)
		require.Errorf(t, err, "no error returned on passing nil map")
		assert.Contains(t, err.Error(), "nil map passed")
	})

	t.Run("map without kubeletexe key", func(t *testing.T) {
		_, err := reconstructKubeletCmd(map[string]string{"--config": "c:\\k\\kubelet.conf"})
		require.Errorf(t, err, "no error returned on passing without kubeletexe key")
		assert.Contains(t, err.Error(), "kubeletexe key not found in the map")
	})

	t.Run("expected command output", testReconstructKubeletCmdExpectedCmd)
}

// testReconstructKubeletCmdExpectedCmd tests if reconstructKubeletCmd() returns the expected command given a predefined
// input map
func testReconstructKubeletCmdExpectedCmd(t *testing.T) {
	kubeletKeyValueArgs := map[string]string{"kubeletexe": "c:\\k\\kubelet.exe",
		"standalone": "--windows-service --another-arg", "--config": "c:\\k\\kubelet.conf"}
	kubeletCmd, err := reconstructKubeletCmd(kubeletKeyValueArgs)
	require.NoError(t, err, "error reconstructing kubelet command from map %v", kubeletKeyValueArgs)
	assert.Equal(t, "c:\\k\\kubelet.exe --windows-service --another-arg --config=c:\\k\\kubelet.conf",
		kubeletCmd)
}

// TestCNI tests the CNI functions ensureDirIsPresent(), checkCNIInputs(), copyFiles() and updateKubeletArgs()
func TestCNI(t *testing.T) {
	err := initCNITestFramework()
	require.NoError(t, err, "unable to initialize CNI test framework")
	// Ignore the return error as there is not much we can do if the temporary directory is not deleted
	defer os.RemoveAll(cniTest.k8sInstallDir)
	defer os.RemoveAll(cniTest.dir)

	t.Run("checkCNIInputs()", testCheckCNIInputs)
	t.Run("ensureDirIsPresent()", testCNIEnsureDirIsPresent)
	// This can run only after ensureDirIsPresent() test is run
	t.Run("copyFiles()", testCNICopyFiles)
	t.Run("updateKubeletArgs()", testCNIUpdateKubeletArgs)
}

// testCNIEnsureDirIsPresent tests ensureDirIsPresent creates the CNI directory when a valid install directory is passed
func testCNIEnsureDirIsPresent(t *testing.T) {
	err := cniTest.cni.ensureDirIsPresent()
	assert.NoError(t, err, "error creating CNI config directory %s", cniDirName)
	assert.DirExists(t, filepath.Join(cniTest.cni.k8sInstallDir, "cni", "config"), "CNI directory was not created")
}

// testCheckCNIInputs tests if checkCNIInputs returns the expected errors on passing invalid inputs
func testCheckCNIInputs(t *testing.T) {
	t.Run("bad install dir", func(t *testing.T) {
		err := checkCNIInputs("C:\\DoesNotExist", "", "")
		assert.Error(t, err, "no error on passing bad install dir")
		assert.Contains(t, err.Error(), "error accessing install directory", "incorrect error thrown")
	})

	t.Run("bad CNI dir", func(t *testing.T) {
		err := checkCNIInputs(cniTest.k8sInstallDir, "C:\\DoesNotExist", "")
		assert.Error(t, err, "no error on passing bad CNI dir")
		assert.Contains(t, err.Error(), "error accessing CNI dir", "incorrect error thrown")
	})

	// We are using the test config file here instead of creating a new file.
	t.Run("CNI dir as file", func(t *testing.T) {
		err := checkCNIInputs(cniTest.k8sInstallDir, cniTest.config, "")
		assert.Error(t, err, "no error on passing file as CNI dir")
		assert.Contains(t, err.Error(), "CNI dir cannot be a file", "incorrect error thrown")
	})

	t.Run("bad CNI config", func(t *testing.T) {
		err := checkCNIInputs(cniTest.k8sInstallDir, cniTest.dir, "C:\\DoesNotExist.conf")
		assert.Error(t, err, "no error on passing bad CNI config")
		assert.Contains(t, err.Error(), "error accessing CNI config", "incorrect error thrown")
	})

	t.Run("CNI config as directory", func(t *testing.T) {
		err := checkCNIInputs(cniTest.k8sInstallDir, cniTest.dir, cniTest.dir)
		assert.Error(t, err, "no error on passing dir as CNI config")
		assert.Contains(t, err.Error(), "CNI config cannot be a directory", "incorrect error thrown")
	})

	t.Run("no files in CNI directory", func(t *testing.T) {
		emptyCNIDir, err := ioutil.TempDir(cniTest.k8sInstallDir, "cni")
		err = checkCNIInputs(cniTest.k8sInstallDir, emptyCNIDir, cniTest.config)
		assert.Error(t, err, "no error on passing empty CNI dir")
		assert.Contains(t, err.Error(), "no files present", "incorrect error thrown")
	})
}

// testCNICopyFiles tests if copyCNIFiles() copies the CNI input binaries and config to the appropriate install location
func testCNICopyFiles(t *testing.T) {
	err := cniTest.cni.copyFiles()
	assert.NoError(t, err, "unexpected error")
	assert.FileExists(t, filepath.Join(cniTest.cni.k8sInstallDir, "cni", filepath.Base(cniTest.exe)), "CNI exe was not copied")
	assert.FileExists(t, filepath.Join(cniTest.cni.k8sInstallDir, "cni", "config", filepath.Base(cniTest.cni.config)),
		"CNI config file was not copied")
}

// checkKubeletCmd asserts that the CNI arguments were added correctly
func checkKubeletCmd(t *testing.T, kubeletCmd string, cni *cniOptions) {
	assert.True(t, strings.HasPrefix(kubeletCmd, "c:\\k\\kubelet.exe"), "kubelet.exe missing in kubelet args")
	assert.Contains(t, kubeletCmd, " --resolv-conf=\"\"", "--resolv-conf missing in kubelet args")
	assert.Contains(t, kubeletCmd, " --network-plugin=cni", "--network-plugin missing in kubelet args")
	assert.Contains(t, kubeletCmd, " --cni-bin-dir="+cni.binDir, "--cni-bin-dir missing in kubelet args")
	assert.Contains(t, kubeletCmd, " --cni-conf-dir="+cni.confDir, "--cni-conf-dir missing in kubelet args")
	assert.NotContains(t, kubeletCmd, " --cni-conf-dir="+cni.confDir+"cni.conf", "cni.conf present in kubelet args")
}

// testCNIUpdateKubeletArgs tests if updateKubeletArgsForCNI() updates the kubelet arguments correctly
func testCNIUpdateKubeletArgs(t *testing.T) {
	t.Run("kubelet command without CNI arguments", func(t *testing.T) {
		kubeletCmd := "c:\\k\\kubelet.exe --config=c:\\k\\kubelet.conf" +
			"--bootstrap-kubeconfig=c:\\k\\bootstrap-kubeconfig --kubeconfig=c:\\k\\kubeconfig " +
			"--pod-infra-container-image=mcr.microsoft.com/k8s/core/pause:1.2.0 --cert-dir=c:/var/lib/kubelet/pki/ " +
			"--windows-service --logtostderr=false --log-file=c:\\var\\log\\kubelet\\kubelet.log " +
			"--register-with-taints=os=Windows:NoSchedule --cloud-provider=aws --v=3"

		err := cniTest.cni.updateKubeletArgs(&kubeletCmd)
		require.NoError(t, err, "error updating kubelet arguments without CNI arguments")
		checkKubeletCmd(t, kubeletCmd, cniTest.cni)
	})

	t.Run("kubelet command with CNI parameters set to different values", func(t *testing.T) {
		kubeletCmd := "c:\\k\\kubelet.exe --config=c:\\k\\kubelet.conf" +
			"--bootstrap-kubeconfig=c:\\k\\bootstrap-kubeconfig --kubeconfig=c:\\k\\kubeconfig " +
			"--pod-infra-container-image=mcr.microsoft.com/k8s/core/pause:1.2.0 --cert-dir=c:/var/lib/kubelet/pki/ " +
			"--windows-service --logtostderr=false --log-file=c:\\var\\log\\kubelet\\kubelet.log " +
			"--register-with-taints=os=Windows:NoSchedule --cloud-provider=aws --v=3 " +
			"--resolv-conf=d:\\k\\etc\\resolv.conf--network-plugin=xyz --cni-bin-dir=d:\\k\\cni " +
			"--cni-conf-dir=d:\\k\\cni\\config\\cni.conf"

		err := cniTest.cni.updateKubeletArgs(&kubeletCmd)
		require.NoError(t, err, "error updating kubelet arguments with pre-existing CNI arguments")
		checkKubeletCmd(t, kubeletCmd, cniTest.cni)
	})

	t.Run("kubelet command that does not start with kubelet.exe", func(t *testing.T) {
		kubeletCmd := "--config=c:\\k\\kubelet.conf"
		err := cniTest.cni.updateKubeletArgs(&kubeletCmd)
		require.Error(t, err, "no error returned on passing kubelet command starting without kubelet.exe")
		assert.Contains(t, err.Error(), "kubelet command does not start with kubelet.exe")
	})

	t.Run("nil kubelet command", func(t *testing.T) {
		err := cniTest.cni.updateKubeletArgs(nil)
		require.Error(t, err, "no error returned on passing nil kubelet command")
		assert.Contains(t, err.Error(), "nil kubelet cmd passed")
	})
}

// TestKubeletDirectoriesCreation tests if the directories needed for Kubelet are initialized as required
func TestKubeletDirectoriesCreation(t *testing.T) {
	// Create a temp directory with wmcb prefix
	dir, err := ioutil.TempDir("", "wmcb")
	require.NoError(t, err, "error creating temp directory")
	// Ignore the return error as there is not much we can do if the temporary directory is not deleted
	defer os.RemoveAll(dir)
	// podManifestDirectory which has to be created by wmcb.
	podManifestDirectory := filepath.Join(dir, "etc", "kubernetes", "manifests")
	// logDirectory which has to be created by wmcb
	logDirectory := filepath.Join(dir, "log")
	wnb := winNodeBootstrapper{
		installDir:  dir,
		logDir:      logDirectory,
		kubeletArgs: make(map[string]string),
	}
	err = wnb.initializeKubeletFiles()
	assert.NoError(t, err, "error initializing kubelet files")
	assert.DirExists(t, podManifestDirectory, "pod manifest directory was not created")
	assert.DirExists(t, logDirectory, "log directory was not created")
}
