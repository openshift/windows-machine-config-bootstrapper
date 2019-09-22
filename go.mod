module github.com/openshift/windows-machine-config-operator

go 1.12

// Replace is used to pin a specific version of a package or to point to sub-go.mod directories.
// Use 'replace' to point to the sub-go.mod directory for building a binary in the root directory and always build by
// package instead of file.
replace github.com/openshift/windows-machine-config-operator/tools/windows-node-installer => ./tools/windows-node-installer

require (
	github.com/go-logr/zapr v0.1.0
	github.com/google/pprof v0.0.0-20190908185732-236ed259b199 // indirect
	github.com/ianlancetaylor/demangle v0.0.0-20181102032728-5e5cf60278f6 // indirect
	github.com/openshift/windows-machine-config-operator/tools/windows-node-installer v0.0.0-00010101000000-000000000000 // indirect
	github.com/spf13/cobra v0.0.5
	go.uber.org/zap v1.10.0
	golang.org/x/arch v0.0.0-20190909030613-46d78d1859ac // indirect
	golang.org/x/tools v0.0.0-20190916201440-b31ee645dd40 // indirect
	sigs.k8s.io/controller-runtime v0.2.1
)
