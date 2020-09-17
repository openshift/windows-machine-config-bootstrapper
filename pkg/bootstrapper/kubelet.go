package bootstrapper

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// kubeletService struct contains the kubelet specific service information
type kubeletService struct {
	// obj is a pointer to the Windows service object
	obj *mgr.Service
	// dependents contains a list of services dependent on the current service
	dependents []*mgr.Service
}

// newKubeletService creates and returns a new kubeletService object
func newKubeletService(ksvc *mgr.Service, dependents []*mgr.Service) (*kubeletService, error) {
	if ksvc == nil {
		return nil, fmt.Errorf("service object should not be nil")
	}
	return &kubeletService{
		obj:        ksvc,
		dependents: dependents,
	}, nil
}

// config retrieves service config from service object Config()
func (k *kubeletService) config() (mgr.Config, error) {
	config, err := k.obj.Config()
	if err != nil {
		return mgr.Config{}, err
	}
	return config, nil
}

// start ensures that the kubelet service is running
func (k *kubeletService) start() error {
	if k.obj == nil {
		return fmt.Errorf("no kubelet service found")
	}
	// if the service is running we do not need to start it
	isServiceRunning, err := k.isRunning()
	if err != nil {
		return fmt.Errorf("unable to check if Windows service is running: %v", err)
	}
	if isServiceRunning {
		return nil
	}
	if err := k.obj.Start(); err != nil {
		return err
	}
	return nil
}

// control sends a signal to the service and waits until it changes state in response to the signal
func (k *kubeletService) control(cmd svc.Cmd, desiredState svc.State) error {
	status, err := k.obj.Control(cmd)
	if err != nil {
		return err
	}
	// Most of the rest of the function borrowed from https://godoc.org/golang.org/x/sys/windows/svc/mgr#Service.Control
	timeout := time.Now().Add(serviceWaitTime)
	for status.State != desiredState {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", desiredState)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = k.obj.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}
	return nil
}

// stop ensures that the kubelet service is stopped
func (k *kubeletService) stop() error {
	isServiceRunning, err := k.isRunning()
	if err != nil {
		return fmt.Errorf("unable to check if kubelet service is running: %v", err)
	}
	if !isServiceRunning {
		return nil
	}

	if err := k.control(svc.Stop, svc.Stopped); err != nil {
		return fmt.Errorf("unable to stop Windows Service %s", KubeletServiceName)
	}

	return nil
}

// refresh updates the kubelet service with the given config and restarts the service
func (k *kubeletService) refresh(config mgr.Config) error {
	if err := k.stop(); err != nil {
		return fmt.Errorf("error stopping kubelet service: %v", err)
	}

	if err := k.obj.UpdateConfig(config); err != nil {
		return fmt.Errorf("error updating kubelet service: %v", err)
	}

	if err := k.start(); err != nil {
		return fmt.Errorf("error starting kubelet service: %v", err)
	}

	return nil
}

// remove deletes the kubelet service via the Windows service API
func (k *kubeletService) remove() error {
	if k.obj == nil {
		return nil
	}
	return k.obj.Delete()
}

// isRunning returns true if the kubelet service is running
func (k *kubeletService) isRunning() (bool, error) {
	status, err := k.obj.Query()
	if err != nil {
		return false, err
	}
	return status.State == svc.Running, nil
}

// stopAndRemove stops and removes the kubelet service
func (k *kubeletService) stopAndRemove() error {
	if k.obj == nil {
		return nil
	}
	k.stop()
	return k.remove()
}

// disconnect removes all connections to the Windows service svcMgr api, and allows services to be deleted
func (k *kubeletService) disconnect() error {
	if k.obj == nil {
		return nil
	}
	err := k.obj.Close()
	if err != nil {
		return err
	}
	return nil
}

// setRecoveryActions sets the recovery actions for service on a failure
func (k *kubeletService) setRecoveryActions() error {
	if k.obj == nil {
		return fmt.Errorf("kubelet service object should not be nil")
	}
	err := k.obj.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5},
	}, 600)
	if err != nil {
		return err
	}
	return nil
}
