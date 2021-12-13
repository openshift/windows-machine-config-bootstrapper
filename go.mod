module github.com/openshift/windows-machine-config-bootstrapper

go 1.16

// Replace is used to pin a specific version of a package or to point to sub-go.mod directories.
// Use 'replace' to point to the sub-go.mod directory for building a binary in the root directory and always build by
// package instead of file.
replace (
	k8s.io/api => k8s.io/api v0.20.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.20.0
)

require (
	github.com/aws/aws-sdk-go-v2/config v1.10.0
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.8.0
	github.com/coreos/ign-converter v0.0.0-20200825151652-ea20012f9844
	github.com/coreos/ignition v0.35.0
	github.com/coreos/ignition/v2 v2.6.0
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/vincent-petithory/dataurl v0.0.0-20160330182126-9a301d65acbb
	golang.org/x/sys v0.0.0-20201112073958-5cba982894dd
	k8s.io/apimachinery v0.20.0
	sigs.k8s.io/controller-runtime v0.7.0
)
