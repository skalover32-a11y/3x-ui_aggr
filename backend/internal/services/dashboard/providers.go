package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/panelclient"
	"agr_3x_ui/internal/services/sshclient"
)

type NodeMetricsProvider interface {
	CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error)
}

type ActiveUsersProvider interface {
	CollectActiveUsers(ctx context.Context, node *db.Node) (ActiveUsersResult, error)
}

type SSHMetricsProvider struct {
	SSH       *sshclient.Client
	Encryptor *security.Encryptor
	Timeout   time.Duration
	prevMu    sync.Mutex
	prevMap   map[uuid.UUID]metricSnapshot
}

type metricSnapshot struct {
	at       time.Time
	rxBytes  int64
	txBytes  int64
	cpuTotal int64
	cpuIdle  int64
}

func NewSSHMetricsProvider(ssh *sshclient.Client, enc *security.Encryptor, timeout time.Duration) *SSHMetricsProvider {
	return &SSHMetricsProvider{
		SSH:       ssh,
		Encryptor: enc,
		Timeout:   timeout,
		prevMap:   make(map[uuid.UUID]metricSnapshot),
	}
}

func (p *SSHMetricsProvider) CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error) {
	if node == nil {
		return NodeMetrics{}, errors.New("node missing")
	}
	key, err := p.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return NodeMetrics{}, err
	}
	run := func(cmd string) (string, error) {
		timeout := p.Timeout
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return p.SSH.RunWithOutput(cctx, node.SSHHost, node.SSHPort, node.SSHUser, key, cmd)
	}

	metrics := NodeMetrics{
		CollectedAt:  time.Now(),
		FromAgent:    false,
		PanelVersion: node.PanelVersion,
	}

	cpuOut, cpuErr := run("cat /proc/stat")
	total, idle, cpuOk := parseCPUStat(cpuOut, cpuErr)
	if cpuOk {
		metrics.CPUPct = p.computeCPUPercent(node.ID, total, idle)
	}

	memTotal, memAvail, memErr := parseMeminfo(run("cat /proc/meminfo"))
	if memErr == nil && memTotal != nil && memAvail != nil {
		used := *memTotal - *memAvail
		metrics.RAMTotalBytes = memTotal
		metrics.RAMUsedBytes = &used
	}

	diskTotal, diskUsed, diskErr := parseDF(run("df -P -B1 -x tmpfs -x devtmpfs /"))
	if diskErr == nil {
		metrics.DiskTotalBytes = diskTotal
		metrics.DiskUsedBytes = diskUsed
	}

	uptime, upErr := parseUptime(run("cat /proc/uptime"))
	if upErr == nil {
		metrics.UptimeSec = uptime
	}

	iface := detectIface(run)
	if iface != "" {
		rx, tx, statErr := readNetBytes(run, iface)
		if statErr == nil {
			metrics.NetRxBytes = &rx
			metrics.NetTxBytes = &tx
			rxBps, txBps := p.computeNetBps(node.ID, rx, tx)
			metrics.NetRxBps = rxBps
			metrics.NetTxBps = txBps
			metrics.NetIface = &iface
		}
	}

	if out, _ := run("systemctl is-active xray || true"); strings.TrimSpace(out) != "" {
		val := strings.TrimSpace(out) == "active"
		metrics.XrayRunning = &val
	}
	if out, _ := run("systemctl is-active x-ui || true"); strings.TrimSpace(out) != "" {
		val := strings.TrimSpace(out) == "active"
		metrics.PanelRunning = &val
	}

	return metrics, nil
}

