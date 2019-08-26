package cloudprovider

// Cloud is the interface that needs to be implemented per provider to allow support for creating Windows nodes on
// that provider.
type Cloud interface {
	// CreateWindowsVM creates a Windows VM based on available image id, instance type, and ssh key name.
	// TODO: CreateWindowsVM should return a provider object for further interaction with the created instance.
	CreateWindowsVM(imageId, instanceType, sshKey string) error
	// DestroyWindowsVM uses 'windows-node-installer.json' file that contains IDs of created instance and
	// security group and deletes them.
	// Example 'windows-node-installer.json' file:
	// {
	//	"InstanceIDs": ["<example-instance-ID>"],
	//  "SecurityGroupIDs": ["<example-security-group-ID>"]
	// {
	// It deletes the security group only if the group is not associated with any instance.
	// The association between the instance and security group are available from individual cloud provider.
	DestroyWindowsVM() error
}
