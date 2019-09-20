module github.com/openshift/windows-machine-config-operator/tools/windows-node-installer

go 1.12

replace (
	k8s.io/api => k8s.io/api v0.0.0-20190313235455-40a48860b5ab // kubernetes-1.14.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190313205120-d7deff9243b1 // kubernetes-1.14.0
	k8s.io/client-go => k8s.io/client-go v11.0.0+incompatible // v11.0.0
)

require (
	github.com/Azure/azure-sdk-for-go v33.2.0+incompatible
	github.com/Azure/go-autorest/autorest v0.9.1
	github.com/Azure/go-autorest/autorest/azure/auth v0.3.0
	github.com/Azure/go-autorest/autorest/to v0.3.0
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/aws/aws-sdk-go v1.23.2
	github.com/coreos/etcd v3.3.10+incompatible
	github.com/deckarep/golang-set v1.7.1 // indirect
	github.com/go-logr/zapr v0.1.0
	github.com/golangci/golangci-lint v1.17.1 // indirect
	github.com/google/pprof v0.0.0-20190908185732-236ed259b199 // indirect
	github.com/googleapis/gnostic v0.3.1 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/ianlancetaylor/demangle v0.0.0-20181102032728-5e5cf60278f6 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/openshift/api v3.9.1-0.20190814194116-a94e914914f4+incompatible
	github.com/openshift/client-go v0.0.0-20190813201236-5a5508328169
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.4.0 // indirect
	github.com/stretchr/testify v1.3.0
	go.uber.org/zap v1.10.0
	golang.org/x/arch v0.0.0-20190909030613-46d78d1859ac // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	k8s.io/api v0.0.0-20190830074751-c43c3e1d5a79 // indirect
	k8s.io/apimachinery v0.0.0-20190830034626-e709f673dfd9
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/utils v0.0.0-20190809000727-6c36bc71fc4a // indirect
	sigs.k8s.io/controller-runtime v0.2.0
)
