package dashboard

import (
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

type AgentProvider struct {
	HTTP      *http.Client
	Encryptor *security.Encryptor
	Timeout   time.Duration
}

func NewAgentProvider(enc *security.Encryptor, timeout time.Duration) *AgentProvider {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &AgentProvider{
		HTTP:      &http.Client{Timeout: timeout},
		Encryptor: enc,
		Timeout:   timeout,
	}
}

func (p *AgentProvider) CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error) {
	if node == nil || !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
		return NodeMetrics{}, errors.New("agent not configured")
	}
	var resp agentStatsResponse
	if err := p.doGet(ctx, node, "/stats", &resp); err != nil {
		return NodeMetrics{}, err
	}
	metrics := NodeMetrics{
		CollectedAt:    resp.CollectedAt,
		FromAgent:      true,
		CPUPct:         resp.CPUPct,
		RAMUsedBytes:   resp.RAMUsedBytes,
		RAMTotalBytes:  resp.RAMTotalBytes,
		DiskUsedBytes:  resp.DiskUsedBytes,
		DiskTotalBytes: resp.DiskTotalBytes,
		NetRxBps:       resp.NetRxBps,
		NetTxBps:       resp.NetTxBps,
		NetRxBytes:     resp.NetRxBytes,
		NetTxBytes:     resp.NetTxBytes,
		NetIface:       resp.NetIface,
		UptimeSec:      resp.UptimeSec,
		AgentVersion:   resp.AgentVersion,
		PanelVersion:   resp.PanelVersion,
		XrayRunning:    resp.XrayRunning,
		PanelRunning:   resp.PanelRunning,
	}
	return metrics, nil
}

func (p *AgentProvider) CollectActiveUsers(ctx context.Context, node *db.Node) (ActiveUsersResult, error) {
	if node == nil || !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "no_source",
			SourceDetail: "agent not configured",
			Available:    false,
		}, nil
	}
	var resp agentUsersResponse
	if err := p.doGet(ctx, node, "/active-users", &resp); err != nil {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "agent",
			SourceDetail: fmt.Sprintf("request failed: %v", err),
			Available:    false,
		}, nil
	}
	result := ActiveUsersResult{
		Users:        resp.Users,
		Source:       resp.Source,
		SourceDetail: resp.SourceDetail,
		Available:    resp.Available,
	}
	if result.Source == "" {
		result.Source = "agent"
	}
	return result, nil
}

func (p *AgentProvider) doGet(ctx context.Context, node *db.Node, path string, dest any) error {
	url := strings.TrimRight(*node.AgentURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	token := ""
	if node.AgentTokenEnc != nil && strings.TrimSpace(*node.AgentTokenEnc) != "" && p.Encryptor != nil {
		if val, err := p.Encryptor.DecryptString(*node.AgentTokenEnc); err == nil {
			token = strings.TrimSpace(val)
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := p.HTTP
	if node.AgentInsecureTLS {
		client = &http.Client{
			Timeout: p.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("agent status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

type agentStatsResponse struct {
	CollectedAt    time.Time `json:"collected_at"`
	CPUPct         *float64  `json:"cpu_pct"`
	RAMUsedBytes   *int64    `json:"ram_used_bytes"`
	RAMTotalBytes  *int64    `json:"ram_total_bytes"`
	DiskUsedBytes  *int64    `json:"disk_used_bytes"`
	DiskTotalBytes *int64    `json:"disk_total_bytes"`
	NetRxBps       *int64    `json:"net_rx_bps"`
	NetTxBps       *int64    `json:"net_tx_bps"`
	NetRxBytes     *int64    `json:"net_rx_bytes"`
	NetTxBytes     *int64    `json:"net_tx_bytes"`
	NetIface       *string   `json:"net_iface"`
	UptimeSec      *int64    `json:"uptime_sec"`
	AgentVersion   *string   `json:"agent_version"`
	PanelVersion   *string   `json:"panel_version"`
	XrayRunning    *bool     `json:"xray_running"`
	PanelRunning   *bool     `json:"panel_running"`
}

type agentUsersResponse struct {
	CollectedAt  time.Time    `json:"collected_at"`
	Source       string       `json:"source"`
	SourceDetail string       `json:"source_detail"`
	Available    bool         `json:"available"`
	Users        []ActiveUser `json:"users"`
}

type CompositeMetricsProvider struct {
	Agent       NodeMetricsProvider
	SSH         NodeMetricsProvider
	PreferAgent bool
}

func (p *CompositeMetricsProvider) CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error) {
	if p.Agent == nil || node == nil || !node.AgentEnabled {
		return NodeMetrics{}, errors.New("agent not configured")
	}
	return p.Agent.CollectNodeMetrics(ctx, node)
}

type CompositeActiveUsersProvider struct {
	Agent        ActiveUsersProvider
	Panel        ActiveUsersProvider
	PreferAgent  bool
	PanelEnabled bool
}

func (p *CompositeActiveUsersProvider) CollectActiveUsers(ctx context.Context, node *db.Node) (ActiveUsersResult, error) {
	if p.Agent == nil || node == nil || !node.AgentEnabled {
		return ActiveUsersResult{
			Users:        nil,
			Source:       "no_source",
			SourceDetail: "agent not configured",
			Available:    false,
		}, nil
	}
	return p.Agent.CollectActiveUsers(ctx, node)
}
