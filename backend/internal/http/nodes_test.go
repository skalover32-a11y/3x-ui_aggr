package httpapi

import (
	"testing"
	"time"
)

func TestComputeAgentOnline(t *testing.T) {
	ttl := 90 * time.Second
	now := time.Now()
	if computeAgentOnline(nil, ttl) {
		t.Fatal("expected false for nil last_seen")
	}
	old := now.Add(-2 * ttl)
	if computeAgentOnline(&old, ttl) {
		t.Fatal("expected false for stale last_seen")
	}
	fresh := now.Add(-10 * time.Second)
	if !computeAgentOnline(&fresh, ttl) {
		t.Fatal("expected true for fresh last_seen")
	}
}
