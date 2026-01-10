package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const agentVersion = "v1"

type Config struct {
	Listen            string   `yaml:"listen"`
	Token             string   `yaml:"token"`
	AllowCIDRs        []string `yaml:"allow_cidrs"`
	AccessLogPath     string   `yaml:"xray_access_log_path"`
	ErrorLogPath      string   `yaml:"xray_error_log_path"`
	PollWindowSeconds int      `yaml:"poll_window_seconds"`
	StatsMode         string   `yaml:"stats_mode"`
	RateLimitRPS      int      `yaml:"rate_limit_rps"`
}

type state struct {
	cfg       Config
	allowlist []*net.IPNet
	limiter   *rateLimiter
	netMu     sync.Mutex
	prevNet   metricSnapshot
	prevCPU   metricSnapshot
}

type metricSnapshot struct {
	at       time.Time
	rxBytes  int64
	txBytes  int64
	cpuTotal int64
	cpuIdle  int64
}

func main() {
	rand.Seed(time.Now().UnixNano())
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	allow, err := parseAllowlist(cfg.AllowCIDRs)
	if err != nil {
		log.Fatalf("allowlist error: %v", err)
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = 5
	}
	st := &state{
		cfg:       cfg,
		allowlist: allow,
		limiter:   newRateLimiter(cfg.RateLimitRPS),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", st.withMiddleware(healthHandler))
	mux.HandleFunc("/version", st.withMiddleware(versionHandler))
	mux.HandleFunc("/stats", st.withMiddleware(st.statsHandler))
	mux.HandleFunc("/active-users", st.withMiddleware(st.activeUsersHandler))
	mux.HandleFunc("/ops/reboot", st.withMiddleware(rebootHandler))
	mux.HandleFunc("/ops/update", st.withMiddleware(updateHandler))

	addr := cfg.Listen
	if addr == "" {
		addr = ":9191"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("node-agent listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func loadConfig(override string) (Config, error) {
	path := strings.TrimSpace(override)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("NODE_AGENT_CONFIG"))
	}
	if path == "" {
		path = "/etc/vlf-agent/config.yaml"
	}
	cfg := Config{
		Listen:            strings.TrimSpace(os.Getenv("NODE_AGENT_ADDR")),
		Token:             strings.TrimSpace(os.Getenv("NODE_AGENT_TOKEN")),
		AllowCIDRs:        splitCSV(os.Getenv("NODE_AGENT_ALLOWLIST")),
		PollWindowSeconds: 60,
		StatsMode:         "log",
		RateLimitRPS:      5,
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.PollWindowSeconds <= 0 {
		cfg.PollWindowSeconds = 60
	}
	if cfg.StatsMode == "" {
		cfg.StatsMode = "log"
	}
	return cfg, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseAllowlist(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	var nets []*net.IPNet
	for _, raw := range cidrs {
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		if !strings.Contains(trim, "/") {
			trim += "/32"
		}
		_, netblock, err := net.ParseCIDR(trim)
		if err != nil {
			return nil, fmt.Errorf("invalid cidr %s", trim)
		}
		nets = append(nets, netblock)
	}
	return nets, nil
}

func (s *state) withMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqID := newRequestID()
		w.Header().Set("X-Request-Id", reqID)
		if !s.allowIP(r.RemoteAddr) {
			log.Printf("req=%s path=%s ip=%s blocked", reqID, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "forbidden"})
			return
		}
		if !s.authOK(r) {
			log.Printf("req=%s path=%s ip=%s unauthorized", reqID, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "unauthorized"})
			return
		}
		if !s.limiter.Allow() {
			log.Printf("req=%s path=%s ip=%s rate_limited", reqID, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "message": "rate limit"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		next(w, r)
		log.Printf("req=%s path=%s ok", reqID, r.URL.Path)
	}
}

func newRequestID() string {
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), rand.Intn(65536))
}

func (s *state) allowIP(remote string) bool {
	if len(s.allowlist) == 0 {
		return false
	}
	ip := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		ip = host
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, netblock := range s.allowlist {
		if netblock.Contains(parsed) {
			return true
		}
	}
	return false
}

func (s *state) authOK(r *http.Request) bool {
	token := strings.TrimSpace(s.cfg.Token)
	if token == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return auth == "Bearer "+token
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"hostname":      host,
		"now":           time.Now().UTC().Format(time.RFC3339),
		"agent_version": agentVersion,
	})
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	osName := strings.TrimSpace(runtimeGOOS())
	uptimeSec := readUptime()
	panelVersion := readPanelVersion()
	xrayVersion := readXrayVersion()
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_version": agentVersion,
		"os":            osName,
		"uptime_sec":    uptimeSec,
		"panel_version": panelVersion,
		"xray_version":  xrayVersion,
	})
}

