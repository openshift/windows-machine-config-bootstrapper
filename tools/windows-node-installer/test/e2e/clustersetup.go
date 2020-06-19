package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	authscheme "github.com/openshift/client-go/authorization/clientset/versioned/scheme"
	clientset "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/client"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacclient "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/auth"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// configDir is a directory containing all yaml files for the resources
	configDir = "deploy"
	// projectName represents namespace where the resources should be created
	projectName = "windows-node-installer"
	// resourceName represents name of the resources to be created
	resourceName = "windows-node-installer"
)

// configFiles contains name of the resource files
var configFiles = [...]string{"namespace.yaml", "role.yaml", "clusterole.yaml", "service_account.yaml", "cluster_role_binding.yaml", "role_binding.yaml"}

// setRbacConfig creates namespace, role, clusterole, rolebinding cluster role binding and service account on the
// cluster. It returns error if any of the resource creation fails.
func setRbacConfig(cfg *rest.Config) error {
	rbacConfig, err := rbacclient.NewForConfig(cfg)
	if err != nil {
		return err
	}
	core, _ := coreclient.NewForConfig(cfg)

	for _, f := range configFiles {

		bytes, err := ioutil.ReadFile(configDir + "/" + f)
		if err != nil {
			return fmt.Errorf("error reading the yaml file for the resource %v", err)
		}
		// UniversalDeserializer converts the yaml file format into Go objects recognized by k8s go-client
		decode := authscheme.Codecs.UniversalDeserializer().Decode

		obj, _, err := decode(bytes, nil, nil)

		if err != nil {
			return fmt.Errorf("error decoding YAML objects: %v", err)
		}

		// get the
		switch o := obj.(type) {

		case *corev1.Namespace:
			// get the required namespace
			_, err := core.Namespaces().Get(context.TODO(), projectName, metav1.GetOptions{})
			if err != nil {
				// create namespace
				_, err := core.Namespaces().Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating  namespace: %v", err)
				}
			}
			log.Print("Namespace Created")

		case *rbac.ClusterRole:
			// get the required cluster role
			_, err := rbacConfig.ClusterRoles().Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				// create cluster role
				_, err := rbacConfig.ClusterRoles().Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating cluster role: %v", err)
				}
				log.Print("Cluster Role Created")
			}

		case *rbac.Role:
			// get the required role
			_, err := rbacConfig.Roles(projectName).Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				// create role in the namespace
				_, err := rbacConfig.Roles(projectName).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating  role: %v", err)
				}
				log.Print("Role Created")
			}

		case *corev1.ServiceAccount:
			// get the required service account
			_, err := core.ServiceAccounts(projectName).Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				// create service account
				_, err := core.ServiceAccounts(projectName).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating  role: %v", err)
				}
				log.Print("Service Account Created")
			}

		case *rbac.RoleBinding:
			// get the required role binding
			_, err := rbacConfig.RoleBindings(projectName).Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				// create role binding
				_, err := rbacConfig.RoleBindings(projectName).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating  role binding %v", err)
				}
				log.Print("Role Binding created")
			}
		case *rbac.ClusterRoleBinding:
			// get the required cluster role binding
			_, err := rbacConfig.ClusterRoleBindings().Get(context.TODO(), resourceName, metav1.GetOptions{})
			if err != nil {
				// create cluster role binding
				_, err := rbacConfig.ClusterRoleBindings().Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("error creating  cluster role binding %v", err)
				}
				log.Print("Cluster Role Binding created")
			}
		default:
			return fmt.Errorf("invalid resource file")

		}
	}
	return nil
}

// getServiceAccountToken returns the service account token value.
func getServiceAccountToken(cfg *rest.Config) (string, error) {
	core, err := coreclient.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("error getting config for core resources: %v", err)
	}
	// get service account
	sa, err := core.ServiceAccounts(projectName).Get(context.TODO(), resourceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting the service account : %v", err)
	}
	var tokenSecret = ""
	// get secret token for the service account
	for _, secret := range sa.Secrets {
		if strings.Contains(secret.Name, resourceName+"-token") {
			tokenSecret = secret.Name
		}
	}
	// get secret object using secret name value
	secret, err := core.Secrets(projectName).Get(context.TODO(), tokenSecret, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting secret for the service account %v", err)
	}
	serviceAccountTokenBytes := secret.Data["token"]
	// decode service account token
	saToken, err := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString(serviceAccountTokenBytes))
	if err != nil {
		return "", fmt.Errorf("error while decoding the service account token %v", err)
	}
	return string(saToken), nil
}

// getOpenShiftClientForSA returns client specific to the service account created in the test cluster. It uses service
// account token to update existing config to use service account permissions.
func getOpenShiftClientForSA(kubeconfig string) (*client.OpenShift, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("error creating config object %v", err)
	}
	// Create roles, role bindings and service account
	if err := setRbacConfig(cfg); err != nil {
		return nil, fmt.Errorf("error creating resources : %v", err)
	}
	// get serviceAccountToken value
	serviceAccountToken, err := getServiceAccountToken(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting the service account token: %v", err)
	}
	// Info object is used to define service account user
	info := auth.Info{BearerToken: serviceAccountToken}
	// MergeWithConfig creates config with the defined info object
	updatedConfig, err := info.MergeWithConfig(*cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating config from service account %v ", err)
	}
	log.Print("config updated to use service account permissions")

	oc, err := clientset.NewForConfig(&updatedConfig)
	if err != nil {
		return nil, fmt.Errorf("error getting OpenShift client %v", err)
	}
	return &client.OpenShift{Client: oc}, nil
}

// deleteResources destroys all resources created during test time. It returns error if any of the resources fail to
// delete.
func deleteResources(kubeconfig string) error {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("error creating config object %v", err)
	}
	rbacConfig, err := rbacclient.NewForConfig(cfg)
	if err != nil {
		return err
	}
	core, _ := coreclient.NewForConfig(cfg)

	// delete role binding
	if err := rbacConfig.RoleBindings(projectName).Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting role binding")
	}
	// delete cluster role binding
	if err := rbacConfig.ClusterRoleBindings().Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting cluster role binding")
	}
	// delete service account
	if err := core.ServiceAccounts(projectName).Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting service account")
	}
	// delete roles
	if err := rbacConfig.Roles(projectName).Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting role")
	}
	// delete cluster role
	if err := rbacConfig.ClusterRoles().Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting cluster roles")
	}
	// delete namespace
	if err := core.Namespaces().Delete(context.TODO(), resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error deleting namespace")
	}
	log.Print("successfully deleted all resources")
	return nil

}
