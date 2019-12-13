module github.com/openshift/windows-machine-config-operator/internal/test

go 1.12

replace (
	k8s.io/api => k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go => k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)

require (
	github.com/masterzen/winrm v0.0.0-20190308153735-1d17eaf15943
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/openshift/windows-machine-config-operator/tools/windows-node-installer v0.0.0-20191123092711-1eb1f9837741
	github.com/pkg/sftp v1.10.1
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20191122220453-ac88ee75c92c
	k8s.io/api v0.0.0-20190918155943-95b840bb6a1f
	k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)
