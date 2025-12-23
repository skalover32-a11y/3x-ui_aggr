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
