package dashboard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/sshclient"
)

type NodeMetricsProvider interface {
	CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error)
}

type ActiveUsersProvider interface {
	CollectActiveUsers(ctx context.Context, node *db.Node) (ActiveUsersResult, error)
}

type DisabledActiveUsersProvider struct{}

func (p *DisabledActiveUsersProvider) CollectActiveUsers(_ context.Context, _ *db.Node) (ActiveUsersResult, error) {
	return ActiveUsersResult{
		Users:        nil,
		Source:       "disabled",
		SourceDetail: "active connection collection is disabled",
		Available:    false,
	}, nil
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
		return NodeMetrics{}, errors.New("server is missing")
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
		AgentVersion: node.AgentVersion,
	}

	cpuOut, cpuErr := run("cat /proc/stat")
	total, idle, cpuOK := parseCPUStat(cpuOut, cpuErr)
	if cpuOK {
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
		v, parseErr := strconv.ParseInt(fields[i], 10, 64)
		if parseErr == nil {
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
