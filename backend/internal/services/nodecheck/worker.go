package nodecheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/alerts"
)

type Worker struct {
	DB       *gorm.DB
	Alerts   *alerts.Service
	Interval time.Duration
}

func New(dbConn *gorm.DB, alertsSvc *alerts.Service, interval time.Duration) *Worker {
	return &Worker{DB: dbConn, Alerts: alertsSvc, Interval: interval}
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
	if err := w.DB.WithContext(ctx).Where("is_enabled = true").Find(&nodes).Error; err != nil {
		return
	}
	settings, _ := w.Alerts.LoadSettings(ctx)
	for _, node := range nodes {
		panelOK, latency, panelErr := true, 0, (*string)(nil)
		var panelErrCode *string
		var panelErrDetail *string
		if isPanelNode(&node) {
			panelOK, latency, panelErr, panelErrCode, panelErrDetail = checkPanel(ctx, node.BaseURL, node.VerifyTLS)
		} else {
			panelOK = true
			latency = 0
			panelErr = nil
		}
		sshOK, sshErr := checkSSH(ctx, node.SSHHost, node.SSHPort, node.SSHEnabled)
		errMsg := joinErrors(panelErr, sshErr)
		entry := db.NodeCheck{
			NodeID:           node.ID,
			TS:               time.Now(),
			PanelOK:          panelOK,
			SSHOK:            sshOK,
			LatencyMS:        latency,
			Error:            errMsg,
			PanelErrorCode:   panelErrCode,
			PanelErrorDetail: panelErrDetail,
		}
		_ = w.DB.WithContext(ctx).Create(&entry).Error
		if w.Alerts != nil {
			w.Alerts.NotifyConnection(ctx, settings, &node, panelOK, panelErr, panelErrCode, sshOK, sshErr)
		}
	}
}

func isPanelNode(node *db.Node) bool {
	if node == nil {
		return false
	}
	if strings.TrimSpace(node.Kind) == "" {
		return strings.TrimSpace(node.BaseURL) != ""
	}
	return strings.EqualFold(node.Kind, "PANEL") && strings.TrimSpace(node.BaseURL) != ""
}

func checkPanel(ctx context.Context, baseURL string, verifyTLS bool) (bool, int, *string, *string, *string) {
	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
		},
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
		if err != nil {
			msg := err.Error()
			code := "GENERIC_HTTP_ERROR"
			return false, 0, &msg, &code, &msg
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		latency := int(time.Since(start).Milliseconds())
		if resp.StatusCode >= 500 {
			msg := fmt.Sprintf("panel status %d", resp.StatusCode)
			code := "GENERIC_HTTP_ERROR"
			return false, latency, &msg, &code, &msg
		}
		return true, latency, nil, nil, nil
	}
	if lastErr != nil {
		msg := lastErr.Error()
		code, detail := classifyTLSError(lastErr)
		return false, 0, &msg, &code, &detail
	}
	msg := "panel check failed"
	code := "GENERIC_HTTP_ERROR"
	return false, 0, &msg, &code, &msg
}

func checkSSH(ctx context.Context, host string, port int, enabled bool) (bool, *string) {
	if !enabled {
		return true, nil
	}
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

func classifyTLSError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	raw := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
		raw = urlErr.Error()
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		if certInvalid.Reason == x509.Expired {
			return "CERT_EXPIRED", raw
		}
		if strings.Contains(strings.ToLower(raw), "not yet valid") {
			return "CERT_NOT_YET_VALID", raw
		}
		if certInvalid.Reason == x509.NotAuthorizedToSign || certInvalid.Reason == x509.CANotAuthorizedForThisName {
			return "UNKNOWN_CA", raw
		}
		return "HANDSHAKE", raw
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return "HOSTNAME_MISMATCH", raw
	}
	var unknownAuth x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return "UNKNOWN_CA", raw
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "GENERIC_HTTP_ERROR", raw
	}
	if strings.Contains(strings.ToLower(raw), "tls") {
		return "HANDSHAKE", raw
	}
	return "GENERIC_HTTP_ERROR", raw
}
