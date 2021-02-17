package bootstrapper

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// svcPollInterval is the is the interval at which we poll the kubelet service
	svcPollInterval = 30 * time.Second
	// svcRunTimeout is the maximum duration to wait for the kubelet service to go to running state
	svcRunTimeout = 2 * time.Minute
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

	if len(k.dependents) == 0 {
		return nil
	}
	for _, dependent := range k.dependents {
		err := startService(dependent)
		if err != nil {
			return fmt.Errorf("failed to start dependent service %s", dependent.Name)
		}
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

// stop ensures that the kubelet service and its dependent services are stopped,
// the list of dependent services is static and contains one level of dependencies
func (k *kubeletService) stop() error {
	isServiceRunning, err := k.isRunning()
	if err != nil {
		return fmt.Errorf("unable to check if kubelet service is running: %v", err)
	}
	if !isServiceRunning {
		return nil
	}
	// the list of dependents is static here and contains one level of dependencies
	if len(k.dependents) != 0 {
		for _, dependent := range k.dependents {
			if err := stopService(dependent); err != nil {
				return fmt.Errorf("failed to stop dependent service %s", dependent.Name)
			}
		}
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
	// Wait for service to go to Running state
	err := wait.Poll(svcPollInterval, svcRunTimeout, func() (done bool, err error) {
		isKubeletRunning, err := k.isRunning()
		if err != nil {
			return false, nil
		}
		return isKubeletRunning, nil
	})
	if err != nil {
		return fmt.Errorf("error running kubelet service %v", err)
	}

	return nil
}

// isRunning returns true if the kubelet service is running
func (k *kubeletService) isRunning() (bool, error) {
	status, err := k.obj.Query()
	if err != nil {
		return false, err
	}
	return status.State == svc.Running, nil
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

// stopAndRemove stops and removes the kubelet service
func (k *kubeletService) stopAndRemove() error {
	if k.obj == nil {
		return nil
	}
	if err := k.stop(); err != nil {
		return fmt.Errorf("failed to stop kubelet service: %v", err)
	}
	return k.obj.Delete()
}

// startService is a helper to start a given service
func startService(serviceObj *mgr.Service) error {
	if serviceObj == nil {
		return fmt.Errorf("service object should not be nil")
	}
	isServiceRunning, err := isServiceRunning(serviceObj)
	if err != nil {
		return fmt.Errorf("unable to check if service is running: %v", err)
	}
	if !isServiceRunning {
		err := serviceObj.Start()
		if err != nil {
			return err
		}
	}
	return nil
}

// controlService is a helper to send control signal to a given service
func controlService(serviceObj *mgr.Service, cmd svc.Cmd, desiredState svc.State) error {
	if serviceObj == nil {
		return fmt.Errorf("service object should not be nil")
	}
	status, err := serviceObj.Control(cmd)
	if err != nil {
		return err
	}
	// Most of the rest of the function borrowed from https://godoc.org/golang.org/x/sys/windows/svc/mgr#Service.Control
	// Arbitrary service wait time of 20 seconds
	timeout := time.Now().Add(serviceWaitTime)
	for status.State != desiredState {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", desiredState)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = serviceObj.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}
	return nil
}

// stopService is a helper to stop a given service
func stopService(serviceObj *mgr.Service) error {
	if serviceObj == nil {
		return fmt.Errorf("service object should not be nil")
	}
	isServiceRunning, err := isServiceRunning(serviceObj)
	if err != nil {
		return fmt.Errorf("unable to check if service is running: %v", err)
	}
	if isServiceRunning {
		err := controlService(serviceObj, svc.Stop, svc.Stopped)
		if err != nil {
			return fmt.Errorf("unable to stop %s service", serviceObj.Name)
		}
	}
	return nil
}

// isServiceRunning returns true if the given service is running
func isServiceRunning(serviceObj *mgr.Service) (bool, error) {
	if serviceObj == nil {
		return false, fmt.Errorf("service object should not be nil")
	}
	status, err := serviceObj.Query()
	if err != nil {
		return false, err
	}
	return status.State == svc.Running, nil
}
