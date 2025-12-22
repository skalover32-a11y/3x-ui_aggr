package sshws

import (
	"sync"
	"time"
)

type IdleTimer struct {
	timeout   time.Duration
	onTimeout func()
	mu        sync.Mutex
	timer     *time.Timer
}

func NewIdleTimer(timeout time.Duration, onTimeout func()) *IdleTimer {
	if timeout <= 0 {
		timeout = time.Minute
	}
	t := &IdleTimer{
		timeout:   timeout,
		onTimeout: onTimeout,
	}
	t.timer = time.AfterFunc(timeout, onTimeout)
	return t
}

func (t *IdleTimer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer == nil {
		t.timer = time.AfterFunc(t.timeout, t.onTimeout)
		return
	}
	t.timer.Reset(t.timeout)
}

func (t *IdleTimer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
	}
}
