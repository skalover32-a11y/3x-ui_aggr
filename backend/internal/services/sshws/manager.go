package sshws

import (
	"errors"
	"sync"
)

var ErrLimitReached = errors.New("ssh session limit reached")

type Manager struct {
	max    int
	mu     sync.Mutex
	active int
}

func NewManager(max int) *Manager {
	if max <= 0 {
		max = 1
	}
	return &Manager{max: max}
}

func (m *Manager) TryAcquire() (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active >= m.max {
		return nil, ErrLimitReached
	}
	m.active++
	released := false
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if released {
			return
		}
		if m.active > 0 {
			m.active--
		}
		released = true
	}, nil
}

func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *Manager) Max() int {
	return m.max
}
