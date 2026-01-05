package checks

import (
	"strings"
	"testing"
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
