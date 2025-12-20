package sshclient

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestNormalizePrivateKey_RSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	normalized, fingerprint, err := NormalizePrivateKey(pemBytes, "")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !strings.Contains(normalized, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("expected rsa pem output, got: %s", normalized[:32])
	}
	if fingerprint == "" {
		t.Fatalf("expected fingerprint")
	}
}
