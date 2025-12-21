package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/services/sshclient"
)

type validateNodeRequest struct {
	BaseURL          string `json:"base_url"`
	VerifyTLS        *bool  `json:"verify_tls"`
	SSHHost          string `json:"ssh_host"`
	SSHPort          int    `json:"ssh_port"`
	SSHUser          string `json:"ssh_user"`
	SSHKey           string `json:"ssh_key"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase"`
	PanelUsername    string `json:"panel_username"`
	PanelPassword    string `json:"panel_password"`
}

type validateSSHResult struct {
	OK                 bool   `json:"ok"`
	Stage              string `json:"stage,omitempty"`
	Error              string `json:"error,omitempty"`
	Fingerprint        string `json:"fingerprint,omitempty"`
	PassphraseRequired bool   `json:"passphrase_required"`
}

type validateURLResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type validateNodeResponse struct {
	SSH          validateSSHResult `json:"ssh"`
	BaseURL      validateURLResult `json:"base_url"`
	PanelVersion string            `json:"panel_version"`
	XrayVersion  string            `json:"xray_version"`
}

func (h *Handler) ValidateNode(c *gin.Context) {
	var req validateNodeRequest
	var keyBytes []byte
	var passphrase string

	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		req.BaseURL = strings.TrimSpace(c.PostForm("base_url"))
		req.SSHHost = strings.TrimSpace(c.PostForm("ssh_host"))
		req.SSHUser = strings.TrimSpace(c.PostForm("ssh_user"))
		req.PanelUsername = strings.TrimSpace(c.PostForm("panel_username"))
		req.PanelPassword = c.PostForm("panel_password")
		req.SSHKey = c.PostForm("ssh_key")
		req.SSHKeyPassphrase = c.PostForm("ssh_key_passphrase")
		passphrase = req.SSHKeyPassphrase
		if raw := strings.TrimSpace(c.PostForm("ssh_port")); raw != "" {
			if val, err := strconv.Atoi(raw); err == nil {
				req.SSHPort = val
			}
		}
		if raw := strings.TrimSpace(c.PostForm("verify_tls")); raw != "" {
			val := raw == "true" || raw == "1"
			req.VerifyTLS = &val
		}
		file, err := c.FormFile("ssh_key_file")
		if err != nil {
			file, _ = c.FormFile("file")
		}
		if file != nil {
			data, err := readFormFile(file)
			if err == nil {
				keyBytes = data
			}
		}
	} else {
		if !parseJSONBody(c, &req) {
			return
		}
		passphrase = req.SSHKeyPassphrase
	}

	if len(keyBytes) == 0 && strings.TrimSpace(req.SSHKey) != "" {
		keyBytes = []byte(req.SSHKey)
	}

	verifyTLS := true
	if req.VerifyTLS != nil {
		verifyTLS = *req.VerifyTLS
	}

	baseURLRes := checkBaseURL(c.Request.Context(), req.BaseURL, verifyTLS)
	sshRes, panelVersion, xrayVersion := h.checkSSHAndVersion(c.Request.Context(), &req, keyBytes, passphrase)

	status := "ok"
	msg := (*string)(nil)
	if !baseURLRes.OK || !sshRes.OK {
		status = "error"
		message := "validation failed"
		msg = &message
	}
	h.auditEvent(c, nil, "NODE_VALIDATE", status, msg, gin.H{
		"base_url": req.BaseURL,
		"ssh_host": req.SSHHost,
		"ssh_port": req.SSHPort,
		"ssh_user": req.SSHUser,
		"base_ok":  baseURLRes.OK,
		"ssh_ok":   sshRes.OK,
	}, nil)

	respondStatus(c, http.StatusOK, validateNodeResponse{
		SSH:          sshRes,
		BaseURL:      baseURLRes,
		PanelVersion: panelVersion,
		XrayVersion:  xrayVersion,
	})
}

func checkBaseURL(ctx context.Context, baseURL string, verifyTLS bool) validateURLResult {
	if strings.TrimSpace(baseURL) == "" {
		return validateURLResult{OK: false, Error: "base_url required"}
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: insecureTLS(verifyTLS),
		},
	}
	candidates := []string{strings.TrimRight(baseURL, "/")}
	if !strings.HasSuffix(baseURL, "/login") {
		candidates = append(candidates, strings.TrimRight(baseURL, "/")+"/login")
	}
	for _, url := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode < 500 {
			return validateURLResult{OK: true, StatusCode: resp.StatusCode}
		}
		return validateURLResult{OK: false, StatusCode: resp.StatusCode, Error: "bad status"}
	}
	return validateURLResult{OK: false, Error: "unreachable"}
}

func insecureTLS(verify bool) *tls.Config {
	return &tls.Config{InsecureSkipVerify: !verify}
}

func (h *Handler) checkSSHAndVersion(ctx context.Context, req *validateNodeRequest, keyBytes []byte, passphrase string) (validateSSHResult, string, string) {
	result := validateSSHResult{}
	if strings.TrimSpace(req.SSHHost) == "" || req.SSHPort == 0 || strings.TrimSpace(req.SSHUser) == "" {
		result.Error = "ssh host/port/user required"
		return result, "", ""
	}
	if len(keyBytes) == 0 {
		result.Error = "ssh key required"
		return result, "", ""
	}
	trimmed := bytes.TrimSpace(keyBytes)
	if isPPK("key.ppk", trimmed) {
		converted, err := convertPPK(ctx, trimmed, passphrase)
		if err != nil {
			result.Error = err.Error()
			result.Stage = "auth"
			return result, "", ""
		}
		trimmed = converted
	} else if !looksLikePrivateKey(trimmed) {
		result.Error = "unsupported key format"
		result.Stage = "auth"
		return result, "", ""
	}
	normalized, fingerprint, err := sshclient.NormalizePrivateKey(trimmed, passphrase)
	if err != nil {
		if errors.Is(err, sshclient.ErrPassphraseRequired) {
			result.PassphraseRequired = true
			result.Stage = "auth"
			result.Error = "passphrase required"
			return result, "", ""
		}
		result.Stage = "auth"
		result.Error = "invalid ssh key"
		return result, "", ""
	}
	result.Fingerprint = fingerprint

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := h.SSHClient.Run(ctx, req.SSHHost, req.SSHPort, req.SSHUser, normalized, "true"); err != nil {
		result.Stage = "command"
		result.Error = err.Error()
		var sshErr *sshclient.Error
		if errors.As(err, &sshErr) {
			result.Stage = sshErr.Stage
			result.Error = sshErr.Err.Error()
		}
		return result, "", ""
	}
	result.OK = true

	xrayVersion := detectVersion(func(cmd string) (string, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return h.SSHClient.RunWithOutput(cctx, req.SSHHost, req.SSHPort, req.SSHUser, normalized, cmd)
	}, []string{
		"if command -v xray >/dev/null 2>&1; then xray version || xray -version; elif [ -x /usr/local/bin/xray ]; then /usr/local/bin/xray version || /usr/local/bin/xray -version; fi",
	})
	panelVersion := detectVersion(func(cmd string) (string, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return h.SSHClient.RunWithOutput(cctx, req.SSHHost, req.SSHPort, req.SSHUser, normalized, cmd)
	}, []string{
		"if command -v x-ui >/dev/null 2>&1; then x-ui version || x-ui -v; elif [ -f /usr/local/x-ui/version ]; then cat /usr/local/x-ui/version; fi",
	})

	return result, panelVersion, xrayVersion
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
	return "unknown"
}
