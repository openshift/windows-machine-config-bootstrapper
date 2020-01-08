package types

// This package should have the types that will be used by component. For example, aws should have it's own
// sub-package
// TODO: Move every cloud provider types here

const (
	// ContainerLogsPort number will be opened on the windows node via firewall
	ContainerLogsPort = "10250"
	// FirewallRuleName is the firewall rule name to open the Container Logs Port
	FirewallRuleName = "ContainerLogsPort"
)

// Credentials holds the information to access the Windows instance created.
type Credentials struct {
	// instanceID uniquely identifies the instanceID
	instanceID string
	// ipAddress contains the public ip address of the instance created
	ipAddress string
	// password to access the instance created
	password string
	// user used for accessing the  instance created
	user string
}

// NewCredentials takes the instanceID, ip address, password and user of the Windows instance created and returns the
// Credentials structure
func NewCredentials(instanceID, ipAddress, password, user string) *Credentials {
	return &Credentials{instanceID: instanceID, ipAddress: ipAddress, password: password, user: user}
}

// GetIPAddress returns the ip address of the given node
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

// GetUserName returns the user name associated with the given node
func (cred *Credentials) GetUserName() string {
	return cred.user
}
