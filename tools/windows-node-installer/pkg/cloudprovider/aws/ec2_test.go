package aws

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

type mockEC2Client struct {
	ec2iface.EC2API
}

type Instance struct {
	name         string
	passwordData string
	state        string
}

var (
	mockClient         = &mockEC2Client{}
	awsProvider        = AwsProvider{EC2: mockClient}
	runningInstance    = Instance{name: "RunningInstance", passwordData: "abc123", state: ec2.InstanceStateNameRunning}
	terminatedInstance = Instance{name: "terminatedInstance", state: ec2.InstanceStateNameTerminated}
)

func (m *mockEC2Client) GetPasswordData(instanceDataInput *ec2.GetPasswordDataInput) (*ec2.GetPasswordDataOutput, error) {
	instanceId := *instanceDataInput.InstanceId
	if instanceId == runningInstance.name {
		return &ec2.GetPasswordDataOutput{
			InstanceId:   &runningInstance.name,
			PasswordData: &runningInstance.passwordData,
		}, nil
	}
	return nil, nil
}

func (m *mockEC2Client) DescribeInstances(instanceInput *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	if len(instanceInput.InstanceIds) > 0 {
		return nil, fmt.Errorf("expected only one instance to be available for testing")
	}
	instanceId := *instanceInput.InstanceIds[0]
	if instanceId == runningInstance.name {
		return &ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{InstanceId: &instanceId, State: &ec2.InstanceState{Name: &runningInstance.state}}}}},
		}, nil
	}
	if instanceId == terminatedInstance.name {
		return &ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{InstanceId: &instanceId, State: &ec2.InstanceState{Name: &terminatedInstance.state}}}}},
		}, nil
	}
	return nil, nil
}

// generateKeyPair generates a new key pair
func generateKeyPair() (*rsa.PrivateKey, *rsa.PublicKey) {
	var bits = 2048
	privKey, _ := rsa.GenerateKey(rand.Reader, bits)
	return privKey, &privKey.PublicKey
}

// encryptWithPublicKey encrypts data with public key
func encryptWithPublicKey(msg []byte, pub *rsa.PublicKey) []byte {
	ciphertext, _ := rsa.EncryptPKCS1v15(rand.Reader, pub, msg)
	return ciphertext
}

// TestRSADecrypt tests rsaDecrypt function which  decrypts the given encrypted data
func TestRSADecrypt(t *testing.T) {
	privKey, publicKey := generateKeyPair()
	msg := "abcdefghijklmnopqrstuvwxyz"
	encryptedData := encryptWithPublicKey([]byte(msg), publicKey)
	privKeyBytes := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privKey),
		},
	)
	decryptedMsg, _ := rsaDecrypt(encryptedData, privKeyBytes)
	if string(decryptedMsg) != msg {
		t.Fatalf("Error decrypting message expected %v got %v", msg, decryptedMsg)
	}
}

//getOtherPortRules adds ports WINRM, ssh or rdp rule as required
func getOtherPortRules(port int64, ip string) *ec2.IpPermission {
	ipPermission := (&ec2.IpPermission{}).
		SetIpProtocol("tcp").
		SetFromPort(port).
		SetToPort(port).
		SetIpRanges([]*ec2.IpRange{
			(&ec2.IpRange{}).
				SetCidrIp(ip + "/32"),
		},
		)
	return ipPermission
}

//getAllPortRule adds the rule to allow all ports
func getAllPortRule(vpcCidr string) *ec2.IpPermission {
	ipPermission := (&ec2.IpPermission{}).
		// -1 is to allow all ports.
		SetIpProtocol("-1").
		SetIpRanges([]*ec2.IpRange{
			(&ec2.IpRange{}).
				SetCidrIp(vpcCidr),
		},
		)
	return ipPermission
}

// TODO: Add additional test coverage for testing examineRulesInSg function to check for the nil rules input
// TestGetRulesForSgUpdate tests the getRulesForSgUpdate which gets all the rules to be updated for local IPs
func TestGetRulesForSgUpdate(t *testing.T) {
	myIP := "144.121.20.163"
	otherIP := "100.121.20.163"
	vpcCidr := "1.1.1.1/32"
	testIO := []struct {
		name     string
		in       []*ec2.IpPermission
		expected []*ec2.IpPermission
	}{
		//test with an empty input of rules
		{
			name: "Empty input",
			in:   []*ec2.IpPermission{},
			expected: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(types.SshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
		//test with a partial input of rules
		{
			name: "Partial input from current IP",
			in: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
			},
			expected: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
				getOtherPortRules(types.SshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
		//test with a complete input of rules
		{
			name: "Complete input from current IP",
			in: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(types.SshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
			expected: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
			},
		},
		//test with a partial input from other IP
		{
			name: "Partial input from other IP",
			in: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
				getOtherPortRules(WINRM_PORT, otherIP),
			},
			expected: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(types.SshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
		//test with a complete input from other IP
		{
			name: "Complete input from other IP",
			in: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
				getOtherPortRules(WINRM_PORT, otherIP),
				getOtherPortRules(types.SshPort, otherIP),
				getOtherPortRules(rdpPort, otherIP),
			},
			expected: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(types.SshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
	}

	for _, tt := range testIO {
		actual := awsProvider.getRulesForSgUpdate(myIP, tt.in, vpcCidr)
		assert.ElementsMatch(t, tt.expected, actual, tt.name, "awsProvider.getRulesForSgUpdate(), actual values do not match expected")
	}
}

func TestAwsProvider_waitUntilPasswordDataIsAvailable(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		want       *ec2.GetPasswordDataOutput
		wantErr    bool
	}{
		{
			// We should get password only for running instance
			name:       "Running Instance",
			instanceID: runningInstance.name,
			wantErr:    false,
			want: &ec2.GetPasswordDataOutput{
				InstanceId:   &runningInstance.name,
				PasswordData: &runningInstance.passwordData,
			},
		},
		{
			name:       "Terminated Instance",
			instanceID: terminatedInstance.name,
			wantErr:    true,
			want:       nil,
		},
	}
	for _, tt := range tests {
		got, err := awsProvider.waitUntilPasswordDataIsAvailable(tt.instanceID)
		if (err != nil) != tt.wantErr {
			assert.NoError(t, err, tt.wantErr)
		}
		assert.Equal(t, got, tt.want)
	}
}
