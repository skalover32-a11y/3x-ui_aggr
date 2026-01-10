package ops

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

type AgentExecutor struct {
	Encryptor *security.Encryptor
	Timeout   time.Duration
}

func NewAgentExecutor(enc *security.Encryptor, timeout time.Duration) *AgentExecutor {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &AgentExecutor{Encryptor: enc, Timeout: timeout}
}

func (e *AgentExecutor) Reboot(ctx context.Context, node *db.Node) (string, int, error) {
	var resp agentOpResponse
	if err := e.doRequest(ctx, node, http.MethodPost, "/ops/reboot", nil, &resp, 15*time.Second); err != nil {
		return "", 1, err
	}
	if !resp.OK {
		return resp.Log, resp.ExitCodeOr(1), errors.New(resp.MessageOr("reboot failed"))
	}
	logText := resp.Log
	if resp.BootID != "" {
		waitLog, err := e.waitForReboot(ctx, node, resp.BootID)
		if waitLog != "" {
			if logText != "" {
				logText += "\n" + waitLog
			} else {
				logText = waitLog
			}
		}
		if err != nil {
			return logText, 1, err
		}
	}
	if logText == "" {
		logText = "reboot scheduled"
	}
	return logText, 0, nil
}

func (e *AgentExecutor) Update(ctx context.Context, node *db.Node, _ UpdateParams) (string, int, error) {
	var resp agentOpResponse
	if err := e.doRequest(ctx, node, http.MethodPost, "/ops/update-panel", nil, &resp, 20*time.Minute); err != nil {
		return "", 1, err
	}
	if !resp.OK {
		return resp.Log, resp.ExitCodeOr(1), errors.New(resp.MessageOr("update failed"))
	}
	return resp.Log, resp.ExitCodeOr(0), nil
}

func (e *AgentExecutor) RestartService(ctx context.Context, node *db.Node, service string) (string, int, error) {
	payload := map[string]string{"service": strings.TrimSpace(service)}
	var resp agentOpResponse
	if err := e.doRequest(ctx, node, http.MethodPost, "/ops/restart-service", payload, &resp, 30*time.Second); err != nil {
		return "", 1, err
	}
	if !resp.OK {
		return resp.Log, resp.ExitCodeOr(1), errors.New(resp.MessageOr("restart failed"))
	}
	return resp.Log, resp.ExitCodeOr(0), nil
}

func (e *AgentExecutor) DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error) {
	return "", 1, errors.New("deploy_agent not supported via agent")
}

func (e *AgentExecutor) doRequest(ctx context.Context, node *db.Node, method, path string, body any, dest any, timeout time.Duration) error {
	if node == nil || !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
		return errors.New("agent not configured")
	}
	if timeout <= 0 {
		timeout = e.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var buf *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(payload)
	} else {
		buf = bytes.NewReader(nil)
	}

	url := strings.TrimRight(*node.AgentURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, buf)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token := ""
	if node.AgentTokenEnc != nil && strings.TrimSpace(*node.AgentTokenEnc) != "" && e.Encryptor != nil {
		if val, err := e.Encryptor.DecryptString(*node.AgentTokenEnc); err == nil {
			token = strings.TrimSpace(val)
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: timeout}
	if node.AgentInsecureTLS {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("agent status %d", resp.StatusCode)
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (e *AgentExecutor) waitForReboot(ctx context.Context, node *db.Node, bootID string) (string, error) {
	if strings.TrimSpace(bootID) == "" {
		return "reboot scheduled (boot_id unknown)", nil
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("reboot not confirmed: %w", ctx.Err())
		case <-ticker.C:
			health, err := e.getHealth(ctx, node, 10*time.Second)
			if err != nil {
				continue
			}
			if strings.TrimSpace(health.BootID) != "" && strings.TrimSpace(health.BootID) != strings.TrimSpace(bootID) {
				return "reboot confirmed", nil
			}
		}
	}
}

func (e *AgentExecutor) getHealth(ctx context.Context, node *db.Node, timeout time.Duration) (agentHealthResponse, error) {
	var resp agentHealthResponse
	err := e.doRequest(ctx, node, http.MethodGet, "/health", nil, &resp, timeout)
	return resp, err
}

type agentOpResponse struct {
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Log      string `json:"log"`
	ExitCode *int   `json:"exit_code"`
	BootID   string `json:"boot_id"`
}

func (r agentOpResponse) ExitCodeOr(fallback int) int {
	if r.ExitCode == nil {
		return fallback
	}
	return *r.ExitCode
}

func (r agentOpResponse) MessageOr(fallback string) string {
	if strings.TrimSpace(r.Message) == "" {
		return fallback
	}
	return r.Message
}

type agentHealthResponse struct {
	OK     bool   `json:"ok"`
	BootID string `json:"boot_id"`
}
