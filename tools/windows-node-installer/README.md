# windows-node-installer
The `windows-node-installer (wni)` is a tool that creates a Windows instance under the same virtual network 
(AWS-VCP, Azure-Vnet, and etc.) used by a given OpenShift cluster running on the selected provider.
The actual configuration on the created Windows instance is done by 
[WMCB](https://github.com/openshift/windows-machine-config-operator) to ensure that the node joins the
OpenShift cluster

### Supported Platforms
 
 - AWS
 
### Pre-requisite

 - An existing OpenShift 4.x cluster running on a supported platform.
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
### Creating a Windows instance:

```bash
wni --help
Creates and destroys Windows instance with an existing OpenShift cluster.

Usage:
  wni [command]

Available Commands:
  aws         Takes aws specific resource names from user
  help        Help about any command

Flags:
      --dir string          directory to save or read window-node-installer.json file from. (default ".")
  -h, --help                help for wni
      --kubeconfig string   file path to the kubeconfig of the existing OpenShift cluster (required).
      --log-level string    log level (e.g. 'info') (default "info")

Use "wni [command] --help" for more information about a command.
```

For example for aws:

```bash
$wni aws --help
Takes aws specific resource names from user

Usage:
  wni aws [command]

Available Commands:
  create      Create a Windows instance on the same provider as the existing OpenShift Cluster.
  destroy     Destroy the Windows instances and resources specified in 'windows-node-installer.json' file.

Flags:
      --credential-account string   account name of a credential used to create the OpenShift Cluster specified in the provider's credentials file. (required)
      --credentials string          file path to the cloud provider credentials of the existing OpenShift cluster (required).
  -h, --help                        help for aws

Global Flags:
      --dir string          directory to save or read window-node-installer.json file from. (default ".")
      --kubeconfig string   file path to the kubeconfig of the existing OpenShift cluster (required).
      --log-level string    log level (e.g. 'info') (default "info")

Use "wni aws [command] --help" for more information about a command.
```

Created instance default properties:
 - Instance name \<OpenShift cluster infrastructure ID\>-windows-worker-\<zone\>-\<random 4 characters string\>
 - Share virtual network with the OpenShift Cluster
 - Public subnet within the virtual network
 - Auto-assigned public IP address
 - Attached security group for Windows (Allow RDP access from user's IP address and all traffic within the virtual 
 network)
 - Attached OpenShift cluster worker IAM role (Profile)
 - Attached OpenShift cluster worker security group

The IDs of created instance and security group are saved to the `window-node-installer.json` file at the current or the
 directory specified in `--dir`.

### Destroying Windows instances:

```bash
Usage:
  wni aws destroy [flags]

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