func (p *SSHMetricsProvider) computeNetBps(nodeID uuid.UUID, rx, tx int64) (*int64, *int64) {
	p.prevMu.Lock()
	defer p.prevMu.Unlock()
	now := time.Now()
	prev, ok := p.prevMap[nodeID]
	p.prevMap[nodeID] = metricSnapshot{at: now, rxBytes: rx, txBytes: tx, cpuTotal: prev.cpuTotal, cpuIdle: prev.cpuIdle}
	if !ok {
		return nil, nil
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0.5 {
		return nil, nil
	}
	rxBps := int64(float64(rx-prev.rxBytes) / elapsed)
	txBps := int64(float64(tx-prev.txBytes) / elapsed)
	if rxBps < 0 {
		rxBps = 0
	}
	if txBps < 0 {
		txBps = 0
	}
	return &rxBps, &txBps
}

func (p *SSHMetricsProvider) computeCPUPercent(nodeID uuid.UUID, total, idle int64) *float64 {
	p.prevMu.Lock()
	defer p.prevMu.Unlock()
	prev := p.prevMap[nodeID]
	p.prevMap[nodeID] = metricSnapshot{at: time.Now(), rxBytes: prev.rxBytes, txBytes: prev.txBytes, cpuTotal: total, cpuIdle: idle}
	if prev.cpuTotal == 0 || prev.cpuIdle == 0 {
		return nil
	}
	totalDelta := total - prev.cpuTotal
	idleDelta := idle - prev.cpuIdle
	if totalDelta <= 0 {
		return nil
	}
	usage := (1.0 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	return &usage
}

type PanelActiveUsersProvider struct {
	Encryptor  *security.Encryptor
	Timeout    time.Duration
	SessionTTL time.Duration
	mu         sync.Mutex
	clients    map[uuid.UUID]*panelSession
}

func NewPanelActiveUsersProvider(enc *security.Encryptor, timeout time.Duration) *PanelActiveUsersProvider {
	return &PanelActiveUsersProvider{
		Encryptor:  enc,
		Timeout:    timeout,
		SessionTTL: 30 * time.Minute,
		clients:    make(map[uuid.UUID]*panelSession),
	}
}

func (p *PanelActiveUsersProvider) CollectActiveUsers(ctx context.Context, node *db.Node) (ActiveUsersResult, error) {
	if node == nil || strings.TrimSpace(node.BaseURL) == "" || strings.TrimSpace(node.PanelUsername) == "" {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "no_source",
			SourceDetail: "panel api not configured",
			Available:    false,
		}, nil
	}
	pass, err := p.Encryptor.DecryptString(node.PanelPasswordEnc)
	if err != nil {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "panel",
			SourceDetail: "decrypt failed",
			Available:    false,
		}, nil
	}
	client, err := p.getClient(node, pass)
	if err != nil {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "panel",
			SourceDetail: fmt.Sprintf("request failed: %v", err),
			Available:    false,
		}, nil
	}
	onlineResp, err := client.OnlineClients()
	if err != nil && isAuthError(err) {
		if loginErr := client.Login(); loginErr == nil {
			p.markLogin(node.ID)
			onlineResp, err = client.OnlineClients()
		}
	}
	if err == nil {
		users := extractOnlineUsers(onlineResp)
		return ActiveUsersResult{
			Users:        users,
			Source:       "panel",
			SourceDetail: "ok",
			Available:    true,
		}, nil
	}
	if err != nil && !isOnlineEndpointUnavailable(err) {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "panel",
			SourceDetail: fmt.Sprintf("request failed: %v", err),
			Available:    false,
		}, nil
	}
	listResp, err := client.ListInbounds()
	if err != nil && isAuthError(err) {
		if loginErr := client.Login(); loginErr == nil {
			p.markLogin(node.ID)
			listResp, err = client.ListInbounds()
		}
	}
	if err != nil {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "panel",
			SourceDetail: fmt.Sprintf("request failed: %v", err),
			Available:    false,
		}, nil
	}
	users := extractActiveUsers(listResp)
	return ActiveUsersResult{
		Users:        users,
		Source:       "panel",
		SourceDetail: "ok",
		Available:    true,
	}, nil
}

type panelSession struct {
	client    *panelclient.Client
	key       string
	lastLogin time.Time
}

func (p *PanelActiveUsersProvider) sessionKey(node *db.Node, pass string) string {
	return strings.TrimSpace(node.BaseURL) + "|" + strings.TrimSpace(node.PanelUsername) + "|" + pass + "|" + strconv.FormatBool(node.VerifyTLS)
}

