package e2e

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/masterzen/winrm"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/cloudprovider"
	"github.com/openshift/windows-machine-config-operator/tools/windows-node-installer/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// Get kubeconfig, AWS credentials, and artifact dir from environment variable set by the OpenShift CI operator.
	kubeconfig     = os.Getenv("KUBECONFIG")
	awsCredentials = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	dir            = os.Getenv("ARTIFACT_DIR")
	privateKeyPath = os.Getenv("KUBE_SSH_KEY_PATH")

	// Path of the WSU playbook
	playbookPath = os.Getenv("WSU_PATH")
	// clusterAddress is the address of the OpenShift cluster e.g. "foo.fah.com".
	// This should not include "https://api-" or a port
	clusterAddress = os.Getenv("CLUSTER_ADDR")

	// The CI-operator uses AWS region `us-east-1` which has the corresponding image ID: ami-0b8d82dea356226d3 for
	// Microsoft Windows Server 2019 Base with Containers.
	imageID      = "ami-0b8d82dea356226d3"
	instanceType = "m4.large"
	sshKey       = "libra"

	// Cloud provider factory that we will use in these tests
	cloud cloudprovider.Cloud
	// Credentials for a spun up instance
	createdInstanceCreds *types.Credentials
	// Temp directory ansible created on the windows host
	ansibleTempDir = ""
	// kubernetes-node-windows-amd64.tar.gz SHA512
	// Value from https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG-1.16.md#node-binaries-1
	// This value should be updated when we change the kubelet version in WSU
	expectedKubeTarSha = "a88e7a1c6f72ea6073dbb4ddfe2e7c8bd37c9a56d94a33823f531e303a9915e7a844ac5880097724e44dfa7f4" +
		"a9659d14b79cc46e2067f6b13e6df3f3f1b0f64"
)

// createAWSWindowsInstance creates a windows instance and populates the "cloud" and "createdInstanceCreds" global
// variables
func createAWSWindowsInstance() error {
	var err error
	cloud, err = cloudprovider.CloudProviderFactory(kubeconfig, awsCredentials, "default", dir,
		imageID, instanceType, sshKey, privateKeyPath)
	if err != nil {
		return fmt.Errorf("could not setup cloud provider: %s", err)
	}
	createdInstanceCreds, err = cloud.CreateWindowsVM()
	if err != nil {
		return fmt.Errorf("could not create windows VM: %s", err)
	}
	return nil
}

// createhostFile creates an ansible host file and returns the path of it
func createHostFile(ip, password string) (string, error) {
	hostFile, err := ioutil.TempFile("", "testWSU")
	if err != nil {
		return "", fmt.Errorf("coud not make temporary file: %s", err)
	}
	defer hostFile.Close()

	_, err = hostFile.WriteString(fmt.Sprintf(`[win]
%s ansible_password='%s'

[win:vars]
ansible_user=Administrator
cluster_address=%s
ansible_port=5986
ansible_connection=winrm
ansible_winrm_server_cert_validation=ignore`, ip, password, clusterAddress))
	return hostFile.Name(), err
}

