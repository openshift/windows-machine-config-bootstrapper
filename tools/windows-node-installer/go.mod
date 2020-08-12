module github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer

go 1.14

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200422081840-fdd1b0c14c88 // OpenShift 4.5
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200422192633-6f6c07fc2a70 // OpenShift 4.5
	k8s.io/api => k8s.io/api v0.18.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.2
	k8s.io/client-go => k8s.io/client-go v0.18.2
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
	github.com/openshift/api v0.0.0-20200422081840-fdd1b0c14c88
	github.com/openshift/client-go v0.0.0-20200422192633-6f6c07fc2a70
	github.com/pkg/errors v0.8.1
	github.com/pkg/sftp v1.11.0
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20200220183623-bac4c82f6975
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v0.18.2
)