func (p *PanelActiveUsersProvider) getClient(node *db.Node, pass string) (*panelclient.Client, error) {
	key := p.sessionKey(node, pass)
	var sess *panelSession
	now := time.Now()
	p.mu.Lock()
	if existing, ok := p.clients[node.ID]; ok {
		sess = existing
	}
	p.mu.Unlock()
	if sess != nil && sess.key == key && p.sessionValid(sess, now) {
		return sess.client, nil
	}
	client, err := panelclient.New(node.BaseURL, node.PanelUsername, pass, node.VerifyTLS)
	if err != nil {
		return nil, fmt.Errorf("client init failed: %v", err)
	}
	if err := client.Login(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.clients[node.ID] = &panelSession{client: client, key: key, lastLogin: now}
	p.mu.Unlock()
	return client, nil
}

func (p *PanelActiveUsersProvider) sessionValid(sess *panelSession, now time.Time) bool {
	if sess == nil {
		return false
	}
	if p.SessionTTL <= 0 {
		return true
	}
	return now.Sub(sess.lastLogin) <= p.SessionTTL
}

func (p *PanelActiveUsersProvider) markLogin(nodeID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sess, ok := p.clients[nodeID]; ok {
		sess.lastLogin = time.Now()
	}
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden")
}

func extractActiveUsers(listResp map[string]any) []ActiveUser {
	obj, ok := listResp["obj"]
	if !ok {
		return nil
	}
	arr, ok := obj.([]any)
	if !ok {
		return nil
	}
	now := time.Now()
	var users []ActiveUser
	for _, item := range arr {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag := asString(inbound["tag"])
		stats, ok := inbound["clientStats"].([]any)
		if !ok {
			continue
		}
		for _, raw := range stats {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !isOnline(entry) {
				continue
			}
			email := asString(entry["email"])
			if email == "" {
				continue
			}
			lastSeen := parseLastSeen(entry, now)
			up := asInt64(entry["up"])
			down := asInt64(entry["down"])
			user := ActiveUser{
				InboundTag:     nilifyString(tag),
				ClientEmail:    email,
				IP:             asString(entry["ip"]),
				TotalUpBytes:   up,
				TotalDownBytes: down,
				LastSeen:       lastSeen,
			}
			users = append(users, user)
		}
	}
	return users
}

func extractOnlineUsers(listResp map[string]any) []ActiveUser {
	obj, ok := listResp["obj"]
	if !ok {
		return nil
	}
	arr, ok := obj.([]any)
	if !ok {
		if raw, ok := obj.([]string); ok {
			arr = make([]any, 0, len(raw))
			for _, item := range raw {
				arr = append(arr, item)
			}
		} else {
			return nil
		}
	}
	now := time.Now()
	var users []ActiveUser
	for _, item := range arr {
		if email, ok := item.(string); ok {
			email = strings.TrimSpace(email)
			if email != "" {
				users = append(users, ActiveUser{
					ClientEmail: email,
					LastSeen:    now,
				})
			}
			continue
		}
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		email := asString(entry["email"])
		if email == "" {
			email = asString(entry["client"])
		}
		if email == "" {
			email = asString(entry["user"])
		}
		if email == "" {
			email = asString(entry["username"])
		}
		if email == "" {
			continue
		}
		tag := asString(entry["inboundTag"])
		if tag == "" {
			tag = asString(entry["inbound_tag"])
		}
		if tag == "" {
			tag = asString(entry["tag"])
		}
		up := asInt64(entry["up"])
		if up == nil || *up == 0 {
			up = asInt64(entry["uplink"])
		}
		down := asInt64(entry["down"])
		if down == nil || *down == 0 {
			down = asInt64(entry["downlink"])
		}
		lastSeen := parseLastSeen(entry, now)
		user := ActiveUser{
			InboundTag:     nilifyString(tag),
			ClientEmail:    email,
			IP:             asString(entry["ip"]),
			TotalUpBytes:   up,
			TotalDownBytes: down,
			LastSeen:       lastSeen,
		}
		users = append(users, user)
	}
	return users
}

func isOnline(entry map[string]any) bool {
	if val, ok := entry["online"].(bool); ok {
		return val
	}
	if val, ok := entry["is_online"].(bool); ok {
		return val
	}
	if val, ok := entry["status"].(string); ok {
		status := strings.ToLower(strings.TrimSpace(val))
		if status == "offline" || status == "disabled" {
			return false
		}
		if status == "online" || status == "active" {
			return true
		}
	}
	if val, ok := entry["enable"].(bool); ok && !val {
		return false
	}
	if ip := asString(entry["ip"]); ip != "" {
		return true
	}
	return false
}

func isOnlineEndpointUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 404") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not available")
}

