# Ansible Playbooks

## Prerequisites for using these playbooks
- Access to an OpenShift cluster
- A Windows Server 2019 instance within the cluster's subnet, with WinRM [configured](https://docs.ansible.com/ansible/latest/user_guide/windows_setup.html#winrm-setup) for ansible
    - You can use the [wni](https://github.com/openshift/windows-machine-config-operator/tree/master/tools/windows-node-installer) tool to create a ready Windows instance on certain cloud providers
    - If you are using a cloud provider, you may have to add a tag to the Windows instance.
      It will be of the format `key:kubernetes.io/cluster/<infraID>` with `value: owned`.
      - You can find the infraID in the Ignition config file metadata `metadata.json`
- Ansible 2.9 and pywinrm installed, and selinux bindings exist on the system
- [oc](https://docs.openshift.com/container-platform/4.2/cli_reference/openshift_cli/getting-started-cli.html) is installed
- The KUBECONFIG environment variable is set to the cluster's kubeconfig location
```
sudo dnf install libselinux-python
pip install selinux ansible==2.9 pywinrm
```
- A `hosts` file with the required variables defined. See below for an example:
```
[win]
# Address of a node and its Windows password
<node_ip> ansible_password=<password>

[win:vars]
# Windows username, typically 'Administrator'
ansible_user=<username>
# Address of the OpenShift cluster e.g. "foo.fah.com". This should not include "https://api-" or a port
cluster_address=<address>

ansible_connection=winrm
ansible_ssh_port=5986
# Required if you do not wish to set up a certificate
#ansible_winrm_server_cert_validation=ignore
```
Confirm that you are able to connect your Windows instance with ansible by using the following command:
```
$ ansible win -i hosts -m win_ping -v
```


## Windows Scale Up (WSU) Playbook
This playbook is responsible for three things:
- Preparing a Windows instance for joining an OpenShift cluster.
- Running the WMCB
- Ensuring the Windows node has successfully joined the cluster

### Usage
Run the WSU playbook:
```
$ ansible-playbook -i hosts tasks/wsu/main.yaml -v
```
