package client

import (
	"fmt"

	v1 "github.com/openshift/api/config/v1"
	clientset "github.com/openshift/client-go/config/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	logger "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// log is the global logger for the client package.
// Each log record produced by this logger will have an identifier containing `client` tag.
var log = logger.Log.WithName("client")

// OpenShift is an Client struct which will be used for all OpenShift related functions to interact with the existing
// Cluster.
type OpenShift struct {
	Client *clientset.Clientset
}

// GetOpenShift uses kubeconfig to create a client for existing OpenShift cluster and returns it or an error.
func GetOpenShift(kubeConfigPath string) (*OpenShift, error) {
	log.Info("kubeconfig source: ", kubeConfigPath)
	rc, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		return nil, err
	}
	oc, err := clientset.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	return &OpenShift{oc}, nil
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
	infra, err := o.Client.ConfigV1().Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return infra, nil
}
