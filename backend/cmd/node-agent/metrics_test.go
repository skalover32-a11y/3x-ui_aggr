package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderPrometheusMetrics(t *testing.T) {
	now := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	stats := map[string]any{
		"agent_version":    agentVersion,
		"service_version":  strPtr(`3x-ui "edge"`),
		"runtime_version":  strPtr("Runtime 1.0"),
		"collected_at":     now.Format(time.RFC3339),
		"cpu_pct":          floatPtr(17.5),
		"ram_used_bytes":   int64Ptr(1024),
		"ram_total_bytes":  int64Ptr(4096),
		"disk_used_bytes":  int64Ptr(2048),
		"disk_total_bytes": int64Ptr(8192),
		"net_iface":        "eth0",
		"net_rx_bytes":     int64Ptr(123),
		"net_tx_bytes":     int64Ptr(456),
		"net_rx_bps":       int64Ptr(12),
		"net_tx_bps":       int64Ptr(34),
		"tcp_connections":  int64Ptr(22),
		"udp_connections":  int64Ptr(7),
		"uptime_sec":       int64Ptr(3600),
		"ping_ms":          int64Ptr(11),
		"service_running":  boolPtr(true),
		"runtime_running":  boolPtr(false),
	}

	body := renderPrometheusMetrics(stats)
	checks := []string{
		"vlf_agent_up 1",
		`vlf_agent_build_info{version="` + agentVersion + `"} 1`,
		`vlf_agent_service_version_info{version="3x-ui \"edge\""} 1`,
		"vlf_agent_collected_at_seconds 1771581600",
		"vlf_agent_cpu_percent 17.5",
		`vlf_agent_network_receive_bytes_total{iface="eth0"} 123`,
		"vlf_agent_service_running 1",
		"vlf_agent_runtime_running 0",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestMetricsMiddlewareAuthModes(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	openState := &state{
		cfg: Config{
			Token:              "secret-token",
			MetricsRequireAuth: false,
		},
		limiter: newRateLimiter(10),
	}
	openRec := httptest.NewRecorder()
	openState.withMetricsMiddleware(okHandler.ServeHTTP).ServeHTTP(openRec, req)
	if openRec.Code != http.StatusNoContent {
		t.Fatalf("expected open metrics status %d, got %d", http.StatusNoContent, openRec.Code)
	}

	secureState := &state{
		cfg: Config{
			Token:              "secret-token",
			MetricsRequireAuth: true,
		},
		limiter: newRateLimiter(10),
	}
	secureRec := httptest.NewRecorder()
	secureState.withMetricsMiddleware(okHandler.ServeHTTP).ServeHTTP(secureRec, req)
	if secureRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status %d, got %d", http.StatusUnauthorized, secureRec.Code)
	}

	authReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authReq.RemoteAddr = "127.0.0.1:12345"
	authReq.Header.Set("Authorization", "Bearer secret-token")
	authRec := httptest.NewRecorder()
	secureState.withMetricsMiddleware(okHandler.ServeHTTP).ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusNoContent {
		t.Fatalf("expected authorized status %d, got %d", http.StatusNoContent, authRec.Code)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}
