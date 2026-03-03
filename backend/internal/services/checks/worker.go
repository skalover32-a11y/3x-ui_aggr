package checks

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/alerts"
	"agr_3x_ui/internal/services/sshclient"
)

// Worker runs generic checks for nodes and services.
type Worker struct {
	DB          *gorm.DB
	Alerts      *alerts.Service
	SSH         *sshclient.Client
	Encryptor   *security.Encryptor
	Interval    time.Duration
	loggedStart bool
}

const (
	minAgentOnlineTTLForGenericSuppress = 90 * time.Second
	maxAgentOnlineTTLForGenericSuppress = 10 * time.Minute
)

func New(dbConn *gorm.DB, alertsSvc *alerts.Service, ssh *sshclient.Client, enc *security.Encryptor, interval time.Duration) *Worker {
	return &Worker{DB: dbConn, Alerts: alertsSvc, SSH: ssh, Encryptor: enc, Interval: interval}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.DB == nil {
		return
	}
	go func() {
		w.backfillServiceChecks(ctx)
		w.backfillBotChecks(ctx)
		w.runOnce(ctx)
		ticker := time.NewTicker(w.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runOnce(ctx)
			}
		}
	}()
}

func (w *Worker) runOnce(ctx context.Context) {
	if w == nil || w.DB == nil {
		return
	}
	var checks []db.Check
	if err := w.DB.WithContext(ctx).Where("enabled = true AND lower(target_type) IN ?", []string{"service", "bot"}).Find(&checks).Error; err != nil {
		return
	}
	if len(checks) == 0 {
		if !w.loggedStart {
			log.Printf("checks runner started: checks=0")
			w.loggedStart = true
		}
		return
	}

	checkCount := len(checks)
	if !w.loggedStart {
		log.Printf("checks runner started: checks=%d", checkCount)
		w.loggedStart = true
	}

	nodeMap := map[string]db.Node{}
	var nodes []db.Node
	_ = w.DB.WithContext(ctx).Find(&nodes).Error
	for _, node := range nodes {
		nodeMap[node.ID.String()] = node
	}

	serviceMap := map[string]db.Service{}
	var services []db.Service
	_ = w.DB.WithContext(ctx).Find(&services).Error
	for _, svc := range services {
		serviceMap[svc.ID.String()] = svc
	}

	botMap := map[string]db.Bot{}
	var bots []db.Bot
	_ = w.DB.WithContext(ctx).Find(&bots).Error
	for _, bot := range bots {
		botMap[bot.ID.String()] = bot
	}

	lastMap := w.loadLastResults(ctx)
	now := time.Now()
	dueCount := 0
	runCount := 0
	for _, check := range checks {
		last := lastMap[check.ID.String()]
		if !isDue(now, check.IntervalSec, last) {
			continue
		}
		dueCount++
		if w.runCheck(ctx, &check, nodeMap, serviceMap, botMap) {
			runCount++
		}
	}
	log.Printf("checks tick: total=%d due=%d executed=%d", checkCount, dueCount, runCount)
}

func isDue(now time.Time, intervalSec int, last *db.CheckResult) bool {
	if intervalSec <= 0 {
		intervalSec = 60
	}
	if last == nil {
		return true
	}
	next := last.TS.Add(time.Duration(intervalSec) * time.Second)
	return now.After(next)
}

