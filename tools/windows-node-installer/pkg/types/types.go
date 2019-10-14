package types

// This package should have the types that will be used by component. For example, aws should have it's own
// sub-package
// TODO: Move every cloud provider types here

// Credentials holds the information to access the Windows instance created.
type Credentials struct {
	// instanceID uniquely identifies the instanceID
	instanceID string
	// ipAddress contains the public ipaddress of the instance created
	ipAddress string
	// password to access the instance created
	password string
}

// NewCredentials takes the instanceID, ipaddress and password of the Windows instance created and returns the
// Credentials structure
func NewCredentials(instanceID, ipAddress, password string) *Credentials {
	return &Credentials{instanceID: instanceID, ipAddress: ipAddress, password: password}
}

// GetIPAddress returns the ipaddress of the given node
func (cred *Credentials) GetIPAddress() string {
	return cred.ipAddress
}

// GetPassword returns the password associated with the given node
func (cred *Credentials) GetPassword() string {
	return cred.password
}

// GetInstanceID returns the instanceId associated with the given node
func (cred *Credentials) GetInstanceId() string {
	return cred.instanceID
}