// TestWSU creates a Windows instance, runs the WSU, and then runs a series of tests to ensure all expected
// behavior was achieved. The following environment variables must be set for this test to run: KUBECONFIG,
// AWS_SHARED_CREDENTIALS_FILE, ARTIFACT_DIR, KUBE_SSH_KEY_PATH, WSU_PATH, CLUSTER_ADDR
func TestWSU(t *testing.T) {
	require.NotEmptyf(t, kubeconfig, "KUBECONFIG environment variable not set")
	require.NotEmptyf(t, awsCredentials, "AWS_SHARED_CREDENTIALS_FILE environment variable not set")
	require.NotEmptyf(t, dir, "ARTIFACT_DIR environment variable not set")
	require.NotEmptyf(t, privateKeyPath, "KUBE_SSH_KEY_PATH environment variable not set")
	require.NotEmptyf(t, playbookPath, "WSU_PATH environment variable not set")
	require.NotEmptyf(t, clusterAddress, "CLUSTER_ADDR environment variable not set")

	// TODO: Check if other cloud provider credentials are available
	if awsCredentials == "" {
		t.Fatal("No cloud provider credentials available")
	}
	err := createAWSWindowsInstance()
	require.NoErrorf(t, err, "Error spinning up Windows VM: %s", err)
	require.NotNil(t, createdInstanceCreds, "Instance credentials are not set")
	defer cloud.DestroyWindowsVMs()
	// In order to run the ansible playbook we create an inventory file:
	// https://docs.ansible.com/ansible/latest/user_guide/intro_inventory.html
	hostFilePath, err := createHostFile(createdInstanceCreds.GetIPAddress(), createdInstanceCreds.GetPassword())
	require.NoErrorf(t, err, "Could not write to host file: %s", err)
	cmd := exec.Command("ansible-playbook", "-vvv", "-i", hostFilePath, playbookPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "WSU playbook returned error: %s, with output: %s", err, string(out))

	// Ansible will copy files to a temporary directory with a path such as:
	// C:\\Users\\Administrator\\AppData\\Local\\Temp\\ansible.z5wa1pc5.vhn\\
	initialSplit := strings.Split(string(out), "C:\\\\Users\\\\Administrator\\\\AppData\\\\Local\\\\Temp\\\\ansible.")
	require.True(t, len(initialSplit) > 1, "Could not find Windows temp dir: %s", out)
	ansibleTempDir = "C:\\Users\\Administrator\\AppData\\Local\\Temp\\ansible." + strings.Split(initialSplit[1], "\"")[0]

	t.Run("Files copied to Windows node", testFilesCopied)
	// TODO: Once the WSU starts the WMCB and adds the node to the cluster, add check to see if the node is "Ready"
}

// testFilesCopied tests that the files we attempted to copy to the Windows host, exist on the Windows host
func testFilesCopied(t *testing.T) {
	expectedFileList := []string{"kubelet.exe", "worker.ign", "wmcb.exe", "kube.tar.gz"}
	endpoint := winrm.NewEndpoint(createdInstanceCreds.GetIPAddress(), 5986, true, true,
		nil, nil, nil, 0)
	client, err := winrm.NewClient(endpoint, "Administrator", createdInstanceCreds.GetPassword())
	require.NoErrorf(t, err, "Could not create winrm client: %s", err)

	// Check if each of the files we expect on the Windows host are there
	for _, filename := range expectedFileList {
		fullPath := ansibleTempDir + "\\" + filename
		// This command will write to stdout, only if the file we are looking for does not exist
		command := fmt.Sprintf("if not exist %s echo fail", fullPath)
		stdout := new(bytes.Buffer)
		_, err := client.Run(command, stdout, os.Stderr)
		assert.NoError(t, err, "Error looking for %s: %s", fullPath, err)
		assert.Emptyf(t, stdout.String(), "Missing file: %s", fullPath)
	}

	// Check the SHA of kube.tar.gz downloaded
	kubeTarPath := ansibleTempDir + "\\" + "kube.tar.gz"
	// certutil is part of default OS installation Windows 7+
	command := fmt.Sprintf("certutil -hashfile %s SHA512", kubeTarPath)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	_, err = client.Run(command, stdout, stderr)
	require.NoError(t, err, "Error generating SHA512 for %s", kubeTarPath)
	require.Equalf(t, stderr.Len(), 0, "Error generating SHA512 for %s", kubeTarPath)
	// CertUtil output example:
	// SHA512 hash of <filepath>:\r\n<SHA-output>\r\nCertUtil: -hashfile command completed successfully.
	// Extracting SHA value from the output
	actualKubeTarSha := strings.Split(stdout.String(), "\r\n")[1]
	assert.Equal(t, expectedKubeTarSha, actualKubeTarSha,
		"kube.tar.gz downloaded does not match expected checksum")
}
