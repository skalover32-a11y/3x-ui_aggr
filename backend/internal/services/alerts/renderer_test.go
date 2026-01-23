package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEscapeHTML(t *testing.T) {
	got := escapeHTML("<tag>&value>")
	want := "&lt;tag&gt;&amp;value&gt;"
	if got != want {
		t.Fatalf("escapeHTML mismatch: got %q want %q", got, want)
	}
}

func TestTruncateError(t *testing.T) {
	long := strings.Repeat("a", maxErrorLen+10)
	got := truncateError(long)
	if len(got) <= maxErrorLen {
		t.Fatalf("expected truncation, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Fatalf("missing truncated suffix: %q", got)
	}
}

func TestRenderAlertCPU(t *testing.T) {
	alert := Alert{
		Type:     AlertCPU,
		NodeID:   uuid.New(),
		NodeName: "NODE-1",
		TS:       time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			Load1:     2.12,
			Threshold: 2.0,
		},
	}
	out, keyboard := RenderAlert(alert, "https://example.com")
	if !strings.Contains(out, "High CPU") {
		t.Fatalf("expected High CPU in output: %s", out)
	}
	if keyboard == nil || len(keyboard.InlineKeyboard) == 0 {
		t.Fatalf("expected inline keyboard")
	}
}

func TestFingerprintNormalization(t *testing.T) {
	alert := Alert{
		Type:     AlertConnection,
		NodeName: "NODE",
		PanelOK:  false,
		SSHOK:    true,
		Error:    "panel: Get \"https://example.com\": status 502",
	}
	fp := fingerprintFor(alert)
	if strings.Contains(fp, "502") {
		t.Fatalf("expected digits to be normalized in fingerprint: %s", fp)
	}
}

func TestCallbackDataShort(t *testing.T) {
	alert := Alert{
		NodeID:   uuid.New(),
		NodeName: "node",
		Type:     AlertConnection,
		TS:       time.Now(),
		Severity: SeverityCritical,
		AlertID:  uuid.New().String(),
	}
	_, keyboard := RenderAlert(alert, "https://example.com")
	if keyboard == nil || len(keyboard.InlineKeyboard) == 0 {
		t.Fatalf("expected inline keyboard")
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != "" && len(btn.CallbackData) > 64 {
				t.Fatalf("callback_data too long: %s", btn.CallbackData)
			}
			if strings.Contains(btn.Text, "??") {
				t.Fatalf("unexpected placeholder in button text: %s", btn.Text)
			}
		}
	}
}

func TestParseCallbackDataLegacyMute(t *testing.T) {
	action, alertID, minutes := ParseCallbackData("mute:1h:fingerprint")
	if action != "mute" {
		t.Fatalf("expected mute, got %s", action)
	}
	if alertID != "fingerprint" {
		t.Fatalf("expected fingerprint, got %s", alertID)
	}
	if minutes != 60 {
		t.Fatalf("expected 60, got %d", minutes)
	}
}
