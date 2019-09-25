# windows-node-installer
The `windows-node-installer (wni)` is a tool that creates a Windows instance under the same virtual network 
(AWS-VCP, Azure-Vnet, and etc.) used by a given OpenShift cluster running on the selected provider.
The actual configuration on the created Windows instance is done by the 
[WMCB](https://github.com/openshift/windows-machine-config-operator) to ensure that the instance joins the
OpenShift cluster as a Windows worker node.

### Supported Platforms
 
 - AWS
 - Azure
 
### Pre-requisite

 - An existing OpenShift 4.2.x cluster running on a supported platform.
 - A `kubeconfig` file for the OpenShift cluster.
 - A valid credentials file of the supported platform.
 
## Getting Started
Install:
```bash
git clone https://github.com/openshift/windows-machine-config-operator.git
cd windows-machine-config-operator
make build-tools
```

## How to use it

<<<<<<< HEAD
The `wni` requires the kubeconfig of the OpenShift cluster, a provider specific credentials file to create and 
destroy a Windows instance on the selected provider. To create an instance, `wni` also 
requires extra information such as image id and instance type. Some optional flags include directory path to 
windows-node-installer.json file and log level display. For more information please 
visit `--help` for any commands or sub-commands.
=======
Available Commands:
  aws         Takes aws specific resource names from user
  azure       Takes azure specific resource names from user
  help        Help about any command
>>>>>>> Added logging implementation and revisions

### Creating a Windows instance:

```bash
./wni aws create --kubeconfig <path to OpenShift cluster>/kubeconfig --credentials <path to aws>/credentials 
--credential-account default --image-id ami-06a4e829b8bbad61e --instance-type m4.large --ssh-key <name of the 
existing ssh key, ie: libra>
```

The default properties of the created instance are:
 - Instance name <OpenShift cluster\'s infrastructure ID>-windows-worker-\<zone\>-<random 4 characters string>
 - Uses the same virtual network created by the OpenShift installer for the cluster
 - Uses a public subnet within the virtual network
 - Auto-assigned public IP address
 - Attached with a security group for Windows that allows RDP access from user\'s IP address and all traffic within the 
 virtual network
 - Attached with the OpenShift cluster\'s worker security group
 - Associated with the OpenShift cluster's worker IAM profile

The IDs of created instance and security group are saved to the `windows-node-installer.json` file at the current or the
 directory specified in `--dir`.

### Destroying Windows instances:

```bash
./wni aws destroy --kubeconfig <path to OpenShift cluster>/kubeconfig --credentials <path to aws>/credentials 
--credential-account default
```
 
The `wni` destroys all resources (instances and security groups) specified in the `windows-node-installer.json` file. 
Security groups will not be deleted if they are still in-use by other instances.


For example for azure:

```bash
$wni azure --help
Takes azure specific resource names from user

Usage:
  wni azure [command]

Available Commands:
  create      Create a Windows instance on the same provider as the existing OpenShift Cluster.
  destroy     Destroy the Windows instances and resources specified in 'windows-node-installer.json' file.

Flags:
      --credential-account string   account name of a credential used to create the OpenShift Cluster specified in the provider's credentials file. (required)
      --credentials string          file path to the cloud provider credentials of the existing OpenShift cluster (required).
  -h, --help                        help for azure

Global Flags:
      --dir string          directory to save or read window-node-installer.json file from. (default ".")
      --kubeconfig string   file path to the kubeconfig of the existing OpenShift cluster (required).
      --log-level string    log level (e.g. 'info') (default "info")

Use "wni aws [command] --help" for more information about a command.
```

Created instance default properties:
 - Instance name \<OpenShift cluster infrastructure ID\>-windows-worker-\<zone\>-\<random characters string\>
 - Share virtual network with the OpenShift Cluster
 - Public subnet within the virtual network
 - Auto-assigned public IP address
 - Attached security group for Windows (Allow RDP access from user's IP address and all traffic within the virtual 
 network)
 - Attached OpenShift cluster worker security group

The IDs of created instance and security group are saved to the `window-node-installer.json` file at the current or the
 directory specified in `--dir`.

### Destroying Windows instances:

```bash
Usage:
  wni azure destroy [flags]

Flags:
  -h, --help   help for destroy

Global Flags:
      --credential-account string   account name of a credential used to create the OpenShift Cluster specified in 
      the provider's credentials file (required).
      --credentials string          file path to the cloud provider credentials of the existing OpenShift cluster 
      (required).
      --dir string                   directory to save or read window-node-installer.json file from. (default ".")
      --kubeconfig string           file path to the kubeconfig of the existing OpenShift cluster (required).
      --log-level string             log level (e.g. 'info') (default "info")
```
 
The `wni` destroys all resources (instances and security groups) specified in the `window-node-installer.json` file. 
Security groups will not be deleted if they are still in-use by other instances.

