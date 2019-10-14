package aws

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
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
