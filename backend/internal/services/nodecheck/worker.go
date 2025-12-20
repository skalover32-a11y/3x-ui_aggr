package nodecheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type Worker struct {
	DB       *gorm.DB
	Interval time.Duration
}

func New(dbConn *gorm.DB, interval time.Duration) *Worker {
	return &Worker{DB: dbConn, Interval: interval}
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.DB == nil {
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
	for _, node := range nodes {
		panelOK, latency, panelErr := checkPanel(ctx, node.BaseURL, node.VerifyTLS)
		sshOK, sshErr := checkSSH(ctx, node.SSHHost, node.SSHPort)
		errMsg := joinErrors(panelErr, sshErr)
		entry := db.NodeCheck{
			NodeID:    node.ID,
			TS:        time.Now(),
			PanelOK:   panelOK,
			SSHOK:     sshOK,
			LatencyMS: latency,
			Error:     errMsg,
		}
		_ = w.DB.WithContext(ctx).Create(&entry).Error
	}
}

func checkPanel(ctx context.Context, baseURL string, verifyTLS bool) (bool, int, *string) {
	start := time.Now()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		msg := err.Error()
		return false, 0, &msg
	}
	resp, err := client.Do(req)
	if err != nil {
		msg := err.Error()
		return false, 0, &msg
	}
	defer resp.Body.Close()
	latency := int(time.Since(start).Milliseconds())
	if resp.StatusCode >= 500 {
		msg := fmt.Sprintf("panel status %d", resp.StatusCode)
		return false, latency, &msg
	}
	return true, latency, nil
}

func checkSSH(ctx context.Context, host string, port int) (bool, *string) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		msg := err.Error()
		return false, &msg
	}
	_ = conn.Close()
	return true, nil
}

func joinErrors(panelErr *string, sshErr *string) *string {
	if panelErr == nil && sshErr == nil {
		return nil
	}
	if panelErr != nil && sshErr != nil {
		msg := fmt.Sprintf("panel: %s; ssh: %s", *panelErr, *sshErr)
		return &msg
	}
	if panelErr != nil {
		msg := fmt.Sprintf("panel: %s", *panelErr)
		return &msg
	}
	msg := fmt.Sprintf("ssh: %s", *sshErr)
	return &msg
}
