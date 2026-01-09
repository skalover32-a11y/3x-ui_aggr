package ops

import (
	"testing"
	"time"
)

func TestHubSubscribePublishUnsubscribe(t *testing.T) {
	hub := NewHub()
	events, unsubscribe := hub.Subscribe("job-1")

	ev := newEvent("job-1", EventJobStatus, map[string]any{"status": "running"})
	hub.Publish("job-1", ev)

	select {
	case got, ok := <-events:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if got.Type != EventJobStatus {
			t.Fatalf("expected %s, got %s", EventJobStatus, got.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	unsubscribe()
	if _, ok := <-events; ok {
		t.Fatal("expected channel to be closed")
	}

	hub.Publish("job-1", ev)
}
