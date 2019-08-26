# winc-vm-setup V1
Set up windows node and ready to connect to a running OpenShift cluster.

## pre-requisite
- An existing openshift cluster running on AWS.
- AWS EC2 credentials (aws_access_key_id and aws_access_key_id)
- kubeconfig of the existing OpenShift Cluster

## What it does
create windows container node (win server 2019) under the same vpc as OpenShift Cluster
```bash
./winc-winc-setup create -h
Usage of create:
  -awsaccount string
    	account name of the aws credentials (default "openshift-dev")
  -awscred string
    	Required: absolute path of aws credentials
  -dir string
    	path to 'winc-setup.json'. (default "./")
  -imageid string
    	Set instance AMI ID tobe deployed. AWS windows server 2019 is ami-04ca2d0801450d495. (default "ami-0943eb2c39917fc11")
  -instancetype string
    	Set instance type tobe deployed. Free tier is t2.micro. (default "m4.large")
  -keyname string
    	Set key.pem for accessing the instance. (default "libra")
  -kubeconfig string
    	Required: absolute path to the kubeconfig file
  -region string
    	Set region where the instance will be running on aws (default "us-east-1")
```

1. grab openshift Cluster vpc name 
2. Windows Node properties:
    - Node Name \<Openshift Cluster Name\>-winNode
    - A m4.large instance
    - Shared vpc with OpenShift Cluster
    - Public Subnet (within the vpc)
    - Auto-assign Public IP
    - Using shared libra key
    - security group (secure public IP RDP with my IP and 10.x/16)
    - Attach IAM role (Openshift Cluster Worker Profile)
    - Attach Security Group (Openshift Cluster - Worker)
3. Output a way to rdp inside of Windows node
```bash
./winc-winc-setup destroy -h
Usage of destroy:
  -awsaccount string
    	account name of the aws credentials (default "openshift-dev")
  -awscred string
    	Required: absolute path of aws credentials
  -dir string
    	path to 'winc-setup.json'. (default "./")
  -region string
    	Set region where the instance will be running on aws (default "us-east-1")
```
1. destroy VM
2. delete security group

## Getting Started
Install:
```bash
git clone https://github.com/openshift/windows-machine-config-operator.git
cd windows-machine-config-operator/tools/winc-winc-setup
export GO111MODULE=on
go build .
```
Create a windows node:
```bash
./winc-vm-setup create -awscred=/abs/path/to/your/aws/credentials -kubeconfig=/abs/path/to/your/kubeconfig
```
Destroy the windows nodes created:
```bash
./winc-winc-setup destroy -awscred=/abs/path/to/your/aws/credentials
```
## V2 (future work) 
1. Ansible
    - firewall
    - powershell
