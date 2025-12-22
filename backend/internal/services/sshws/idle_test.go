package sshws

import (
	"testing"
	"time"
)

func TestIdleTimerFires(t *testing.T) {
	fired := make(chan struct{}, 1)
	timer := NewIdleTimer(30*time.Millisecond, func() {
		fired <- struct{}{}
	})
	defer timer.Stop()
	timer.Reset()
	select {
	case <-fired:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("idle timer did not fire")
	}
}
