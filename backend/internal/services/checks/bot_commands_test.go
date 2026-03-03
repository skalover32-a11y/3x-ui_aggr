package checks

import (
	"strings"
	"testing"

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
	check := &db.Check{Type: "systemd"}
	msg := "dial tcp 1.2.3.4:22: i/o timeout"
	if !shouldSuppressGenericAlert(check, &msg) {
		t.Fatalf("expected suppression for ssh transport timeout")
	}
}

func TestShouldNotSuppressGenericAlertForSystemdStateError(t *testing.T) {
	check := &db.Check{Type: "systemd"}
	msg := "state=inactive"
	if shouldSuppressGenericAlert(check, &msg) {
		t.Fatalf("did not expect suppression for state error")
	}
}

func TestShouldSuppressNodeSSHGenericAlert(t *testing.T) {
	check := &db.Check{Type: "ssh"}
	msg := "dial tcp 1.2.3.4:22: i/o timeout"
	if !shouldSuppressGenericAlert(check, &msg) {
		t.Fatalf("expected suppression for ssh check type")
	}
}
