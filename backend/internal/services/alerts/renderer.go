package alerts

import (
	"fmt"
	"regexp"
	"strconv"
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

func RenderRecovery(alert Alert, publicBaseURL string) (string, *InlineKeyboard) {
	lines := []string{renderRecoveryTitle(alert), renderRecoveryPrimary(alert)}
	lines = append(lines, renderMeta(alert)...)
	msg := strings.Join(lines, "\n")
	return msg, buildKeyboard(alert, publicBaseURL)
}

func renderRecoveryTitle(alert Alert) string {
	title := escapeHTML(alert.NodeName)
	if alert.TargetType == "bot" && strings.TrimSpace(alert.BotKind) != "" {
		return fmt.Sprintf("<b>?? Recovered - %s (%s)</b>", title, escapeHTML(alert.BotKind))
	}
	if strings.TrimSpace(alert.ServiceKind) != "" {
		return fmt.Sprintf("<b>?? Recovered - %s (%s)</b>", title, escapeHTML(alert.ServiceKind))
	}
	return fmt.Sprintf("<b>?? Recovered - %s</b>", title)
}

func renderRecoveryPrimary(alert Alert) string {
	ts := formatTime(alert.TS)
	label := strings.TrimSpace(alert.CheckType)
	if label == "" {
		label = "check"
	}
	return fmt.Sprintf("%s: <b>ok</b> | %s", escapeHTML(label), ts)
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
		return fmt.Sprintf("<b>?? High CPU - %s</b>", title)
	case AlertMemory:
		return fmt.Sprintf("<b>?? High memory - %s</b>", title)
	case AlertDisk:
		return fmt.Sprintf("<b>?? Low disk space - %s</b>", title)
	case AlertConnection:
		return fmt.Sprintf("<b>?? Connection issue - %s</b>", title)
	case AlertTLS:
		return fmt.Sprintf("<b>?? TLS issue - %s</b>", title)
	case AlertGeneric:
		if alert.TargetType == "bot" {
			if strings.TrimSpace(alert.BotKind) != "" {
				return fmt.Sprintf("<b>?? Bot alert - %s (%s)</b>", title, escapeHTML(alert.BotKind))
			}
			return fmt.Sprintf("<b>?? Bot alert - %s</b>", title)
		}
		if strings.TrimSpace(alert.ServiceKind) != "" {
			return fmt.Sprintf("<b>?? Service alert - %s (%s)</b>", title, escapeHTML(alert.ServiceKind))
		}
		return fmt.Sprintf("<b>?? Service alert - %s</b>", title)
	default:
		return fmt.Sprintf("<b>?? Alert - %s</b>", title)
	}
}
func renderPrimary(alert Alert) string {
	ts := formatTime(alert.TS)
	switch alert.Type {
	case AlertCPU:
		return fmt.Sprintf("load1: <b>%.2f</b> (threshold %.2f) | %s", alert.Metrics.Load1, alert.Metrics.Threshold, ts)
	case AlertMemory:
		return fmt.Sprintf("used: <b>%.1f%%</b> (threshold %.1f%%) | %s", alert.Metrics.UsedPct, alert.Metrics.Threshold, ts)
	case AlertDisk:
		return fmt.Sprintf("free: <b>%.1f%%</b> (threshold %.1f%%) | %s", alert.Metrics.FreePct, alert.Metrics.Threshold, ts)
	case AlertConnection:
		return fmt.Sprintf("SSH: %s | %s", statusBadge(alert.SSHOK), ts)
	case AlertTLS:
		reason := tlsLabel(alert.CheckType)
		if reason == "" {
			reason = "TLS error"
		}
		return fmt.Sprintf("%s | %s", escapeHTML(reason), ts)
	case AlertGeneric:
		label := strings.TrimSpace(alert.CheckType)
		if label == "" {
			label = "check"
		}
		status := strings.TrimSpace(alert.Status)
		if status == "" {
			status = "unknown"
		}
		return fmt.Sprintf("%s: <b>%s</b> | %s", escapeHTML(label), escapeHTML(status), ts)
	default:
		return fmt.Sprintf("Time: %s", ts)
	}
}
func renderMeta(alert Alert) []string {
	lines := []string{}
	if alert.PanelURL != "" && alert.Type != AlertConnection && alert.Type != AlertTLS {
		lines = append(lines, fmt.Sprintf("Panel: <code>%s</code>", escapeHTML(alert.PanelURL)))
	}
	if alert.IP != "" {
		lines = append(lines, fmt.Sprintf("Host: <code>%s</code>", escapeHTML(alert.IP)))
	}
	if strings.TrimSpace(alert.ServiceKind) != "" {
		lines = append(lines, fmt.Sprintf("Service: <code>%s</code>", escapeHTML(alert.ServiceKind)))
	}
	if strings.TrimSpace(alert.Target) != "" {
		label := "Target"
		if alert.TargetType == "bot" {
			label = "Bot"
		}
		lines = append(lines, fmt.Sprintf("%s: <code>%s</code>", label, escapeHTML(alert.Target)))
	}
	if strings.TrimSpace(alert.CheckType) != "" {
		lines = append(lines, fmt.Sprintf("Check: <code>%s</code>", escapeHTML(alert.CheckType)))
	}
	if alert.Metrics.StatusCode != 0 {
		lines = append(lines, fmt.Sprintf("HTTP status: <code>%d</code>", alert.Metrics.StatusCode))
	}
	lines = append(lines, "Channel: <code>Telegram</code>")
	lines = append(lines, fmt.Sprintf("Severity: <b>%s</b>", severityLabel(alert.Severity)))
	lines = append(lines, recommendationLine(alert))
	return lines
}
func renderReason(alert Alert) string {
	short := shortReason(alert.Error)
	return fmt.Sprintf("Reason: <code>%s</code>", escapeHTML(short))
}