func parseLastSeen(entry map[string]any, fallback time.Time) time.Time {
	keys := []string{
		"last_seen",
		"lastSeen",
		"last_online",
		"lastOnline",
		"last_online_at",
		"lastOnlineAt",
	}
	for _, key := range keys {
		if raw, ok := entry[key]; ok {
			if val := parseTimeValue(raw); val != nil {
				return *val
			}
		}
	}
	return fallback
}

func parseTimeValue(raw any) *time.Time {
	switch v := raw.(type) {
	case time.Time:
		return &v
	case string:
		val := strings.TrimSpace(v)
		if val == "" {
			return nil
		}
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return &t
		}
		if t, err := time.Parse("2006-01-02 15:04:05", val); err == nil {
			return &t
		}
	case float64:
		iv := int64(v)
		if iv <= 0 {
			return nil
		}
		return unixToTime(iv)
	case int64:
		if v <= 0 {
			return nil
		}
		return unixToTime(v)
	case int:
		if v <= 0 {
			return nil
		}
		return unixToTime(int64(v))
	case json.Number:
		if iv, err := v.Int64(); err == nil && iv > 0 {
			return unixToTime(iv)
		}
	}
	return nil
}

func unixToTime(val int64) *time.Time {
	if val > 1_000_000_000_000 {
		t := time.UnixMilli(val).UTC()
		return &t
	}
	t := time.Unix(val, 0).UTC()
	return &t
}

func parseCPUStat(out string, err error) (int64, int64, bool) {
	if err != nil {
		return 0, 0, false
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	if !scanner.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var total int64
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseInt(fields[i], 10, 64)
		if err == nil {
			total += v
		}
	}
	idle, _ := strconv.ParseInt(fields[4], 10, 64)
	return total, idle, true
}

func parseMeminfo(out string, err error) (*int64, *int64, error) {
	if err != nil {
		return nil, nil, err
	}
	var total, avail *int64
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			total = parseMemLine(line)
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			avail = parseMemLine(line)
		}
	}
	if total == nil || avail == nil {
		return nil, nil, fmt.Errorf("invalid meminfo")
	}
	return total, avail, nil
}

func parseMemLine(line string) *int64 {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}
	val, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil
	}
	bytes := val * 1024
	return &bytes
}

func parseDF(out string, err error) (*int64, *int64, error) {
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return nil, nil, fmt.Errorf("invalid df")
	}
	parts := strings.Fields(lines[1])
	if len(parts) < 6 {
		return nil, nil, fmt.Errorf("invalid df")
	}
	total, err1 := strconv.ParseInt(parts[1], 10, 64)
	used, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return nil, nil, fmt.Errorf("invalid df")
	}
	return &total, &used, nil
}

func parseUptime(out string, err error) (*int64, error) {
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 1 {
		return nil, fmt.Errorf("invalid uptime")
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}
	sec := int64(val)
	return &sec, nil
}

func detectIface(run func(cmd string) (string, error)) string {
	out, err := run("sh -lc \"ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i==\\\"dev\\\") {print $(i+1); exit}}'\"")
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	out, err = run("sh -lc \"ip route show default 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i==\\\"dev\\\") {print $(i+1); exit}}'\"")
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	out, err = run("sh -lc \"ls /sys/class/net 2>/dev/null | grep -v '^lo$' | head -n1\"")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func readNetBytes(run func(cmd string) (string, error), iface string) (int64, int64, error) {
	if strings.TrimSpace(iface) == "" {
		return 0, 0, fmt.Errorf("missing iface")
	}
	cmd := fmt.Sprintf("sh -lc \"cat /sys/class/net/%s/statistics/rx_bytes /sys/class/net/%s/statistics/tx_bytes\"", iface, iface)
	out, err := run(cmd)
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("invalid net bytes")
	}
	rx, err1 := strconv.ParseInt(lines[0], 10, 64)
	tx, err2 := strconv.ParseInt(lines[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("invalid net bytes")
	}
	return rx, tx, nil
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asInt64(value any) *int64 {
	switch v := value.(type) {
	case int64:
		return &v
	case int:
		val := int64(v)
		return &val
	case float64:
		val := int64(v)
		return &val
	case json.Number:
		if num, err := v.Int64(); err == nil {
			return &num
		}
	}
	return nil
}

func nilifyString(val string) *string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	v := strings.TrimSpace(val)
	return &v
}
