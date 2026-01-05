package checks

import (
	"strings"
	"testing"
)

func TestDockerCheckCommand(t *testing.T) {
	cmd := dockerCheckCommand("my-container")
	if !strings.Contains(cmd, "docker inspect") {
		t.Fatalf("expected docker inspect command, got %s", cmd)
	}
	if !strings.Contains(cmd, "'my-container'") {
		t.Fatalf("expected container to be quoted, got %s", cmd)
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
