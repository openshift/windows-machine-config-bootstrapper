# Logging on Windows instance

These playbooks install Fluentd on Windows Instance, in order to enable logging support.

Before running,
- Set up an OpenShift cluster.
- Set up a Windows instance as part of the cluster. You can use `wni` tool to set it up by following this [link](https://github.com/openshift/windows-machine-config-bootstrapper/tree/master/tools/windows-node-installer).
- Ensure Ansible environment is set up to run the playbook.
- Ensure a valid inventory file is set up.

Run `main.yml` with the following command:
```
$ ansible-playbook -v -i <inventory_file> main.yml --extra-vars "ruby_fetch_url=<ruby_fetch_url>"
```
As a part of Fluentd installation, Ruby DevKit is required.
Here, `<ruby_fetch_url>` is the URL to fetch Ruby DevKit. The version of the same should be >=2.4 and < 2.6
The URL can be found [here](https://rubyinstaller.org/downloads/).

Sample command with URL:
```
$ ansible-playbook -v -i hosts main.yml --extra-vars "ruby_fetch_url=
'https://github.com/oneclick/rubyinstaller2/releases/download/RubyInstaller-2.5.7-1/rubyinstaller-devkit-2.5.7-1-x64.exe'"
```

There are also some optional arguments which could be provided while running the playbook:
- `install_dir` is the directory where you would like Ruby to be installed.
This path should already exist on the system.
- `fluentd_version` is the version of Fluentd to be installed. `1.5.1` is recommended version for
[OpenShift 4.3 release](https://github.com/openshift/origin-aggregated-logging/tree/master/fluentd)

The run command with optional arguments could look like:
```
$ ansible-playbook -v -i <inventory_file> main.yml --extra-vars "ruby_fetch_url=<ruby_fetch_url>
install_dir=<install_dir_path> fluentd_version=<fluentd_version>"
```
