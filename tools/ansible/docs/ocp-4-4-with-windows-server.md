# OpenShift Container Platform 4.4 with Microsoft Windows Server 2019/1809 Nodes

**Developer Preview**

## Pre-requisites

This document is tested against a Fedora release 31 Linux host for running all
the Linux commands.

Please note that a command preceded by `>` is to be run in a PowerShell window on
a Windows instance, and a command preceded by `$` is to be run on a Linux console
(localhost).

### Installation of prerequisite packages 

Python3 and pip are required to follow this guide. To check if python3 is
installed on the system, run the following command:

```sh
$ python3 --version
```

To install python3, run the following command:

```sh
$ sudo dnf install python3
```

To check if pip is installed on the system, run the following command:

```sh
$ python3 -m pip --version
pip 19.0.3 from /usr/lib/python3.7/site-packages/pip (python 3.7)
```

To install pip, refer to the [pip installation](https://pip.pypa.io/en/stable/installing/)
guide.

Install the `jq` library to parse JSON files:

```sh
$ sudo dnf install jq
```

Install Git to allow for cloning GitHub repositories:

```sh
$ sudo dnf install git
```

## Bring up the OpenShift cluster with ovn-kubernetes

Download the
[OpenShift 4.4.4 Installer and Client](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.4.4/)

**Note:** Download both the `openshift-install-*` and `openshift-client-*` files.

Unzip the files with the following commands. For example:

```sh
$ tar -xzvf openshift-install-linux-<version>.tar.gz
$ tar -xzvf openshift-client-linux-<version>.tar.gz
```

After extracting the OpenShift client, move the `oc` and `kubectl` binaries to
`/usr/local/bin`:

```sh
$ sudo mv {kubectl,oc} /usr/local/bin
```

### Create the install-config

```sh
$ ./openshift-install create install-config --dir=<cluster_directory>
> SSH Public Key <path-to-your-rsa>/id_rsa.pub
> Platform  <i.e. aws>
> Region <region close by i.e. us-east-1>
> Base Domain <Your Domain>
> Cluster Name  <cluster_name>
> Pull Secret <content of pull-secrets>
```

The [official OpenShift Container Platform documentation](https://docs.openshift.com/container-platform/4.4/installing/installing_azure/installing-azure-account.html)
should be consulted for credentials and other cloud provider-related
instructions.

The previous step results in an `install-config.yaml` file in your current
directory. Edit the `install-config.yaml` to switch `networkType` from
`OpenShiftSDN` to `OVNKubernetes`  inside the cluster directory:

```sh
$ sed -i 's/OpenShiftSDN/OVNKubernetes/g' install-config.yaml
```

### Create manifests

You must enable hybrid networking on the cluster. Therefore, you must create the
manifests:

```sh
$ ./openshift-install create manifests --dir=<cluster_directory>
```

This creates a `manifests` and `openshift` folder in your `<cluster_directory>`.

### Configuring OVNKubernetes on a Hybrid cluster

Inside the `<cluster_directory>`, create a copy of the
`manifests/cluster-network-02-config.yml` file as `manifests/cluster-network-03-config.yml`. 

```sh
$ cp manifests/cluster-network-02-config.yml manifests/cluster-network-03-config.yml
```

Edit the `manifests/cluster-network-03-config.yml` file as shown below:

1. Modify the `api version` to `operator.openshift.io/v1`:

    ```sh
    $ sed -i 's/config.openshift.io\/v1/operator.openshift.io\/v1/g' manifests/cluster-network-03-config.yml
    ```

2. Add the following to the `spec:` section of `manifests/cluster-network-03-config.yml`:

    ```sh
    spec:
      defaultNetwork:
        type: OVNKubernetes
        ovnKubernetesConfig:
          hybridOverlayConfig:
            hybridClusterNetwork:
            - cidr: 10.132.0.0/14
              hostPrefix: 23

    ```

Here is an example of the `manifests/cluster-network-03-config.yml` file:

```yml
apiVersion: operator.openshift.io/v1
kind: Network
metadata:
  creationTimestamp: null
  name: cluster
spec:
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
  externalIP:
    policy: {}
  networkType: OVNKubernetes
  serviceNetwork:
  - 172.30.0.0/16
  defaultNetwork:
    type: OVNKubernetes
    ovnKubernetesConfig:
      hybridOverlayConfig:
        hybridClusterNetwork:
        - cidr: 10.132.0.0/14
          hostPrefix: 23
status: {}
```

**Note:** The `hybridClusterNetwork` CIDR cannot overlap the `clusterNetwork`
CIDR.

### Create the cluster

Execute the following command to create the cluster and wait for it to display:

```sh
$ ./openshift-install create cluster --dir=<cluster_directory>
```

Export the `kubeconfig` so that oc can communicate with the cluster:

```sh
$ export KUBECONFIG=$(pwd)/<cluster_directory>/auth/kubeconfig
```

**Note**: Only using the absolute path is supported.

Make sure you can interact with the cluster: 

```sh
$ oc get nodes
```

```sh
NAME                                        STATUS   ROLES    AGE     VERSION
pmahajan-az-44bwz-master-0                  Ready    master   14h     v1.17.1
pmahajan-az-44bwz-master-1                  Ready    master   14h     v1.17.1
pmahajan-az-44bwz-master-2                  Ready    master   14h     v1.17.1
pmahajan-az-44bwz-worker-centralus1-bc8mr   Ready    worker   13h     v1.17.1
pmahajan-az-44bwz-worker-centralus2-xwr2h   Ready    worker   13h     v1.17.1
pmahajan-az-44bwz-worker-centralus3-mprvk   Ready    worker   13h     v1.17.1
```

### Verify Hybrid networking 
 
The network.operator cluster CR spec should look like the example below:

```sh
$ oc get network.operator cluster -o yaml
```

```yml
...
spec:

  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23  
  defaultNetwork:
    ovnKubernetesConfig:
      hybridOverlayConfig:
        hybridClusterNetwork:
        - cidr: 10.132.0.0/14
          hostPrefix: 23
    type: OVNKubernetes
  serviceNetwork:
  - 172.30.0.0/16
status: {}
...
```

## Bring up the Windows node

Launch a *Windows 2019 Server Datacenter with Containers* instance.

Note the public IP required for creating the host file in the
[Setup Ansible connection](#setup-ansible-connection) step.

You can now [setup Ansible](#setup-ansible-connection) before moving on to the
next steps.

#### Setup Ansible connection

Now you can use Ansible to configure the Windows host. On the Linux host, install
`ansible` and `pywinrm`, as well as `selinux-python` bindings:

**Note:**  This step assumes that python3 is installed on the system. Ansible
will not work without python. 

```sh
$ sudo dnf install python3-libselinux
$ pip install ansible==2.9 pywinrm selinux --user
```

Create a hosts file with the following information:

```
[win]

<public_ip> ansible_password=<password> private_ip=<private_ip>

[win:vars]
ansible_user=<username>
cluster_address=<cluster_address>
ansible_connection=winrm
ansible_ssh_port=5986
ansible_winrm_server_cert_validation=ignore
```

`<cluster_address>` is the cluster endpoint. It is the combination of your cluster
name and the base domain for your cluster.

```sh
$ oc cluster-info | head -n1 | sed 's/.*\/\/api.//g'| sed 's/:.*//g'
```

Provide the IPv4 public IP as `<public_ip>`.

![](./images/gen-public-ip.png)

`<private_ip>` is the private IP of the node.

`<username>` and `<password>` are the login credentials for the Windows
instance. Note the username must have administrative privileges.

Here is an example hosts file:

```
[win]
40.69.185.26 ansible_password='mypassword’ private_ip=10.0.32.7

[win:vars]
ansible_user=core
cluster_address=winc-cluster.winc.azure.devcluster.openshift.com
ansible_connection=winrm
ansible_ssh_port=5986
ansible_winrm_server_cert_validation=ignore
```

Test if Ansible is able to communicate with the Windows instance with the
following command:

```sh
$ ansible win -i <name_of_the_hosts_file> -m win_ping -v
```

**Note:** If you do not want to provide the password in the hosts file, you can
provide the same as an extra variable to any Ansible command. For example, the
above command could be executed as:

```sh
$ ansible win -i <name_of_the_hosts_file> -m win_ping -v --extra-vars "ansible_password=<password>"
```

### Bootstrap the windows node

On a Linux host, run the Ansible Playbook that transfers the necessary files
onto the Windows instance and bootstraps it so that it can join the cluster as a
worker node.

**Note:** Playbook assumes you have `jq` installed. Your active RDP connection
might be disrupted during the execution of the playbook.

Clone the GitHub repository to download the Ansible playbook and all the required
dependencies:

```sh
$ git clone https://github.com/openshift/windows-machine-config-bootstrapper.git
$ git fetch && git checkout release-4.4
```

Run the Ansible playbook to bootstrap the Windows worker node. Make sure you
have at least 6GB of free space in the `/tmp` directory.

```sh
$ ansible-playbook -i <path_to_hosts_file> windows-machine-config-bootstrapper/tools/ansible/tasks/wsu/main.yaml -v
```

```
$ oc get nodes -l kubernetes.io/os=windows
```

You can now see the Windows instance has joined the cluster

```sh
NAME                                        STATUS   ROLES    AGE     VERSION
winworker-obm7a                             Ready    worker   2m11s   v1.17.1
```

#### API rate limit exceeded error when running WSU

The WSU playbook uses GitHub API to fetch releases for WMCB. You might encounter an
API rate limit exceeded error while running the WSU playbook in `TASK [Get release]`
and `TASK [Get latest 0.8.x cni plugins version]`. The issue occurs due to GitHub
rate-limiting unauthenticated requests at 60 requests per hour. As a workaround,
wait for the rate-limit to reset (at most 1 hour) before running the playbook
again.

## Test Windows workload

You can now create a pod that can be deployed on a Windows instance. Here is an
example [WebServer](https://gist.githubusercontent.com/suhanime/683ee7b5a2f55c11e3a26a4223170582/raw/d893db98944bf615fccfe73e6e4fb19549a362a5/WinWebServer.yaml)
deployment to create a pod:

**Note:** Given the size of Windows images, it is recommended to pull the Docker
image `mcr.microsoft.com/windows/servercore:ltsc2019` on the instance first,
before creating the pods. 

On the Windows instance, run the following command in a PowerShell window:

```pwsh
> docker pull mcr.microsoft.com/windows/servercore:ltsc2019
```

**Note:** Refer to the [cloud provider instructions](#cloud-provider-instructions)
to set up and RDP into your Windows node.

On the Linux host, deploy the pods:

```sh
$ oc create -f https://gist.githubusercontent.com/suhanime/683ee7b5a2f55c11e3a26a4223170582/raw/d893db98944bf615fccfe73e6e4fb19549a362a5/WinWebServer.yaml -n default
```

Once the deployment has been created, check the status of the pods:

```sh
$ oc get pods -n default 
NAME                             READY   STATUS    RESTARTS   AGE
win-webserver-6f5bdc5b95-x65tq   1/1     Running   0          14m
```

We have created a service of
[LoadBalancer](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer)
type:

```sh
$ oc get service win-webserver -n default 
NAME            TYPE           CLUSTER-IP    EXTERNAL-IP   PORT(S)        AGE
win-webserver   LoadBalancer   172.30.0.31  20.185.74.192  80:31412/TCP   17m
```

```sh
$ curl 20.185.74.192:80
<html><body><H1>Windows Container Web Server</H1><p>IP 10.132.1.2 callerCount 4 </body></html>
```

### Deploying in a namespace other than default

To deploy into a different namespace, SCC must be disabled in that
namespace. This should never be used in production, and any namespace that this
has been done to should not be used to run Linux pods.

To skip SCC for a namespace, the label `openshift.io/run-level = 1` should be
applied to the namespace. This will apply to both Linux and Windows pods, and
thus Linux pods should not be deployed into this namespace.

For example, to create a new project and apply the label, run the following
commands:

```sh
$ oc new-project <project_name>
$ oc label namespace <project_name> "openshift.io/run-level=1"
```

## Cloud provider instructions

Refer here for steps on [Azure](./azure/azure-with-windows-server.md) and
[AWS](./aws/aws-with-windows-server.md).
