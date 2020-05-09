module github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer

go 1.13

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200205145930-e9d93e317dd1 // OpenShift 4.3
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20191125132246-f6563a70e19a // OpenShift 4.3
	k8s.io/api => k8s.io/api v0.16.7
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.7
	k8s.io/client-go => k8s.io/client-go v0.16.7
)

require (
	github.com/Azure/azure-sdk-for-go v34.1.0+incompatible
	github.com/Azure/go-autorest/autorest v0.9.2
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.0
	github.com/Azure/go-autorest/autorest/to v0.3.0
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/aws/aws-sdk-go v1.23.2
	github.com/coreos/etcd v3.3.10+incompatible
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/masterzen/winrm v0.0.0-20190308153735-1d17eaf15943
	github.com/openshift/api v0.0.0-00010101000000-000000000000
	github.com/openshift/client-go v0.0.0-00010101000000-000000000000
	github.com/pkg/sftp v1.11.0
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20191011191535-87dc89f01550
	golang.org/x/net v0.0.0-20191209160850-c0dbc17a3553 // indirect
	k8s.io/api v0.16.7
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.0.0-00010101000000-000000000000
)
