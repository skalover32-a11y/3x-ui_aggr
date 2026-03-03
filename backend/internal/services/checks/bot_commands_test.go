package checks

import (
	"strings"
	"testing"
	"time"

	"agr_3x_ui/internal/db"
)

func TestDockerCheckCommand(t *testing.T) {
	cmd := dockerListCommand()
	if !strings.Contains(cmd, "docker ps") {
		t.Fatalf("expected docker ps command, got %s", cmd)
	}
}

func TestSystemdCheckCommand(t *testing.T) {
	cmd := systemdCheckCommand("my.service")
	if !strings.Contains(cmd, "systemctl is-active") {
		t.Fatalf("expected systemctl command, got %s", cmd)
	}
	if !strings.Contains(cmd, "'my.service'") {
		t.Fatalf("expected unit to be quoted, got %s", cmd)
	}
}

func TestShouldSuppressGenericAlertForSSHTransportFailures(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-30 * time.Second)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	check := &db.Check{Type: "systemd"}
	msg := "dial tcp 1.2.3.4:22: i/o timeout"
	if !shouldSuppressGenericAlert(node, check, &msg, now, 2*time.Minute) {
		t.Fatalf("expected suppression for ssh transport timeout")
	}
}

func TestShouldNotSuppressGenericAlertForSystemdStateError(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-30 * time.Second)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	check := &db.Check{Type: "systemd"}
	msg := "state=inactive"
	if shouldSuppressGenericAlert(node, check, &msg, now, 2*time.Minute) {
		t.Fatalf("did not expect suppression for state error")
	}
}

func TestShouldSuppressNodeSSHGenericAlert(t *testing.T) {
	now := time.Now()
	check := &db.Check{Type: "ssh"}
	msg := "dial tcp 1.2.3.4:22: i/o timeout"
	if !shouldSuppressGenericAlert(nil, check, &msg, now, 2*time.Minute) {
		t.Fatalf("expected suppression for ssh check type")
	}
}

func TestShouldNotSuppressGenericAlertForOfflineAgent(t *testing.T) {
	now := time.Now()
	lastSeen := now.Add(-12 * time.Minute)
	node := &db.Node{
		AgentEnabled:    true,
		AgentInstalled:  true,
		AgentLastSeenAt: &lastSeen,
	}
	check := &db.Check{Type: "docker"}
	msg := "dial tcp 1.2.3.4:22: i/o timeout"
	if shouldSuppressGenericAlert(node, check, &msg, now, 2*time.Minute) {
		t.Fatalf("did not expect suppression when agent is offline")
	}
}
