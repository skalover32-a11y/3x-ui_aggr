package nodecheck

import (
	"crypto/x509"
	"testing"
	"time"

	"agr_3x_ui/internal/db"
)

func TestClassifyTLSExpired(t *testing.T) {
	err := x509.CertificateInvalidError{Reason: x509.Expired}
	code, _ := classifyTLSError(err)
	if code != "CERT_EXPIRED" {
		t.Fatalf("expected CERT_EXPIRED, got %s", code)
	}
}

func TestShouldSuppressSSHTransportAlertWhenAgentOnline(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-45 * time.Second)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	errMsg := "dial tcp 89.44.85.56:22: i/o timeout"
	if !shouldSuppressSSHTransportAlert(node, &errMsg, now, 2*time.Minute) {
		t.Fatalf("expected suppression for transport ssh failure when agent is online")
	}
}

func TestShouldSuppressSSHTransportAlertWhenAgentOffline(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-8 * time.Minute)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	errMsg := "dial tcp 89.44.85.56:22: i/o timeout"
	if shouldSuppressSSHTransportAlert(node, &errMsg, now, 2*time.Minute) {
		t.Fatalf("did not expect suppression when agent is offline")
	}
}

func TestShouldSuppressSSHTransportAlertKeepsAuthErrors(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-30 * time.Second)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	errMsg := "ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"
	if shouldSuppressSSHTransportAlert(node, &errMsg, now, 2*time.Minute) {
		t.Fatalf("did not expect suppression for non-transport ssh error")
	}
}