func (w *Worker) loadLastResults(ctx context.Context) map[string]*db.CheckResult {
	out := map[string]*db.CheckResult{}
	if w == nil || w.DB == nil {
		return out
	}
	var rows []db.CheckResult
	_ = w.DB.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (check_id)
			id, check_id, ts, status, metrics, error, latency_ms
		FROM check_results
		ORDER BY check_id, ts DESC
	`).Scan(&rows).Error
	for i := range rows {
		row := rows[i]
		out[row.CheckID.String()] = &row
	}
	return out
}

func retriesFor(check *db.Check) int {
	if check == nil {
		return 1
	}
	if check.Retries <= 0 {
		return 1
	}
	return check.Retries + 1
}

func (w *Worker) runCheck(ctx context.Context, check *db.Check, nodeMap map[string]db.Node, serviceMap map[string]db.Service, botMap map[string]db.Bot) bool {
	if check == nil {
		return false
	}
	switch strings.ToLower(check.TargetType) {
	case "node":
		node, ok := nodeMap[check.TargetID.String()]
		if !ok || !node.IsEnabled {
			return false
		}
		var settings *alerts.Settings
		if w.Alerts != nil {
			settings, _ = w.Alerts.LoadSettingsForOrg(ctx, node.OrgID)
		}
		w.runNodeCheck(ctx, settings, &node, check)
		return true
	case "service":
		service, ok := serviceMap[check.TargetID.String()]
		if !ok || !service.IsEnabled {
			return false
		}
		node, ok := nodeMap[service.NodeID.String()]
		if !ok || !node.IsEnabled {
			return false
		}
		var settings *alerts.Settings
		if w.Alerts != nil {
			settings, _ = w.Alerts.LoadSettingsForOrg(ctx, node.OrgID)
		}
		w.runServiceCheck(ctx, settings, &node, &service, check)
		return true
	case "bot":
		bot, ok := botMap[check.TargetID.String()]
		if !ok || !bot.IsEnabled {
			return false
		}
		node, ok := nodeMap[bot.NodeID.String()]
		if !ok || !node.IsEnabled {
			return false
		}
		var settings *alerts.Settings
		if w.Alerts != nil {
			settings, _ = w.Alerts.LoadSettingsForOrg(ctx, node.OrgID)
		}
		w.runBotCheck(ctx, settings, &node, &bot, check)
		return true
	}
	return false
}

func (w *Worker) backfillServiceChecks(ctx context.Context) {
	if w == nil || w.DB == nil {
		return
	}
	res := w.DB.WithContext(ctx).Exec(`
		INSERT INTO checks (target_type, target_id, type, interval_sec, timeout_ms, retries, fail_after_sec, recover_after_ok, enabled, severity_rules, created_at, updated_at)
		SELECT 'service', s.id, 'HTTP', 60, 3000, 1, 300, 2, true, '{}'::jsonb, now(), now()
		FROM services s
		WHERE NOT EXISTS (
			SELECT 1 FROM checks c WHERE c.target_type = 'service' AND c.target_id = s.id
		)
	`)
	if res.Error != nil {
		log.Printf("service checks backfill failed: %v", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		log.Printf("service checks backfill: created=%d", res.RowsAffected)
	}
}

func (w *Worker) backfillBotChecks(ctx context.Context) {
	if w == nil || w.DB == nil {
		return
	}
	res := w.DB.WithContext(ctx).Exec(`
		INSERT INTO checks (target_type, target_id, type, interval_sec, timeout_ms, retries, fail_after_sec, recover_after_ok, enabled, severity_rules, created_at, updated_at)
		SELECT 'bot', b.id, upper(b.kind), 30, 3000, 1, 300, 2, true, '{}'::jsonb, now(), now()
		FROM bots b
		WHERE NOT EXISTS (
			SELECT 1 FROM checks c WHERE c.target_type = 'bot' AND c.target_id = b.id
		)
	`)
	if res.Error != nil {
		log.Printf("bot checks backfill failed: %v", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		log.Printf("bot checks backfill: created=%d", res.RowsAffected)
	}
}

func (w *Worker) runNodeCheck(ctx context.Context, settings *alerts.Settings, node *db.Node, check *db.Check) {
	tries := retriesFor(check)
	switch strings.ToLower(check.Type) {
	case "ssh":
		if !node.SSHEnabled {
			return
		}
		var ok bool
		var latency int
		var errMsg *string
		for i := 0; i < tries; i++ {
			cctx, cancel := context.WithTimeout(ctx, w.timeoutFor(check))
			ok, latency, errMsg = w.checkSSH(cctx, node)
			cancel()
			if ok {
				break
			}
		}
		_ = w.storeResult(ctx, check.ID, ok, latency, nil, errMsg)
		w.notifyGeneric(ctx, settings, node, nil, check, ok, latency, 0, errMsg)
	case "tcp":
		target := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
		var ok bool
		var latency int
		var errMsg *string
		for i := 0; i < tries; i++ {
			ok, latency, errMsg = checkTCP(ctx, target, w.timeoutFor(check))
			if ok {
				break
			}
		}
		_ = w.storeResult(ctx, check.ID, ok, latency, nil, errMsg)
		w.notifyGeneric(ctx, settings, node, nil, check, ok, latency, 0, errMsg)
	}
}

func (w *Worker) executeServiceCheck(ctx context.Context, node *db.Node, service *db.Service, check *db.Check) (bool, int, int, int64, *string) {
	tries := retriesFor(check)
	checkType := strings.ToLower(check.Type)
	if checkType == "" {
		checkType = "http"
	}
	if checkType == "tcp" {
		target := serviceHostPort(service, node)
		if target == "" {
			msg := "service target missing"
			return false, 0, 0, 0, &msg
		}
		var ok bool
		var latency int
		var errMsg *string
		for i := 0; i < tries; i++ {
			ok, latency, errMsg = checkTCP(ctx, target, w.timeoutFor(check))
			if ok {
				break
			}
		}
		if ok {
			return true, latency, 0, 0, nil
		}
		return false, latency, 0, 0, errMsg
	}
	url := serviceURL(service, node, checkType)
	if url == "" {
		msg := "service url missing"
		return false, 0, 0, 0, &msg
	}
	var statusCode int
	var latency int
	var bytes int64
	var errMsg *string
	ok := false
	for i := 0; i < tries; i++ {
		statusCode, latency, bytes, errMsg = checkHTTP(ctx, url, node.VerifyTLS, serviceHeaders(service), w.timeoutFor(check))
		ok = statusCode > 0 && isExpectedStatus(service, statusCode)
		if ok {
			break
		}
	}
	return ok, latency, statusCode, bytes, errMsg
}

func (w *Worker) runServiceCheck(ctx context.Context, settings *alerts.Settings, node *db.Node, service *db.Service, check *db.Check) {
	ok, latency, statusCode, bytes, errMsg := w.executeServiceCheck(ctx, node, service, check)
	metrics := map[string]any{"status_code": statusCode, "bytes": bytes}
	_ = w.storeResult(ctx, check.ID, ok, latency, metrics, errMsg)
	w.notifyGeneric(ctx, settings, node, service, check, ok, latency, statusCode, errMsg)
}

func (w *Worker) runBotCheck(ctx context.Context, settings *alerts.Settings, node *db.Node, bot *db.Bot, check *db.Check) {
	ok, latency, statusCode, bytes, errMsg := w.executeBotCheck(ctx, node, bot, check)
	metrics := map[string]any{}
	if statusCode > 0 {
		metrics["status_code"] = statusCode
		metrics["bytes"] = bytes
	}
	_ = w.storeResult(ctx, check.ID, ok, latency, metrics, errMsg)
	if w.Alerts != nil && !w.shouldSuppressGenericAlert(node, check, errMsg) {
		w.Alerts.NotifyGenericBot(ctx, settings, node, bot, check, ok, latency, statusCode, errMsg)
	}
}

func (w *Worker) timeoutFor(check *db.Check) time.Duration {
	if check.TimeoutMS <= 0 {
		return 3 * time.Second
	}
	return time.Duration(check.TimeoutMS) * time.Millisecond
}

func (w *Worker) checkSSH(ctx context.Context, node *db.Node) (bool, int, *string) {
	if w == nil || w.SSH == nil || w.Encryptor == nil {
		msg := "ssh client not configured"
		return false, 0, &msg
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		msg := "unsupported ssh auth method"
		return false, 0, &msg
	}
	key, err := w.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		msg := err.Error()
		return false, 0, &msg
	}
	start := time.Now()
	err = w.SSH.Run(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key, "true")
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		msg := err.Error()
		return false, latency, &msg
	}
	return true, latency, nil
}

func (w *Worker) storeResult(ctx context.Context, checkID uuid.UUID, ok bool, latency int, metrics map[string]any, errMsg *string) *db.CheckResult {
	status := "ok"
	if !ok {
		status = "fail"
	}
	payload := datatypes.JSON([]byte("{}"))
	if metrics != nil {
		b, err := json.Marshal(metrics)
		if err == nil {
			payload = datatypes.JSON(b)
		}
	}
	var latencyPtr *int
	if latency > 0 {
		latencyPtr = &latency
	}
	row := db.CheckResult{
		CheckID:   checkID,
		TS:        time.Now(),
		Status:    status,
		Metrics:   payload,
		Error:     errMsg,
		LatencyMS: latencyPtr,
	}
	_ = w.DB.WithContext(ctx).Create(&row).Error
	return &row
}

func (w *Worker) notifyGeneric(ctx context.Context, settings *alerts.Settings, node *db.Node, service *db.Service, check *db.Check, ok bool, latency int, statusCode int, errMsg *string) {
	if w == nil || w.Alerts == nil {
		return
	}
	if w.shouldSuppressGenericAlert(node, check, errMsg) {
		return
	}
	status := "ok"
	if !ok {
		status = "fail"
	}
	w.Alerts.NotifyGeneric(ctx, settings, node, service, check, status, latency, statusCode, errMsg)
}

func (w *Worker) shouldSuppressGenericAlert(node *db.Node, check *db.Check, errMsg *string) bool {
	return shouldSuppressGenericAlert(node, check, errMsg, time.Now(), w.agentOnlineTTLForGenericSuppress())
}

func (w *Worker) agentOnlineTTLForGenericSuppress() time.Duration {
	ttl := 3 * time.Minute
	if w != nil && w.Interval > 0 {
		ttl = 3 * w.Interval
	}
	if ttl < minAgentOnlineTTLForGenericSuppress {
		return minAgentOnlineTTLForGenericSuppress
	}
	if ttl > maxAgentOnlineTTLForGenericSuppress {
		return maxAgentOnlineTTLForGenericSuppress
	}
	return ttl
}

func shouldSuppressGenericAlert(node *db.Node, check *db.Check, errMsg *string, now time.Time, agentOnlineTTL time.Duration) bool {
	if check == nil {
		return false
	}
	checkType := strings.ToLower(strings.TrimSpace(check.Type))
	// Node SSH checks are already surfaced by connection alerts from nodecheck worker.
	if checkType == "ssh" {
		return true
	}
	// Systemd/docker checks rely on SSH transport. If SSH is unreachable, connection alert
	// carries the root cause and generic alerts only add noise.
	if checkType != "systemd" && checkType != "docker" {
		return false
	}
	if errMsg == nil {
		return false
	}
	if !isSSHTransportFailure(*errMsg) {
		return false
	}
	return isAgentOnlineForGenericSuppress(node, now, agentOnlineTTL)
}

func isAgentOnlineForGenericSuppress(node *db.Node, now time.Time, ttl time.Duration) bool {
	if node == nil {
		return false
	}
	if !node.AgentEnabled && !node.AgentInstalled {
		return false
	}
	if node.AgentLastSeenAt == nil {
		return false
	}
	if ttl <= 0 {
		ttl = minAgentOnlineTTLForGenericSuppress
	}
	return now.Sub(*node.AgentLastSeenAt) <= ttl
}

func isSSHTransportFailure(raw string) bool {
	msg := strings.ToLower(strings.TrimSpace(raw))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "dial tcp") {
		return true
	}
	if strings.Contains(msg, "i/o timeout") {
		return true
	}
	if strings.Contains(msg, "connection refused") {
		return true
	}
	if strings.Contains(msg, "no route to host") {
		return true
	}
	if strings.Contains(msg, "network is unreachable") {
		return true
	}
	if strings.Contains(msg, "host is unreachable") {
		return true
	}
	return false
}

func (w *Worker) executeBotCheck(ctx context.Context, node *db.Node, bot *db.Bot, check *db.Check) (bool, int, int, int64, *string) {
	tries := retriesFor(check)
	checkType := strings.ToLower(strings.TrimSpace(check.Type))
	if checkType == "" {
		checkType = strings.ToLower(strings.TrimSpace(bot.Kind))
	}
	switch checkType {
	case "docker":
		container := strings.TrimSpace(valueOrEmpty(bot.DockerContainer))
		return w.executeDockerCheck(ctx, node, container, tries)
	case "systemd":
		cmd := systemdCheckCommand(strings.TrimSpace(valueOrEmpty(bot.SystemdUnit)))
		return w.executeSSHStateCheck(ctx, node, cmd, "active", tries)
	default:
		url := botURL(bot, node)
		if url == "" {
			msg := "bot health url missing"
			return false, 0, 0, 0, &msg
		}
		var statusCode int
		var latency int
		var bytes int64
		var errMsg *string
		ok := false
		for i := 0; i < tries; i++ {
			statusCode, latency, bytes, errMsg = checkHTTP(ctx, url, node.VerifyTLS, nil, w.timeoutFor(check))
			ok = statusCode > 0 && isExpectedBotStatus(bot, statusCode)
			if ok {
				break
			}
		}
		return ok, latency, statusCode, bytes, errMsg
	}
}

func (w *Worker) executeSSHStateCheck(ctx context.Context, node *db.Node, cmd string, expected string, tries int) (bool, int, int, int64, *string) {
	if node == nil {
		msg := "node missing"
		return false, 0, 0, 0, &msg
	}
	if !node.SSHEnabled {
		msg := "ssh disabled"
		return false, 0, 0, 0, &msg
	}
	var ok bool
	var latency int
	var errMsg *string
	for i := 0; i < tries; i++ {
		out, ms, err := w.runSSHCommand(ctx, node, cmd)
		latency = ms
		if err != nil {
			errMsg = err
			ok = false
		} else {
			state := strings.TrimSpace(out)
			ok = strings.EqualFold(state, expected)
			if !ok {
				msg := fmt.Sprintf("state=%s", state)
				errMsg = &msg
			} else {
				errMsg = nil
			}
		}
		if ok {
			break
		}
	}
	return ok, latency, 0, 0, errMsg
}

func (w *Worker) executeDockerCheck(ctx context.Context, node *db.Node, container string, tries int) (bool, int, int, int64, *string) {
	if strings.TrimSpace(container) == "" {
		msg := "container required"
		return false, 0, 0, 0, &msg
	}
	var ok bool
	var latency int
	var errMsg *string
	for i := 0; i < tries; i++ {
		out, ms, err := w.runSSHCommand(ctx, node, dockerListCommand())
		latency = ms
		if err == nil {
			ok, errMsg = parseDockerPsOutput(out, container)
			if ok {
				break
			}
		} else {
			errMsg = err
		}
		out, ms, err = w.runSSHCommand(ctx, node, dockerListCommandSudo())
		latency = ms
		if err == nil {
			ok, errMsg = parseDockerPsOutput(out, container)
			if ok {
				break
			}
		} else {
			errMsg = err
		}
	}
	return ok, latency, 0, 0, errMsg
}

func (w *Worker) runSSHCommand(ctx context.Context, node *db.Node, cmd string) (string, int, *string) {
	if w == nil || w.SSH == nil || w.Encryptor == nil {
		msg := "ssh client not configured"
		return "", 0, &msg
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		msg := "unsupported ssh auth method"
		return "", 0, &msg
	}
	key, err := w.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		msg := err.Error()
		return "", 0, &msg
	}
	start := time.Now()
	out, err := w.SSH.RunWithOutput(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key, cmd)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		msg := err.Error()
		return "", latency, &msg
	}
	return out, latency, nil
}

func serviceHeaders(service *db.Service) map[string]string {
	if service == nil || len(service.Headers) == 0 {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(service.Headers, &out); err != nil {
		return nil
	}
	return out
}

func serviceURL(service *db.Service, node *db.Node, checkType string) string {
	if service == nil {
		return ""
	}
	if service.URL != nil && strings.TrimSpace(*service.URL) != "" {
		raw := strings.TrimSpace(*service.URL)
		path := strings.TrimSpace(valueOrEmpty(service.HealthPath))
		if path != "" {
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if parsed, err := url.Parse(raw); err == nil {
				if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
					parsed.Path = path
					return parsed.String()
				}
			}
		}
		return raw
	}
	schema := "http"
	mode := strings.ToLower(strings.TrimSpace(valueOrEmpty(service.TLSMode)))
	if mode == "https" || mode == "tls" || checkType == "https" {
		schema = "https"
	}
	host := strings.TrimSpace(valueOrEmpty(service.Host))
	if host == "" && node != nil {
		host = strings.TrimSpace(node.Host)
		if host == "" {
			host = strings.TrimSpace(node.SSHHost)
		}
	}
	if host == "" {
		return ""
	}
	port := 0
	if service.Port != nil {
		port = *service.Port
	}
	if port <= 0 {
		if schema == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	path := strings.TrimSpace(valueOrEmpty(service.HealthPath))
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://%s:%d%s", schema, host, port, path)
}

func serviceHostPort(service *db.Service, node *db.Node) string {
	host := strings.TrimSpace(valueOrEmpty(service.Host))
	if host == "" && node != nil {
		host = strings.TrimSpace(node.Host)
		if host == "" {
			host = strings.TrimSpace(node.SSHHost)
		}
	}
	if host == "" {
		return ""
	}
	port := 0
	if service.Port != nil {
		port = *service.Port
	}
	if port <= 0 {
		port = 80
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func valueOrEmpty(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func botURL(bot *db.Bot, node *db.Node) string {
	if bot == nil {
		return ""
	}
	if bot.HealthURL != nil && strings.TrimSpace(*bot.HealthURL) != "" {
		raw := strings.TrimSpace(*bot.HealthURL)
		path := strings.TrimSpace(valueOrEmpty(bot.HealthPath))
		if path != "" {
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if parsed, err := url.Parse(raw); err == nil {
				if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
					parsed.Path = path
					return parsed.String()
				}
			}
		}
		return raw
	}
	host := ""
	if node != nil {
		host = strings.TrimSpace(node.Host)
		if host == "" {
			host = strings.TrimSpace(node.SSHHost)
		}
	}
	if host == "" {
		return ""
	}
	path := strings.TrimSpace(valueOrEmpty(bot.HealthPath))
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("http://%s%s", host, path)
}

func isExpectedBotStatus(bot *db.Bot, statusCode int) bool {
	if bot == nil || len(bot.ExpectedStatus) == 0 {
		return statusCode >= 200 && statusCode < 400
	}
	for _, val := range bot.ExpectedStatus {
		if int(val) == statusCode {
			return true
		}
	}
	return false
}

func dockerListCommand() string {
	return "docker ps --format '{{.Names}}'"
}

func dockerListCommandSudo() string {
	return "sudo -n docker ps --format '{{.Names}}'"
}

func parseDockerPsOutput(out, container string) (bool, *string) {
	needle := strings.TrimSpace(container)
	if needle == "" {
		msg := "container required"
		return false, &msg
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if strings.EqualFold(name, needle) {
			return true, nil
		}
	}
	msg := fmt.Sprintf("container %s not running", needle)
	return false, &msg
}

func systemdCheckCommand(unit string) string {
	return fmt.Sprintf("systemctl is-active %s", shellQuote(unit))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	escaped := strings.ReplaceAll(value, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

func checkHTTP(ctx context.Context, url string, verifyTLS bool, headers map[string]string, timeout time.Duration) (int, int, int64, *string) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		msg := err.Error()
		return 0, 0, 0, &msg
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		msg := err.Error()
		return 0, 0, 0, &msg
	}
	defer resp.Body.Close()
	latency := int(time.Since(start).Milliseconds())
	bytes := resp.ContentLength
	if bytes < 0 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		bytes = int64(len(limited))
	}
	return resp.StatusCode, latency, bytes, nil
}

func checkTCP(ctx context.Context, addr string, timeout time.Duration) (bool, int, *string) {
	start := time.Now()
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		msg := err.Error()
		return false, latency, &msg
	}
	_ = conn.Close()
	return true, latency, nil
}

func isExpectedStatus(service *db.Service, statusCode int) bool {
	if service == nil || len(service.ExpectedStatus) == 0 {
		return statusCode >= 200 && statusCode < 400
	}
	for _, val := range service.ExpectedStatus {
		if int(val) == statusCode {
			return true
		}
	}
	return false
}

func (w *Worker) RunNowService(ctx context.Context, serviceID uuid.UUID) (*db.CheckResult, error) {
	if w == nil || w.DB == nil {
		return nil, errors.New("db not configured")
	}
	var service db.Service
	if err := w.DB.WithContext(ctx).First(&service, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	var node db.Node
	if err := w.DB.WithContext(ctx).First(&node, "id = ?", service.NodeID).Error; err != nil {
		return nil, err
	}
	var check db.Check
	if err := w.DB.WithContext(ctx).
		Where("target_type = ? AND target_id = ? AND enabled = true", "service", service.ID).
		Order("created_at desc").
		First(&check).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			backfill := db.Check{
				TargetType:     "service",
				TargetID:       service.ID,
				Type:           "HTTP",
				IntervalSec:    60,
				TimeoutMS:      3000,
				Retries:        1,
				FailAfterSec:   300,
				RecoverAfterOK: 2,
				Enabled:        true,
				SeverityRules:  datatypes.JSON([]byte("{}")),
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			if err := w.DB.WithContext(ctx).Create(&backfill).Error; err != nil {
				return nil, err
			}
			check = backfill
		} else {
			return nil, err
		}
	}
	var settings *alerts.Settings
	if w.Alerts != nil {
		settings, _ = w.Alerts.LoadSettingsForOrg(ctx, node.OrgID)
	}
	ok, latency, statusCode, bytes, errMsg := w.executeServiceCheck(ctx, &node, &service, &check)
	metrics := map[string]any{"status_code": statusCode, "bytes": bytes}
	result := w.storeResult(ctx, check.ID, ok, latency, metrics, errMsg)
	w.notifyGeneric(ctx, settings, &node, &service, &check, ok, latency, statusCode, errMsg)
	return result, nil
}

func (w *Worker) RunNowBot(ctx context.Context, botID uuid.UUID) (*db.CheckResult, error) {
	if w == nil || w.DB == nil {
		return nil, errors.New("db not configured")
	}
	var bot db.Bot
	if err := w.DB.WithContext(ctx).First(&bot, "id = ?", botID).Error; err != nil {
		return nil, err
	}
	var node db.Node
	if err := w.DB.WithContext(ctx).First(&node, "id = ?", bot.NodeID).Error; err != nil {
		return nil, err
	}
	var check db.Check
	if err := w.DB.WithContext(ctx).
		Where("target_type = ? AND target_id = ? AND enabled = true", "bot", bot.ID).
		Order("created_at desc").
		First(&check).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			backfill := db.Check{
				TargetType:     "bot",
				TargetID:       bot.ID,
				Type:           strings.ToUpper(strings.TrimSpace(bot.Kind)),
				IntervalSec:    30,
				TimeoutMS:      3000,
				Retries:        1,
				FailAfterSec:   300,
				RecoverAfterOK: 2,
				Enabled:        true,
				SeverityRules:  datatypes.JSON([]byte("{}")),
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			if err := w.DB.WithContext(ctx).Create(&backfill).Error; err != nil {
				return nil, err
			}
			check = backfill
		} else {
			return nil, err
		}
	}
	var settings *alerts.Settings
	if w.Alerts != nil {
		settings, _ = w.Alerts.LoadSettingsForOrg(ctx, node.OrgID)
	}
	ok, latency, statusCode, bytes, errMsg := w.executeBotCheck(ctx, &node, &bot, &check)
	metrics := map[string]any{}
	if statusCode > 0 {
		metrics["status_code"] = statusCode
		metrics["bytes"] = bytes
	}
	result := w.storeResult(ctx, check.ID, ok, latency, metrics, errMsg)
	if w.Alerts != nil && !w.shouldSuppressGenericAlert(&node, &check, errMsg) {
		w.Alerts.NotifyGenericBot(ctx, settings, &node, &bot, &check, ok, latency, statusCode, errMsg)
	}
	return result, nil
}
