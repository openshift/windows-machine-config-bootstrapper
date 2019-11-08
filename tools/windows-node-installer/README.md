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
requires extra information such as image id and instance type. Some optional flags include directory path to 
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
--credential-account default --image-id ami-06a4e829b8bbad61e --instance-type m4.large --ssh-key <name of the 
existing ssh key, ie: libra> --private-key <private key to decrypt the aws instance password>
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
The e2e test for azure run under the assumption that Windows instance is already created and the instanceId's,
subnetGroupId's are present in the windows-node-installer.json file in `--dir`. Currently it tests if `winRM` port is opened on the
worker subnet and can ansible can execute remote commands on the Windows node.

To run the e2e do:
```bash
export PACKAGE=github.com/openshift/windows-machine-config-operator
export TOOLS_DIR=$PACKAGE/tools/windows-node-installer
go test -run=TestWinRMSetup $TOOLS_DIR/test/e2e/... -v
```