func ParseCallbackData(raw string) (string, string, int) {
	data := strings.TrimSpace(raw)
	if data == "" {
		return "", "", 0
	}
	if strings.HasPrefix(data, "a:") {
		return "ack", strings.TrimSpace(strings.TrimPrefix(data, "a:")), 0
	}
	if strings.HasPrefix(data, "m1:") {
		return "mute", strings.TrimSpace(strings.TrimPrefix(data, "m1:")), 60
	}
	if strings.HasPrefix(data, "r:") {
		return "retry", strings.TrimSpace(strings.TrimPrefix(data, "r:")), 0
	}
	if strings.HasPrefix(data, "o:") {
		return "open", strings.TrimSpace(strings.TrimPrefix(data, "o:")), 0
	}
	parts := strings.Split(data, ":")
	if len(parts) == 0 {
		return "", "", 0
	}
	action := strings.TrimSpace(parts[0])
	switch action {
	case "ack", "open", "retry":
		if len(parts) < 2 {
			return action, "", 0
		}
		return action, strings.TrimSpace(parts[1]), 0
	case "mute":
		if len(parts) < 2 {
			return action, "", 0
		}
		// legacy: mute:<duration>:<fingerprint>
		if len(parts) >= 3 && (strings.Contains(parts[1], "h") || isNumeric(parts[1])) {
			return action, strings.TrimSpace(parts[2]), parseMuteMinutes(parts[1])
		}
		minutes := 0
		if len(parts) >= 3 {
			minutes = parseMuteMinutes(parts[2])
		}
		return action, strings.TrimSpace(parts[1]), minutes
	default:
		return "", "", 0
	}
}

