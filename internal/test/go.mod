module github.com/openshift/windows-machine-config-bootstrapper/internal/test

go 1.12

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200205145930-e9d93e317dd1 // OpenShift 4.3
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20191125132246-f6563a70e19a // OpenShift 4.3
	k8s.io/api => k8s.io/api v0.16.7
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.7
	k8s.io/client-go => k8s.io/client-go v0.16.7
)

require (
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/protobuf v1.3.3 // indirect
	github.com/google/go-github/v29 v29.0.2
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/json-iterator/go v1.1.9 // indirect
	github.com/openshift/client-go v0.0.0-00010101000000-000000000000
	github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer v0.0.0-20200521181434-c9423833fb65
	github.com/pkg/sftp v1.11.0
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20191122220453-ac88ee75c92c // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	k8s.io/api v0.16.7
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20200124190032-861946025e34 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)
