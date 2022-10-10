package framework

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	mapi "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/credentials"
	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/providers"
	"github.com/openshift/windows-machine-config-bootstrapper/internal/test/windows"
)

const (
	// sshKey is the key that will be used to access created Windows VMs
	sshKey = "openshift-dev"
)

// cloudProvider holds the information related to cloud provider
// TODO: Move this to proper location which can destroy the VM that got created.
// https://issues.redhat.com/browse/WINC-245
var cloudProvider providers.CloudProvider

// TestWindowsVM is the interface for interacting with a Windows VM in the test framework. This will hold the
// specialized information related to test suite
type TestWindowsVM interface {
	// RetrieveDirectories recursively copies the files and directories from the directory in the remote Windows VM
	// to the given directory on the local host.
	RetrieveDirectories(string, string) error
	// Compose the Windows VM we have from MachineSets
	windows.WindowsVM
}

// createMachineSet() gets the generated MachineSet configuration from cloudprovider package and creates a MachineSet
func (f *TestFramework) createMachineSet() error {
	cloudProvider, err := providers.NewCloudProvider(sshKey)
	if err != nil {
		return fmt.Errorf("error instantiating cloud provider %v", err)
	}
	machineSet, err := cloudProvider.GenerateMachineSet(true, 1)
	if err != nil {
		return fmt.Errorf("error generating Windows MachineSet: %v", err)
	}
	log.Print("Creating Machine Sets")
	_, err = f.machineClient.MachineSets("openshift-machine-api").Create(context.TODO(), machineSet, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating MachineSet %v", err)
	}
	f.machineSet = machineSet
	log.Printf("Created Machine Set %v", machineSet.Name)
	return nil
}

// getWindowsMachines() waits until all the machines required are in Provisioned state. It returns an array of all
// the machines created. All the machines are created concurrently.
func (f *TestFramework) getWindowsMachines(vmCount int, skipVMSetup bool) ([]mapi.Machine, error) {
	log.Print("Waiting for Machines...")
	windowsOSLabel := "machine.openshift.io/os-id"
	var provisionedMachines []mapi.Machine
	// it takes approximately 12 minutes in the CI for all the machines to appear.
	timeOut := 12 * time.Minute
	if skipVMSetup {
		timeOut = 1 * time.Minute
	}
	startTime := time.Now()
	for i := 0; time.Since(startTime) <= timeOut; i++ {
		allMachines := &mapi.MachineList{}
		allMachines, err := f.machineClient.Machines("openshift-machine-api").List(context.TODO(), metav1.ListOptions{LabelSelector: windowsOSLabel})
		if err != nil {
			return nil, fmt.Errorf("failed to list machines: %v", err)
		}
		provisionedMachines = []mapi.Machine{}

		phaseProvisioned := "Provisioned"

		for _, machine := range allMachines.Items {
			instanceStatus := machine.Status
			if instanceStatus.Phase != nil && *instanceStatus.Phase == phaseProvisioned {
				provisionedMachines = append(provisionedMachines, machine)
			}
		}
		time.Sleep(5 * time.Second)
	}
	if skipVMSetup {
		if vmCount == 0 {
			return nil, fmt.Errorf("no Windows VMs found")
		}
		return provisionedMachines, nil
	}
	if vmCount == len(provisionedMachines) {
		return provisionedMachines, nil
	}
	return nil, fmt.Errorf("expected VM count %d but got %d", vmCount, len(provisionedMachines))
}

// newWindowsMachineSet creates and sets up Windows VMs in the cloud and returns the WindowsVM interface that can be used to
// interact with the VM. If no error is returned then it is guaranteed that the VM was
// created and can be interacted with.
func (f *TestFramework) newWindowsMachineSet(vmCount int, skipVMSetup bool) ([]TestWindowsVM, error) {
	w := make([]TestWindowsVM, vmCount)
	if skipVMSetup {
		log.Print("Skip VM setup option selected. Not setting up the VMs...")
	} else {
		err := f.createMachineSet()
		if err != nil {
			return nil, fmt.Errorf("error creating Windows MachineSet: %v", err)
		}
	}

	provisionedMachines, err := f.getWindowsMachines(vmCount, skipVMSetup)
	if err != nil {
		log.Print("unable to provision MachineSets. Trying to delete created MachineSets")
		if skipVMSetup {
			log.Print("skipped VM setup while creation. Not destroying the MachineSets...")
		} else {
			msErr := f.DestroyMachineSet()
			if msErr != nil {
				log.Printf("destroying MachineSets failed: %v", msErr)
			}
		}
		return nil, err
	}

	for i, machine := range provisionedMachines {
		winVM := &windows.Windows{}

		ipAddress := ""
		for _, address := range machine.Status.Addresses {
			if address.Type == core.NodeInternalIP {
				ipAddress = address.Address
			}
		}
		if len(ipAddress) == 0 {
			return nil, fmt.Errorf("no associated internal ip for machine: %s", machine.Name)
		}

		// Get the instance ID associated with the Windows machine.
		providerID := *machine.Spec.ProviderID
		if len(providerID) == 0 {
			return nil, fmt.Errorf("no provider id associated with machine")
		}
		// Ex: aws:///us-east-1e/i-078285fdadccb2eaa. We always want the last entry which is the instanceID
		providerTokens := strings.Split(providerID, "/")
		instanceID := providerTokens[len(providerTokens)-1]
		if len(instanceID) == 0 {
			return nil, fmt.Errorf("empty instance id in provider id")
		}
		creds := credentials.NewCredentials(instanceID, ipAddress, credentials.Username)
		winVM.Credentials = creds
		log.Print("setting up ssh")
		log.Print("using the mounted private key to access the VMs through ssh")
		winVM.Credentials.SetSSHKey(f.Signer)
		if err := winVM.GetSSHClient(); err != nil {
			return nil, fmt.Errorf("unable to get ssh client for vm %s : %v", instanceID, err)
		}
		w[i] = winVM
	}
	return w, nil
}

// DestroyMachineSet() deletes the MachineSet which in turn deletes all the Machines created by the MachineSet
func (f *TestFramework) DestroyMachineSet() error {
	log.Print("Destroying MachineSets")
	if f.machineSet == nil {
		log.Print("unable to find MachineSet to be deleted, was skip VM setup option selected ?")
		log.Print("MachineSets/Machines needs to be deleted manually \nNot deleting MachineSets...")
		return nil
	}
	err := f.machineClient.MachineSets("openshift-machine-api").Delete(context.TODO(), f.machineSet.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("unable to delete MachineSet %v", err)
	}
	log.Print("MachineSets Destroyed")
	return nil
}
