package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

const (
	defaultPrometheusBaseURL = "http://prometheus:9090"
	promSettingsCacheTTL     = 30 * time.Second
)

var promMetricNameRegex = strings.Join([]string{
	"vlf_agent_build_info",
	"vlf_agent_collected_at_seconds",
	"vlf_agent_cpu_percent",
	"vlf_agent_memory_used_bytes",
	"vlf_agent_memory_total_bytes",
	"vlf_agent_disk_used_bytes",
	"vlf_agent_disk_total_bytes",
	"vlf_agent_network_receive_bytes_per_second",
	"vlf_agent_network_transmit_bytes_per_second",
	"vlf_agent_network_receive_bytes_total",
	"vlf_agent_network_transmit_bytes_total",
	"vlf_agent_uptime_seconds",
	"vlf_agent_ping_milliseconds",
	"vlf_agent_tcp_connections",
	"vlf_agent_udp_connections",
}, "|")

type PrometheusMetricsProvider struct {
	DB      *gorm.DB
	HTTP    *http.Client
	Timeout time.Duration
	cacheMu sync.Mutex
	cache   map[uuid.UUID]promSettingsCacheEntry
}

type promSettingsCacheEntry struct {
	BaseURL string
	At      time.Time
	Err     error
}

type promInstantQueryResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string              `json:"resultType"`
		Result     []promVectorElement `json:"result"`
	} `json:"data"`
}

type promVectorElement struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

func NewPrometheusMetricsProvider(dbConn *gorm.DB, timeout time.Duration) *PrometheusMetricsProvider {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &PrometheusMetricsProvider{
		DB:      dbConn,
		HTTP:    &http.Client{Timeout: timeout},
		Timeout: timeout,
		cache:   map[uuid.UUID]promSettingsCacheEntry{},
	}
}

func (p *PrometheusMetricsProvider) CollectNodeMetrics(ctx context.Context, node *db.Node) (NodeMetrics, error) {
	if node == nil || !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
		return NodeMetrics{}, errors.New("agent not configured")
	}
	if node.OrgID == nil {
		return NodeMetrics{}, errors.New("node org is missing")
	}
	baseURL, err := p.loadBaseURL(ctx, *node.OrgID)
	if err != nil {
		return NodeMetrics{}, err
	}
	instance, err := promInstanceFromAgentURL(*node.AgentURL)
	if err != nil {
		return NodeMetrics{}, err
	}

	upQuery := fmt.Sprintf(`up{instance=%q}`, instance)
	upRows, err := p.queryInstant(ctx, baseURL, upQuery)
	if err != nil {
		return NodeMetrics{}, err
	}
	if len(upRows) == 0 {
		return NodeMetrics{}, fmt.Errorf("prometheus target %s not found", instance)
	}
	upVal, err := parsePromValue(upRows[0].Value)
	if err != nil {
		return NodeMetrics{}, err
	}
	if upVal <= 0 {
		return NodeMetrics{}, fmt.Errorf("prometheus target %s is down", instance)
	}

	query := fmt.Sprintf(`{__name__=~"%s",instance=%q}`, promMetricNameRegex, instance)
	rows, err := p.queryInstant(ctx, baseURL, query)
	if err != nil {
		return NodeMetrics{}, err
	}
	if len(rows) == 0 {
		return NodeMetrics{}, fmt.Errorf("no prometheus metrics for %s", instance)
	}

	metrics := NodeMetrics{
		CollectedAt: time.Now().UTC(),
		FromAgent:   true,
	}
	if node.AgentVersion != nil && strings.TrimSpace(*node.AgentVersion) != "" {
		val := strings.TrimSpace(*node.AgentVersion)
		metrics.AgentVersion = &val
	}
	var hasValue bool
	for _, row := range rows {
		name := strings.TrimSpace(row.Metric["__name__"])
		if name == "" {
			continue
		}
		value, err := parsePromValue(row.Value)
		if err != nil {
			continue
		}
		switch name {
		case "vlf_agent_build_info":
			if version := strings.TrimSpace(row.Metric["version"]); version != "" {
				metrics.AgentVersion = promStrPtr(version)
			}
		case "vlf_agent_collected_at_seconds":
			metrics.CollectedAt = time.Unix(int64(value), 0).UTC()
		case "vlf_agent_cpu_percent":
			metrics.CPUPct = promFloatPtr(value)
			hasValue = true
		case "vlf_agent_memory_used_bytes":
			metrics.RAMUsedBytes = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_memory_total_bytes":
			metrics.RAMTotalBytes = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_disk_used_bytes":
			metrics.DiskUsedBytes = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_disk_total_bytes":
			metrics.DiskTotalBytes = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_network_receive_bytes_per_second":
			metrics.NetRxBps = promInt64Ptr(value)
			if iface := strings.TrimSpace(row.Metric["iface"]); iface != "" {
				metrics.NetIface = promStrPtr(iface)
			}
			hasValue = true
		case "vlf_agent_network_transmit_bytes_per_second":
			metrics.NetTxBps = promInt64Ptr(value)
			if iface := strings.TrimSpace(row.Metric["iface"]); iface != "" {
				metrics.NetIface = promStrPtr(iface)
			}
			hasValue = true
		case "vlf_agent_network_receive_bytes_total":
			metrics.NetRxBytes = promInt64Ptr(value)
			if iface := strings.TrimSpace(row.Metric["iface"]); iface != "" {
				metrics.NetIface = promStrPtr(iface)
			}
			hasValue = true
		case "vlf_agent_network_transmit_bytes_total":
			metrics.NetTxBytes = promInt64Ptr(value)
			if iface := strings.TrimSpace(row.Metric["iface"]); iface != "" {
				metrics.NetIface = promStrPtr(iface)
			}
			hasValue = true
		case "vlf_agent_uptime_seconds":
			metrics.UptimeSec = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_ping_milliseconds":
			metrics.PingMs = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_tcp_connections":
			metrics.TCPConnections = promInt64Ptr(value)
			hasValue = true
		case "vlf_agent_udp_connections":
			metrics.UDPConnections = promInt64Ptr(value)
			hasValue = true
		}
	}
	if !hasValue {
		return NodeMetrics{}, fmt.Errorf("prometheus returned no usable metrics for %s", instance)
	}
	return metrics, nil
}

