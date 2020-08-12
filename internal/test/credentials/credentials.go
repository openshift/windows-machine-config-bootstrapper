package credentials

import (
	"fmt"
	"golang.org/x/crypto/ssh"
)

const (
	// Username is the default windows username on AWS
	Username = "Administrator"
)

// Credentials holds the information to access the Windows instance created.
type Credentials struct {
	// instanceID uniquely identifies the instanceID
	instanceID string
	// ipAddress contains the public ip address of the instance created
	ipAddress string
	// sshKey to access the instance created
	sshKey ssh.Signer
	// user used for accessing the  instance created
	user string
}

// NewCredentials takes the instanceID, ip address and user of the Windows instance created and returns the
// Credentials structure
func NewCredentials(instanceID, ipAddress, user string) *Credentials {
	return &Credentials{instanceID: instanceID, ipAddress: ipAddress, user: user}
}

// IPAddress returns the ip address of the given node
func (cred *Credentials) IPAddress() string {
	return cred.ipAddress
}

// SSHKey returns the SSH key associated with the given node
func (cred *Credentials) SSHKey() ssh.Signer {
	return cred.sshKey
}

// SetSSHKey sets the ssh signer for given node
func (cred *Credentials) SetSSHKey(signer ssh.Signer) {
	cred.sshKey = signer
}

// GetInstanceID returns the instanceId associated with the given node
func (cred *Credentials) InstanceId() string {
	return cred.instanceID
}

// UserName returns the user name associated with the given node
func (cred *Credentials) UserName() string {
	return cred.user
}

// String returns the string representation of Creds. This is required for Creds to be used with flags.
func (c *Credentials) String() string {
	return fmt.Sprintf("%v", *c)
}
