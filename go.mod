module github.com/openshift/windows-machine-config-operator

go 1.12

// Replace is used to pin a specific version of a package or to point to sub-go.mod directories.
// Use 'replace' to point to the sub-go.mod directory for building a binary in the root directory and always build by
// package instead of file.
replace (
	github.com/openshift/windows-machine-config-operator/tools/windows-node-installer => ./tools/windows-node-installer
	k8s.io/api => k8s.io/api v0.0.0-20190313235455-40a48860b5ab // kubernetes-1.14.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190313205120-d7deff9243b1 // kubernetes-1.14.0
)

require (
	github.com/Azure/azure-sdk-for-go v33.4.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.9.1 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/ajeddeloh/go-json v0.0.0-20170920214419-6a2fe990e083 // indirect
	github.com/coreos/ignition v0.33.0
	github.com/go-logr/zapr v0.1.0
	github.com/google/pprof v0.0.0-20190908185732-236ed259b199 // indirect
	github.com/ianlancetaylor/demangle v0.0.0-20181102032728-5e5cf60278f6 // indirect
	github.com/openshift/windows-machine-config-operator/tools/windows-node-installer v0.0.0-00010101000000-000000000000 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.3.0
	github.com/vincent-petithory/dataurl v0.0.0-20160330182126-9a301d65acbb
	go.uber.org/zap v1.10.0
	go4.org v0.0.0-20190919214946-0cfe6e5be80f // indirect
	golang.org/x/sys v0.0.0-20190403152447-81d4e9dc473e
	k8s.io/apimachinery v0.0.0-20190923155427-ec87dd743e08
	k8s.io/kubelet v0.0.0-20190923161547-13146ddde0d1
	sigs.k8s.io/controller-runtime v0.2.1
)
