# Windows Machine Config Bootstrapper

Bootstrapper is the entity responsible for bootstrapping a Windows node. The current scope of this component is to
perform an one shot configuration of the Windows node to ensure that it can be become a worker node. Following are the
jobs that the bootstrapper does:
- Parse the worker ignition file to get the bootstrap kubeconfig
- Ensures that the kubelet gets the correct kubelet config
- Run the kubelet as a windows service

This will be remotely invoked from a Ansible script or can be run locally