func (p *PrometheusMetricsProvider) loadBaseURL(ctx context.Context, orgID uuid.UUID) (string, error) {
	if p == nil || p.DB == nil {
		return "", errors.New("db not configured")
	}
	now := time.Now()
	p.cacheMu.Lock()
	if entry, ok := p.cache[orgID]; ok && now.Sub(entry.At) <= promSettingsCacheTTL {
		p.cacheMu.Unlock()
		if entry.Err != nil {
			return "", entry.Err
		}
		return entry.BaseURL, nil
	}
	p.cacheMu.Unlock()

	var row db.PromSetting
	err := p.DB.WithContext(ctx).Where("org_id = ?", orgID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = errors.New("prometheus settings are not configured")
		}
		p.cacheMu.Lock()
		p.cache[orgID] = promSettingsCacheEntry{At: now, Err: err}
		p.cacheMu.Unlock()
		return "", err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(row.PromURL), "/")
	if baseURL == "" {
		baseURL = defaultPrometheusBaseURL
	}
	p.cacheMu.Lock()
	p.cache[orgID] = promSettingsCacheEntry{BaseURL: baseURL, At: now}
	p.cacheMu.Unlock()
	return baseURL, nil
}

func (p *PrometheusMetricsProvider) queryInstant(ctx context.Context, baseURL, query string) ([]promVectorElement, error) {
	if p == nil || p.HTTP == nil {
		return nil, errors.New("prometheus client not configured")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultPrometheusBaseURL
	}
	q := url.Values{}
	q.Set("query", query)
	endpoint := baseURL + "/api/v1/query?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("prometheus http %d: %s", resp.StatusCode, message)
	}
	var payload promInstantQueryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(payload.Status)) != "success" {
		reason := strings.TrimSpace(payload.Error)
		if reason == "" {
			reason = "query failed"
		}
		return nil, fmt.Errorf("prometheus %s: %s", strings.TrimSpace(payload.ErrorType), reason)
	}
	if payload.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected result type: %s", payload.Data.ResultType)
	}
	return payload.Data.Result, nil
}

func promInstanceFromAgentURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("agent url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		u, err = url.Parse("http://" + raw)
	}
	if err != nil {
		return "", err
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return "", errors.New("invalid agent url")
	}
	if !strings.Contains(host, ":") {
		if strings.EqualFold(u.Scheme, "https") {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	return host, nil
}

func parsePromValue(sample []any) (float64, error) {
	if len(sample) < 2 {
		return 0, errors.New("invalid prometheus sample")
	}
	switch raw := sample[1].(type) {
	case string:
		return strconv.ParseFloat(strings.TrimSpace(raw), 64)
	case float64:
		return raw, nil
	default:
		return 0, errors.New("invalid prometheus sample value type")
	}
}

func promInt64Ptr(v float64) *int64 {
	val := int64(v)
	return &val
}

func promFloatPtr(v float64) *float64 {
	val := v
	return &val
}

func promStrPtr(v string) *string {
	val := strings.TrimSpace(v)
	if val == "" {
		return nil
	}
	return &val
}