func parseMuteMinutes(raw string) int {
	val := strings.TrimSpace(raw)
	if val == "" {
		return 0
	}
	if strings.HasSuffix(val, "h") {
		val = strings.TrimSuffix(val, "h")
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	if strings.HasSuffix(raw, "h") {
		return n * 60
	}
	return n
}

func isNumeric(raw string) bool {
	if raw == "" {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func buildKeyboard(alert Alert, publicBaseURL string) *InlineKeyboard {
	alertID := strings.TrimSpace(alert.AlertID)
	callbackRow := []InlineButton{}
	if alertID != "" {
		callbackRow = []InlineButton{
			{Text: "✅ Ack", CallbackData: fmt.Sprintf("a:%s", alertID)},
			{Text: "🔇 Mute 1h", CallbackData: fmt.Sprintf("m1:%s", alertID)},
			{Text: "🔁 Retry", CallbackData: fmt.Sprintf("r:%s", alertID)},
			{Text: "🔎 Open", CallbackData: fmt.Sprintf("o:%s", alertID)},
		}
	}
	if strings.TrimSpace(publicBaseURL) == "" || alert.NodeID == uuid.Nil {
		if len(callbackRow) == 0 {
			return nil
		}
		return &InlineKeyboard{InlineKeyboard: [][]InlineButton{callbackRow}}
	}
	base := strings.TrimRight(publicBaseURL, "/")
	nodeURL := fmt.Sprintf("%s/nodes?node=%s", base, alert.NodeID.String())
	metricsURL := fmt.Sprintf("%s/nodes?node=%s&tab=metrics", base, alert.NodeID.String())
	linkRow := []InlineButton{
		{Text: "🔗 Открыть ноду", URL: nodeURL},
		{Text: "📊 Метрики", URL: metricsURL},
	}
	rows := [][]InlineButton{linkRow}
	if len(callbackRow) > 0 {
		rows = append(rows, callbackRow)
	}
	return &InlineKeyboard{InlineKeyboard: rows}
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

func recommendationLine(alert Alert) string {
	switch alert.Type {
	case AlertCPU:
		return "Recommendation: check top processes and system load."
	case AlertMemory:
		return "Recommendation: inspect memory usage and caches."
	case AlertDisk:
		return "Recommendation: free disk space or grow the volume."
	case AlertConnection:
		return "Recommendation: verify SSH connectivity and credentials."
	case AlertTLS:
		return "Recommendation: renew TLS certificate or disable TLS verification if appropriate."
	case AlertGeneric:
		if alert.TargetType == "bot" {
			return "Recommendation: inspect bot process and check configuration."
		}
		return "Recommendation: inspect service health and check configuration."
	default:
		return "Recommendation: inspect logs for details."
	}
}

func tlsLabel(code string) string {
	switch strings.TrimSpace(code) {
	case "CERT_EXPIRED":
		return "TLS certificate expired"
	case "CERT_NOT_YET_VALID":
		return "TLS certificate not yet valid"
	case "UNKNOWN_CA":
		return "TLS unknown CA"
	case "HOSTNAME_MISMATCH":
		return "TLS hostname mismatch"
	case "HANDSHAKE":
		return "TLS handshake failed"
	default:
		return ""
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
	nodeKey := "unknown"
	if alert.NodeID != uuid.Nil {
		nodeKey = alert.NodeID.String()
	}
	switch alert.Type {
	case AlertConnection:
		component := strings.ToLower(strings.TrimSpace(alert.TargetType))
		if component == "" {
			component = "connection"
		}
		return fmt.Sprintf("connection|%s|%s", nodeKey, component)
	case AlertTLS:
		return fmt.Sprintf("tls|%s", nodeKey)
	case AlertCPU:
		return fmt.Sprintf("cpu|%s|load1>=%.2f", nodeKey, alert.Metrics.Threshold)
	case AlertMemory:
		return fmt.Sprintf("memory|%s|mem>=%.1f", nodeKey, alert.Metrics.Threshold)
	case AlertDisk:
		return fmt.Sprintf("disk|%s|disk<=%.1f", nodeKey, alert.Metrics.Threshold)
	case AlertGeneric:
		if alert.CheckID != uuid.Nil {
			return fmt.Sprintf("generic|%s|check:%s", nodeKey, alert.CheckID.String())
		}
		target := strings.ToLower(strings.TrimSpace(alert.Target))
		checkType := strings.ToLower(strings.TrimSpace(alert.CheckType))
		return fmt.Sprintf("generic|%s|check:%s|target:%s", nodeKey, checkType, target)
	default:
		return fmt.Sprintf("%s|%s", strings.ToLower(string(alert.Type)), nodeKey)
	}
}
