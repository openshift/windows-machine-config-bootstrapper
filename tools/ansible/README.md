## Windows Scale Up (WSU) Playbook
This playbook is responsible for setting up your Windows instance so it is ready and can be added to your OpenShift cluster.
There are couple prerequisites for using this playbook.
- Set up a running OpenShift cluster.
- An ansible environment to run the Ansible playbook with required packages. Here are a sample step to set up your Ansible environment.
  Here are the steps to set up the Ansible Envrionment on a Fedora system:
  ```
  $ sudo dnf install libselinux-python
  $ pip install ansible pywinrm kubernetes openshift
  ```
- A Windows Server 2019 instance running in the same VPC as the OpenShift cluster with winrm enabled.
  You can use `wni` tool to setup the Windows instance following this [link](https://github.com/openshift/windows-machine-config-operator/tree/master/tools/windows-node-installer). The winrm module is enabled automatically if you use `wni` tool to create the server node. But this `wni` is currently not supported in production use cases.
- You need to add a tag to your Windows Server 2019 instance, with the `key: <cluster_name>` and `value: owned` before your run the playbook. 
- Login to your cluster using `oc` command.

Once you have your Windows instance setup, a `hosts` file is need to run this Ansible playbook (the `hosts` file can be on any directory of your system) with your instance information. You can create a new `hosts` file or modify any existing `hosts` file on your system. 
Here is a sample `hosts` file. Inside the sample `hosts` field, `<username>` and `<password>` are the account information to login to the Windows instance, `<node_ip>` is the public ip address of your Windows instance. This file can be in the same directory with your playbook.
```
[win]
<node_ip>
[win:vars]
ansible_user=<username>
ansible_password=<password>
ansible_connection=winrm
ansible_ssh_port=5986
ansible_winrm_server_cert_validation=ignore
```
Confirm that you are able to ping your Windows instance using the following command (this command can be run anywhere):
```
# replace <host file location> with the hosts file contains your Windows instance information.
$ ansible win -i <host file location> -m win_ping -vvvvv
```
If you are able to ping the Windows instance, you are ready to use `wsu.yaml` playbook to configure your Windows instance and let it join your cluster.
These are the environment variables for executing the wsu playbook. All the environment variable are **required**.
- `ansible_password` if this is not given in the hosts file
- `kubelet_location`: kubelet binary download link
- `wmcb_location`: wmcb binary download link
- `openshift_node_bootstrap_server`: cluster server namespace for fetching the ignition file

Here is a sample command to execute this ansible playbook.
```
$ ansible-playbook -vvvv -i hosts wsu.yaml --extra-vars "ansible_password=<decrypted_password> kubelet_location=<kubelet location> wmcb_location=<wmcb_location> openshift_node_bootstrap_server=<openshift_node_bootstrap_server>"
```
Just replace the argument in arrow bracket with corresponding information in your system, and this ansible playbook will prepare your Windows instance.
Once you have done that follow these [steps](https://github.com/openshift/windows-machine-config-operator#testing) to verify your node is joined.

