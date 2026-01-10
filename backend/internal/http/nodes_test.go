package httpapi

import (
	"testing"
	"time"
)

func TestComputeAgentOnline(t *testing.T) {
	ttl := 90 * time.Second
	now := time.Now()
	if computeAgentOnline(nil, true, ttl) {
		t.Fatal("expected false for nil last_seen")
	}
	old := now.Add(-2 * ttl)
	if computeAgentOnline(&old, true, ttl) {
		t.Fatal("expected false for stale last_seen")
	}
	fresh := now.Add(-10 * time.Second)
	if !computeAgentOnline(&fresh, true, ttl) {
		t.Fatal("expected true for fresh last_seen")
	}
	if computeAgentOnline(&fresh, false, ttl) {
		t.Fatal("expected false when agent not installed")
	}
}
