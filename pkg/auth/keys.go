package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"

	"golang.org/x/crypto/ssh"
)

func GenerateSigner() (ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	pemKey := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return ssh.ParsePrivateKey(pem.EncodeToMemory(pemKey))
}

func LoadOrGenerateSigner(path string) (ssh.Signer, error) {
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return ssh.ParsePrivateKey(data)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	pemKey := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	pemBytes := pem.EncodeToMemory(pemKey)
	
	err = os.WriteFile(path, pemBytes, 0600)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(pemBytes)
}

func GetHostKeyFingerprint(signer ssh.Signer) string {
	pubKey := signer.PublicKey()
	return ssh.FingerprintSHA256(pubKey)
}