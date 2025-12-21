package metrics

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/sshclient"
)

// Worker collects SSH metrics and versions.
type Worker struct {
	DB        *gorm.DB
	SSH       *sshclient.Client
	Encryptor *security.Encryptor
	Interval  time.Duration
	Retention time.Duration
	lastPrune time.Time
}

func New(dbConn *gorm.DB, ssh *sshclient.Client, enc *security.Encryptor, interval, retention time.Duration) *Worker {
	return &Worker{DB: dbConn, SSH: ssh, Encryptor: enc, Interval: interval, Retention: retention}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.DB == nil || w.SSH == nil || w.Encryptor == nil {
		return
	}
	go func() {
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
	var nodes []db.Node
	if err := w.DB.WithContext(ctx).Find(&nodes).Error; err != nil {
		return
	}
	now := time.Now()
	for _, node := range nodes {
		w.collectForNode(ctx, &node)
	}
	if w.Retention > 0 && (w.lastPrune.IsZero() || time.Since(w.lastPrune) > 24*time.Hour) {
		cutoff := time.Now().Add(-w.Retention)
		_ = w.DB.WithContext(ctx).Where("ts < ?", cutoff).Delete(&db.NodeMetric{}).Error
		w.lastPrune = now
	}
}

func (w *Worker) collectForNode(ctx context.Context, node *db.Node) {
	key, err := w.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		msg := fmt.Sprintf("decrypt key: %v", err)
		w.saveMetric(ctx, node.ID, nil, nil, nil, nil, nil, nil, nil, &msg)
		return
	}
	run := func(cmd string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return w.SSH.RunWithOutput(cctx, node.SSHHost, node.SSHPort, node.SSHUser, key, cmd)
	}

	loads, loadErr := parseLoad(run("cat /proc/loadavg"))
	memTotal, memAvail, memErr := parseMeminfo(run("cat /proc/meminfo"))
	diskTotal, diskUsed, diskErr := parseDF(run("df -P -B1 -x tmpfs -x devtmpfs /"))

	errMsg := joinErrors(loadErr, memErr, diskErr)
	w.saveMetric(ctx, node.ID, loads[0], loads[1], loads[2], memTotal, memAvail, diskTotal, diskUsed, errMsg)

	xrayVersion := detectVersion(run, []string{
		"if command -v xray >/dev/null 2>&1; then xray version || xray -version; elif [ -x /usr/local/bin/xray ]; then /usr/local/bin/xray version || /usr/local/bin/xray -version; fi",
	})
	panelVersion := detectVersion(run, []string{
		"if command -v x-ui >/dev/null 2>&1; then x-ui version || x-ui -v; elif [ -f /usr/local/x-ui/version ]; then cat /usr/local/x-ui/version; fi",
	})
	now := time.Now()
	update := map[string]any{
		"xray_version":        nilifyString(xrayVersion),
		"panel_version":       nilifyString(panelVersion),
		"versions_checked_at": now,
	}
	_ = w.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", node.ID).Updates(update).Error
}

func nilifyString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := strings.TrimSpace(s)
	return &v
}

func detectVersion(run func(cmd string) (string, error), commands []string) string {
	for _, cmd := range commands {
		out, err := run(cmd)
		if err != nil {
			continue
		}
		trim := strings.TrimSpace(out)
		if trim != "" {
			lines := strings.Split(trim, "\n")
			return strings.TrimSpace(lines[0])
		}
	}
	return ""
}

func (w *Worker) saveMetric(ctx context.Context, nodeID uuid.UUID, load1, load5, load15 *float64, memTotal, memAvail, diskTotal, diskUsed *int64, errMsg *string) {
	entry := db.NodeMetric{
		NodeID:            nodeID,
		TS:                time.Now(),
		Load1:             load1,
		Load5:             load5,
		Load15:            load15,
		MemTotalBytes:     memTotal,
		MemAvailableBytes: memAvail,
		DiskTotalBytes:    diskTotal,
		DiskUsedBytes:     diskUsed,
		Error:             errMsg,
	}
	_ = w.DB.WithContext(ctx).Create(&entry).Error
}

func parseLoad(out string, err error) ([3]*float64, *string) {
	var result [3]*float64
	if err != nil {
		msg := err.Error()
		return result, &msg
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 3 {
		msg := "invalid loadavg"
		return result, &msg
	}
	for i := 0; i < 3; i++ {
		if v, err := strconv.ParseFloat(fields[i], 64); err == nil {
			result[i] = &v
		}
	}
	return result, nil
}

func parseMeminfo(out string, err error) (*int64, *int64, *string) {
	if err != nil {
		msg := err.Error()
		return nil, nil, &msg
	}
	var total, avail *int64
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			if val := parseMemLine(line); val != nil {
				total = val
			}
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			if val := parseMemLine(line); val != nil {
				avail = val
			}
		}
	}
	if avail == nil {
		scanner = bufio.NewScanner(strings.NewReader(out))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemFree:") {
				avail = parseMemLine(line)
			}
		}
	}
	if total == nil || avail == nil {
		msg := "invalid meminfo"
		return total, avail, &msg
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

func parseDF(out string, err error) (*int64, *int64, *string) {
	if err != nil {
		msg := err.Error()
		return nil, nil, &msg
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		msg := "invalid df"
		return nil, nil, &msg
	}
	parts := strings.Fields(lines[1])
	if len(parts) < 6 {
		msg := "invalid df columns"
		return nil, nil, &msg
	}
	total, err1 := strconv.ParseInt(parts[1], 10, 64)
	used, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		msg := "invalid df numbers"
		return nil, nil, &msg
	}
	return &total, &used, nil
}

func joinErrors(errs ...*string) *string {
	var parts []string
	for _, e := range errs {
		if e != nil && strings.TrimSpace(*e) != "" {
			parts = append(parts, *e)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	msg := strings.Join(parts, "; ")
	return &msg
}
