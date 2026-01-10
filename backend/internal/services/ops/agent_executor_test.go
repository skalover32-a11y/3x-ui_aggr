package ops

import (
	"strings"
	"testing"
)

func TestFormatAgentStatusErrorIncludesMessage(t *testing.T) {
	body := []byte(`{"ok":false,"message":"confirm must be REBOOT"}`)
	err := formatAgentStatusError(400, body)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "agent status 400") {
		t.Fatalf("unexpected error prefix: %s", msg)
	}
	if !strings.Contains(msg, "confirm must be REBOOT") {
		t.Fatalf("expected message in error, got: %s", msg)
	}
}

func TestReadAgentErrorBodyLimit(t *testing.T) {
	longBody := strings.Repeat("a", maxAgentErrorBodyBytes+200) + "TAIL"
	body, err := readAgentErrorBody(strings.NewReader(longBody))
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	err = formatAgentStatusError(400, body)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "TAIL") {
		t.Fatalf("expected truncated body, got: %s", msg)
	}
}
