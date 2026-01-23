package nodecheck

import (
	"crypto/x509"
	"testing"
)

func TestClassifyTLSExpired(t *testing.T) {
	err := x509.CertificateInvalidError{Reason: x509.Expired}
	code, _ := classifyTLSError(err)
	if code != "CERT_EXPIRED" {
		t.Fatalf("expected CERT_EXPIRED, got %s", code)
	}
}
