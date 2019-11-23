module github.com/openshift/windows-machine-config-operator/tools/ansible/tasks/wsu/test

go 1.12

require (
	github.com/masterzen/winrm v0.0.0-20190308153735-1d17eaf15943
	github.com/openshift/windows-machine-config-operator/tools/windows-node-installer v0.0.0-20191030214023-2d2bc09f61b3
	github.com/stretchr/testify v1.4.0
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)
