package sshclient

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

var ErrPassphraseRequired = errors.New("passphrase required")

func NormalizePrivateKey(keyBytes []byte, passphrase string) (string, string, error) {
	raw, err := parseRawPrivateKey(keyBytes, passphrase)
	if err != nil {
		return "", "", err
	}
	normalized, err := marshalPrivateKey(raw)
	if err != nil {
		return "", "", err
	}
	signer, err := ssh.ParsePrivateKey(normalized)
	if err != nil {
		return "", "", fmt.Errorf("parse normalized key: %w", err)
	}
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	return string(normalized), fingerprint, nil
}

func parseRawPrivateKey(keyBytes []byte, passphrase string) (any, error) {
	var (
		raw any
		err error
	)
	if passphrase != "" {
		raw, err = ssh.ParseRawPrivateKeyWithPassphrase(keyBytes, []byte(passphrase))
	} else {
		raw, err = ssh.ParseRawPrivateKey(keyBytes)
	}
	if err != nil {
		var passErr *ssh.PassphraseMissingError
		if errors.As(err, &passErr) {
			return nil, ErrPassphraseRequired
		}
		return nil, err
	}
	return raw, nil
}

func marshalPrivateKey(raw any) ([]byte, error) {
	switch key := raw.(type) {
	case *rsa.PrivateKey:
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
	case *ecdsa.PrivateKey:
		b, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
	case ed25519.PrivateKey:
		b, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
	default:
		return nil, fmt.Errorf("unsupported key type %T", raw)
	}
}
