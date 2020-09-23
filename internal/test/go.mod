module github.com/openshift/windows-machine-config-bootstrapper/internal/test

go 1.14

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200901182017-7ac89ba6b971 // OpenShift 4.6
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200827190008-3062137373b5 // OpenShift 4.6
	k8s.io/api => k8s.io/api v0.19.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.0
	k8s.io/client-go => k8s.io/client-go v0.19.0
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200520125206-5e266b553d8e // This is coming from machine-api repo
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.6.1-0.20200902144306-f2d4ad78c7ab
)

require (
	github.com/aws/aws-sdk-go v1.23.2
	github.com/google/go-github/v29 v29.0.2
	github.com/openshift/api v0.0.0-20200901182017-7ac89ba6b971
	github.com/openshift/client-go v0.0.0-20200827190008-3062137373b5
	github.com/openshift/machine-api-operator v0.2.1-0.20200520080344-fe76daf636f4
	github.com/pkg/errors v0.9.1
	github.com/pkg/sftp v1.11.0
	github.com/stretchr/testify v1.5.1
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	k8s.io/api v0.19.0
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v0.19.0
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.6.2
)