func (s *state) statsHandler(w http.ResponseWriter, r *http.Request) {
	metrics := s.collectStats(r.Context())
	writeJSON(w, http.StatusOK, metrics)
}

func (s *state) activeUsersHandler(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(s.cfg.StatsMode, "xray_api") {
		writeJSON(w, http.StatusOK, map[string]any{
			"collected_at":  time.Now().UTC().Format(time.RFC3339),
			"source":        "agent",
			"source_detail": "xray api not configured",
			"available":     false,
			"users":         []any{},
		})
		return
	}
	users, detail, ok := s.collectUsersFromLog()
	writeJSON(w, http.StatusOK, map[string]any{
		"collected_at":  time.Now().UTC().Format(time.RFC3339),
		"source":        "agent",
		"source_detail": detail,
		"available":     ok,
		"users":         users,
	})
}

func rebootHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "/sbin/reboot")
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "message": "not implemented"})
}

func (s *state) collectStats(ctx context.Context) map[string]any {
	now := time.Now().UTC()
	cpuPct := s.readCPU()
	ramUsed, ramTotal := readMem()
	diskUsed, diskTotal := readDisk()
	iface := detectIface()
	rxBytes, txBytes := readNetBytes(iface)
	rxBps, txBps := s.computeNetBps(rxBytes, txBytes)
	uptime := readUptime()
	panelRunning := checkSystemctl("x-ui")
	xrayRunning := checkSystemctl("xray")
	panelVersion := readPanelVersion()

	return map[string]any{
		"agent_version":    agentVersion,
		"collected_at":     now.Format(time.RFC3339),
		"cpu_pct":          cpuPct,
		"ram_used_bytes":   ramUsed,
		"ram_total_bytes":  ramTotal,
		"disk_used_bytes":  diskUsed,
		"disk_total_bytes": diskTotal,
		"net_iface":        iface,
		"net_rx_bytes":     rxBytes,
		"net_tx_bytes":     txBytes,
		"net_rx_bps":       rxBps,
		"net_tx_bps":       txBps,
		"uptime_sec":       uptime,
		"panel_version":    panelVersion,
		"panel_running":    panelRunning,
		"xray_running":     xrayRunning,
	}
}

func (s *state) computeNetBps(rxBytes, txBytes *int64) (*int64, *int64) {
	if rxBytes == nil || txBytes == nil {
		return nil, nil
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	now := time.Now()
	prev := s.prevNet
	s.prevNet = metricSnapshot{at: now, rxBytes: *rxBytes, txBytes: *txBytes}
	if prev.at.IsZero() {
		return nil, nil
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0.5 {
		return nil, nil
	}
	rxBps := int64(float64(*rxBytes-prev.rxBytes) / elapsed)
	txBps := int64(float64(*txBytes-prev.txBytes) / elapsed)
	if rxBps < 0 {
		rxBps = 0
	}
	if txBps < 0 {
		txBps = 0
	}
	return &rxBps, &txBps
}

func (s *state) readCPU() *float64 {
	total, idle := readCPUStat()
	if total == 0 {
		return nil
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	prev := s.prevCPU
	s.prevCPU = metricSnapshot{at: time.Now(), cpuTotal: total, cpuIdle: idle}
	if prev.cpuTotal == 0 {
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

func readCPUStat() (int64, int64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		return 0, 0
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 {
		return 0, 0
	}
	var total int64
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseInt(fields[i], 10, 64)
		if err == nil {
			total += v
		}
	}
	idle, _ := strconv.ParseInt(fields[4], 10, 64)
	return total, idle
}

func readMem() (*int64, *int64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, nil
	}
	var total, avail *int64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
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
		return nil, nil
	}
	used := *total - *avail
	return &used, total
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

func readDisk() (*int64, *int64) {
	out, err := exec.Command("df", "-P", "-B1", "-x", "tmpfs", "-x", "devtmpfs", "/").Output()
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, nil
	}
	parts := strings.Fields(lines[1])
	if len(parts) < 6 {
		return nil, nil
	}
	total, err1 := strconv.ParseInt(parts[1], 10, 64)
	used, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &used, &total
}

func readUptime() *int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil
	}
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) == 0 {
		return nil
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil
	}
	sec := int64(val)
	return &sec
}

func detectIface() string {
	if iface := runCommand("sh", "-lc", "ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}'"); strings.TrimSpace(iface) != "" {
		return strings.TrimSpace(iface)
	}
	if iface := runCommand("sh", "-lc", "ip route show default 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}'"); strings.TrimSpace(iface) != "" {
		return strings.TrimSpace(iface)
	}
	if iface := runCommand("sh", "-lc", "ls /sys/class/net 2>/dev/null | grep -v '^lo$' | head -n1"); strings.TrimSpace(iface) != "" {
		return strings.TrimSpace(iface)
	}
	return ""
}

