package client

import (
	"context"
	"fmt"
	"log"

	"github.com/openshift/api/config/v1"
	clientset "github.com/openshift/client-go/config/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// OpenShift is an Client struct which will be used for all OpenShift related functions to interact with the existing
// Cluster.
type OpenShift struct {
	Client *clientset.Clientset
}

// GetOpenShift creates client for the current OpenShift cluster. If Kubeconfig is provided, it is used to create client,
// otherwise it uses in-cluster config.
func GetOpenShift(kubeConfigPath string) (*OpenShift, error) {
	log.Printf("kubeconfig source: %s", kubeConfigPath)
	var rc *rest.Config
	var err error

	if kubeConfigPath == "" {
		// InClusterConfig uses default service account or service account provided by the pod to obtain config.
		rc, err = rest.InClusterConfig()
	} else {
		rc, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	}
	if err != nil {
		return nil, fmt.Errorf("error creating the config object %v", err)
	}

	oc, err := clientset.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	return &OpenShift{oc}, nil
}

// GetInfrastructureID returns the infrastructure ID of the OpenShift cluster or an error.
func (o *OpenShift) GetInfrastructureID() (string, error) {
	infra, err := o.getInfrastructure()
	if err != nil {
		return "", err
	}
	if infra.Status == (v1.InfrastructureStatus{}) {
		return "", fmt.Errorf("infrastructure status is nil")
	}
	return infra.Status.InfrastructureName, nil
}

// GetCloudProvider returns the Provider details of a given OpenShift client including provider type and region or
// an error.
func (o *OpenShift) GetCloudProvider() (*v1.PlatformStatus, error) {
	infra, err := o.getInfrastructure()
	if err != nil {
		return nil, err
	}
	if infra.Status == (v1.InfrastructureStatus{}) || infra.Status.PlatformStatus == nil {
		return nil, fmt.Errorf("error getting infrastructure status")
	}
	return infra.Status.PlatformStatus, nil
}

// getInfrastructure returns the information of current Infrastructure referred by the OpenShift client or an error.
func (o *OpenShift) getInfrastructure() (*v1.Infrastructure, error) {
	infra, err := o.Client.ConfigV1().Infrastructures().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return infra, nil
}
