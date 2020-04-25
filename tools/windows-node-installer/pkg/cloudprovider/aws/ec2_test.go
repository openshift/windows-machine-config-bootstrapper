package aws

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"
	"testing"
)

var (
	awsProvider = AwsProvider{}
)

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
				getOtherPortRules(sshPort, myIP),
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
				getOtherPortRules(sshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
		//test with a complete input of rules
		{
			name: "Complete input from current IP",
			in: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(sshPort, myIP),
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
				getOtherPortRules(sshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
		//test with a complete input from other IP
		{
			name: "Complete input from other IP",
			in: []*ec2.IpPermission{
				getAllPortRule(vpcCidr),
				getOtherPortRules(WINRM_PORT, otherIP),
				getOtherPortRules(sshPort, otherIP),
				getOtherPortRules(rdpPort, otherIP),
			},
			expected: []*ec2.IpPermission{
				getOtherPortRules(WINRM_PORT, myIP),
				getOtherPortRules(sshPort, myIP),
				getOtherPortRules(rdpPort, myIP),
			},
		},
	}

	for _, tt := range testIO {
		actual := awsProvider.getRulesForSgUpdate(myIP, tt.in, vpcCidr)
		assert.ElementsMatch(t, tt.expected, actual, tt.name, "awsProvider.getRulesForSgUpdate(), actual values do not match expected")
	}
}
