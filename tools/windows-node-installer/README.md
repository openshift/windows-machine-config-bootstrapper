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

The `wni` requires the kubeconfig of the OpenShift cluster, a provider specific credentials file to create and 
destroy a Windows instance on the selected provider. To create an instance, `wni` also 
requires extra information such as the instance type. Some optional flags include directory path to
windows-node-installer.json file and log level display. For more information please 
visit `--help` for any commands or sub-commands.
Available Commands:
  aws         Create and destroy windows instances in aws
  azure       Create and destroy windows instances in azure
  help        Help about any command

## AWS Platform
### Creating a Windows instance:

```bash
./wni aws create --kubeconfig <path to OpenShift cluster>/kubeconfig --credentials <path to aws>/credentials 
--credential-account default --instance-type m5a.large --ssh-key <name of the
existing ssh keypair in aws> --private-key <private key to decrypt the aws instance password.>
```

*Note*: Due to a bug in the Intel 82599 network adapter used in most Intel based instances that causes issues with
overlay networks, we suggest using AMD based instances like `m5a.large`

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


## Azure Platform
### Creating a Windows instance:

We can create an instance using azure create subcommand, Currently all the flags in create subcommand are optional, i.e they don't expect 
the user to fill in the values. One can also provide already created resources such IP & NIC names via `ipName`, `nicName` options towards 
node creation if you don't want the installer to create one. The other arguments are `image-id` and  `instance-type` which deals with OS 
and the size settings for the virtual machine. The `image-id` is a URN which is a combination of Publisher:Offer:Sku:Version ex:`RedHat:RHEL:7.4:latest`. 
Once the instance is created successfully a file will be created under instance name in the `dir` explaining the steps on accessing the instance,
please make sure the `dir` exists. For any reason if it couldn't write the data into a file it writes output into the STDOUT. 
For more info on the details please visit `--help` on azure create subcommand.

Sample Create Command:
```bash
./wni azure create --kubeconfig ~/OpenShift/azure/auth/kubeconfig --image-id MicrosoftWindowsServer:WindowsServer:2019-Datacenter:latest \
--instance-type Standard_D2s_v3 --credentials ~/.azure/osServicePrincipal.json --dir ./windowsnodeinstaller/
```

### Destroy Windows instances:
Sample Delete Command:
```bash
./wni azure destroy --kubeconfig ~/OpenShift/azure/auth/kubeconfig --credentials ~/.azure/osServicePrincipal.json \
--dir ./windowsnodeinstaller/
```


### End to end testing
The e2e test for azure run under the assumption that Windows instance is already created and the instanceId's and
subnetGroupId's are present in the windows-node-installer.json. Currently it tests if the required security groups are
present in the worker subnet and if ansible can execute remote commands on the Windows node.

These tests are eventually supposed to run in CI due to which some environment variables needed be exported before they
are run. In addition windows-node-installer.json and winworker-* files need to be present in the
`tools\windows-node-installer` directory of this repo.

To run the e2e change the directory to the `tools\windows-node-installer` of this repo and run the following commands:
```bash
export ARTIFACT_DIRECTORY=$(pwd)
export KUBECONFIG=<your kubeconfig location>
export AZURE_AUTH_LOCATION=<your osServicePrincipal.json location>
go test -run=TestCreateVM ./test/e2e/... -v
```
