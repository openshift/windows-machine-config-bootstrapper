module github.com/openshift/windows-machine-config-bootstrapper

go 1.14

// Replace is used to pin a specific version of a package or to point to sub-go.mod directories.
// Use 'replace' to point to the sub-go.mod directory for building a binary in the root directory and always build by
// package instead of file.
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190313235455-40a48860b5ab // kubernetes-1.14.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190313205120-d7deff9243b1 // kubernetes-1.14.0
)

require (
	github.com/ajeddeloh/go-json v0.0.0-20170920214419-6a2fe990e083 // indirect
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e // indirect
	github.com/coreos/ignition v0.33.0
	github.com/go-bindata/go-bindata/v3 v3.1.3
	github.com/go-logr/zapr v0.1.0
	github.com/gogo/protobuf v1.2.1 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.3.0
	github.com/vincent-petithory/dataurl v0.0.0-20160330182126-9a301d65acbb
	go.uber.org/atomic v1.4.0 // indirect
	go.uber.org/zap v1.10.0
	go4.org v0.0.0-20190919214946-0cfe6e5be80f // indirect
	golang.org/x/sys v0.0.0-20190403152447-81d4e9dc473e
	k8s.io/api v0.0.0-20190923155552-eac758366a00 // indirect
	k8s.io/apimachinery v0.0.0-20190923155427-ec87dd743e08 // indirect
	sigs.k8s.io/controller-runtime v0.2.1
)
