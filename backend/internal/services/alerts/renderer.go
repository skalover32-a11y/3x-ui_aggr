package alerts

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	parseModeHTML = "HTML"
	maxErrorLen   = 400
)

var digitRe = regexp.MustCompile(`[0-9]+`)

type InlineButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

func RenderAlert(alert Alert, publicBaseURL string) (string, *InlineKeyboard) {
	lines := []string{renderTitle(alert), renderPrimary(alert)}
	lines = append(lines, renderMeta(alert)...)
	if alert.Occurrences > 1 {
		lines = append(lines, fmt.Sprintf("Occurrences: %d", alert.Occurrences))
	}
	if alert.Error != "" {
		lines = append(lines, renderReason(alert))
		lines = append(lines, fmt.Sprintf("<pre>%s</pre>", escapeHTML(truncateError(alert.Error))))
	}
	msg := strings.Join(lines, "\n")
	return msg, buildKeyboard(alert, publicBaseURL)
}

func renderTitle(alert Alert) string {
	title := escapeHTML(alert.NodeName)
	switch alert.Type {
	case AlertCPU:
		return fmt.Sprintf("<b>🔥 High CPU — %s</b>", title)
	case AlertMemory:
		return fmt.Sprintf("<b>🔥 High memory — %s</b>", title)
	case AlertDisk:
		return fmt.Sprintf("<b>⚠️ Low disk space — %s</b>", title)
	case AlertConnection:
		return fmt.Sprintf("<b>🚨 Connection issue — %s</b>", title)
	default:
		return fmt.Sprintf("<b>⚠️ Alert — %s</b>", title)
	}
}

func renderPrimary(alert Alert) string {
	ts := formatTime(alert.TS)
	switch alert.Type {
	case AlertCPU:
		return fmt.Sprintf("load1: <b>%.2f</b> (threshold %.2f) • %s", alert.Metrics.Load1, alert.Metrics.Threshold, ts)
	case AlertMemory:
		return fmt.Sprintf("used: <b>%.1f%%</b> (threshold %.1f%%) • %s", alert.Metrics.UsedPct, alert.Metrics.Threshold, ts)
	case AlertDisk:
		return fmt.Sprintf("free: <b>%.1f%%</b> (threshold %.1f%%) • %s", alert.Metrics.FreePct, alert.Metrics.Threshold, ts)
	case AlertConnection:
		return fmt.Sprintf("PANEL: %s • SSH: %s • %s", statusBadge(alert.PanelOK), statusBadge(alert.SSHOK), ts)
	default:
		return fmt.Sprintf("Time: %s", ts)
	}
}

func renderMeta(alert Alert) []string {
	lines := []string{}
	if alert.PanelURL != "" {
		lines = append(lines, fmt.Sprintf("Panel: <code>%s</code>", escapeHTML(alert.PanelURL)))
	}
	if alert.IP != "" {
		lines = append(lines, fmt.Sprintf("Host: <code>%s</code>", escapeHTML(alert.IP)))
	}
	lines = append(lines, "Channel: <code>Telegram</code>")
	lines = append(lines, fmt.Sprintf("Severity: <b>%s</b>", severityLabel(alert.Severity)))
	lines = append(lines, recommendationLine(alert.Type))
	return lines
}

func renderReason(alert Alert) string {
	short := shortReason(alert.Error)
	return fmt.Sprintf("Reason: <code>%s</code>", escapeHTML(short))
}

func buildKeyboard(alert Alert, publicBaseURL string) *InlineKeyboard {
	callbackRow := []InlineButton{
		{Text: "🔁 Retry", CallbackData: fmt.Sprintf("retry:%s", alert.AlertID)},
		{Text: "🔕 Mute 1h", CallbackData: fmt.Sprintf("mute:1h:%s", alert.NodeID.String())},
	}
	if strings.TrimSpace(publicBaseURL) == "" || alert.NodeID == uuid.Nil {
		return &InlineKeyboard{InlineKeyboard: [][]InlineButton{callbackRow}}
	}
	base := strings.TrimRight(publicBaseURL, "/")
	nodeURL := fmt.Sprintf("%s/nodes?node=%s", base, alert.NodeID.String())
	metricsURL := fmt.Sprintf("%s/nodes?node=%s&tab=metrics", base, alert.NodeID.String())
	linkRow := []InlineButton{
		{Text: "🔎 Открыть ноду", URL: nodeURL},
		{Text: "📊 Метрики", URL: metricsURL},
	}
	return &InlineKeyboard{InlineKeyboard: [][]InlineButton{linkRow, callbackRow}}
}

func statusBadge(ok bool) string {
	if ok {
		return "✅ ok"
	}
	return "❌ fail"
}

func severityLabel(level Severity) string {
	switch level {
	case SeverityCritical:
		return "CRITICAL"
	case SeverityWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}

func recommendationLine(alertType AlertType) string {
	switch alertType {
	case AlertCPU:
		return "Recommendation: check top processes and system load."
	case AlertMemory:
		return "Recommendation: inspect memory usage and caches."
	case AlertDisk:
		return "Recommendation: free disk space or grow the volume."
	case AlertConnection:
		return "Recommendation: verify panel URL and network connectivity."
	default:
		return "Recommendation: inspect logs for details."
	}
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	return ts.Format("2006-01-02 15:04:05")
}

func escapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

func truncateError(err string) string {
	err = strings.TrimSpace(err)
	if len(err) <= maxErrorLen {
		return err
	}
	return err[:maxErrorLen] + "... (truncated)"
}

func shortReason(err string) string {
	err = strings.TrimSpace(err)
	if err == "" {
		return "unknown"
	}
	line := strings.Split(err, "\n")[0]
	if len(line) > 120 {
		return line[:120] + "..."
	}
	return line
}

func fingerprintFor(alert Alert) string {
	root := strings.ToLower(shortReason(alert.Error))
	switch alert.Type {
	case AlertCPU:
		root = fmt.Sprintf("load1>=%.2f", alert.Metrics.Threshold)
	case AlertMemory:
		root = fmt.Sprintf("mem>=%.1f", alert.Metrics.Threshold)
	case AlertDisk:
		root = fmt.Sprintf("disk<=%.1f", alert.Metrics.Threshold)
	}
	root = digitRe.ReplaceAllString(root, "0")
	root = strings.TrimSpace(root)
	statusBits := ""
	if alert.Type == AlertConnection {
		statusBits = fmt.Sprintf("panel=%t,ssh=%t", alert.PanelOK, alert.SSHOK)
	}
	return fmt.Sprintf("%s|%s|%s|%s", alert.Type, strings.ToLower(alert.NodeName), statusBits, root)
}