func readNetBytes(iface string) (*int64, *int64) {
	if strings.TrimSpace(iface) == "" {
		return nil, nil
	}
	rxPath := filepath.Join("/sys/class/net", iface, "statistics/rx_bytes")
	txPath := filepath.Join("/sys/class/net", iface, "statistics/tx_bytes")
	rxRaw, err1 := os.ReadFile(rxPath)
	txRaw, err2 := os.ReadFile(txPath)
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	rx, err1 := strconv.ParseInt(strings.TrimSpace(string(rxRaw)), 10, 64)
	tx, err2 := strconv.ParseInt(strings.TrimSpace(string(txRaw)), 10, 64)
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &rx, &tx
}

func checkSystemctl(unit string) *bool {
	out := runCommand("systemctl", "is-active", unit)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	ok := strings.TrimSpace(out) == "active"
	return &ok
}

func readPanelVersion() *string {
	out := runCommand("sh", "-lc", "if [ -x /usr/local/x-ui/x-ui ]; then /usr/local/x-ui/x-ui -v; elif command -v x-ui >/dev/null 2>&1; then x-ui -v 2>/dev/null || x-ui version; elif [ -f /usr/local/x-ui/version ]; then cat /usr/local/x-ui/version; fi; true")
	return nilifyString(out)
}

func readXrayVersion() *string {
	out := runCommand("sh", "-lc", "if command -v xray >/dev/null 2>&1; then xray version || xray -version; elif [ -x /usr/local/bin/xray ]; then /usr/local/bin/xray version || /usr/local/bin/xray -version; elif [ -x /usr/local/x-ui/bin/xray-linux-amd64 ]; then /usr/local/x-ui/bin/xray-linux-amd64 -version; fi; true")
	return nilifyString(out)
}

func nilifyString(val string) *string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	v := strings.TrimSpace(strings.Split(val, "\n")[0])
	return &v
}

func runtimeGOOS() string {
	return strings.TrimSpace(runCommand("uname", "-s"))
}

func runCommand(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *state) collectUsersFromLog() ([]map[string]any, string, bool) {
	path := strings.TrimSpace(s.cfg.AccessLogPath)
	if path == "" {
		return nil, "access log path not configured", false
	}
	lines, err := tailLines(path, 400)
	if err != nil {
		return nil, fmt.Sprintf("access log read failed: %v", err), false
	}
	window := time.Duration(s.cfg.PollWindowSeconds) * time.Second
	if window <= 0 {
		window = 60 * time.Second
	}
	now := time.Now()
	users := map[string]map[string]any{}
	for _, line := range lines {
		email, ip := parseAccessLine(line)
		if email == "" {
			continue
		}
		if _, ok := users[email]; !ok {
			users[email] = map[string]any{
				"client_email": email,
				"inbound_tag":  nil,
				"ip":           ip,
				"last_seen":    now.UTC().Format(time.RFC3339),
			}
		}
	}
	result := make([]map[string]any, 0, len(users))
	for _, entry := range users {
		result = append(result, entry)
	}
	if len(result) == 0 {
		return result, fmt.Sprintf("no users in last %ds", int(window.Seconds())), true
	}
	return result, "log", true
}

var emailRe = regexp.MustCompile(`(?i)(email|user|account)[=:\s]+([a-z0-9._%+\-@]+)`)
var ipRe = regexp.MustCompile(`([0-9]{1,3}\.){3}[0-9]{1,3}|([a-f0-9:]+:+)+[a-f0-9]+`)

func parseAccessLine(line string) (string, string) {
	email := ""
	ip := ""
	if match := emailRe.FindStringSubmatch(line); len(match) >= 3 {
		email = strings.TrimSpace(match[2])
	}
	if match := ipRe.FindStringSubmatch(line); len(match) >= 1 {
		ip = strings.TrimSpace(match[0])
	}
	return email, ip
}

func tailLines(path string, maxLines int) ([]string, error) {
	if maxLines <= 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := stat.Size()
	if size == 0 {
		return nil, nil
	}
	const readSize = int64(64 * 1024)
	var offset int64
	if size > readSize {
		offset = size - readSize
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(buf)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

type rateLimiter struct {
	mu      sync.Mutex
	tokens  int
	max     int
	lastRef time.Time
}

func newRateLimiter(rps int) *rateLimiter {
	return &rateLimiter{tokens: rps, max: rps, lastRef: time.Now()}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if now.Sub(r.lastRef) >= time.Second {
		r.tokens = r.max
		r.lastRef = now
	}
	if r.tokens <= 0 {
		return false
	}
	r.tokens--
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
