package dashboard

import (
	"sync"
	"time"
)

const (
	EventNodeMetricsUpdate = "node_metrics_update"
	EventActiveUsersUpdate = "active_users_update"
	EventSnapshot          = "snapshot"
	EventHeartbeat         = "heartbeat"
)

const eventBufferSize = 200

type Event struct {
	Type string `json:"type"`
	Ts   string `json:"ts"`
	Data any    `json:"data"`
}

type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[chan Event]struct{}),
	}
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	if h == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, eventBufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.unsubscribe(ch) }
}

func (h *Hub) Publish(event Event) {
	if h == nil {
		return
	}
	h.mu.RLock()
	chans := make([]chan Event, 0, len(h.subs))
	for ch := range h.subs {
		chans = append(chans, ch)
	}
	h.mu.RUnlock()
	for _, ch := range chans {
		if !trySend(ch, event) {
			h.unsubscribe(ch)
		}
	}
}

func (h *Hub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; !ok {
		return
	}
	delete(h.subs, ch)
	close(ch)
}

func trySend(ch chan Event, event Event) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case ch <- event:
		return true
	default:
		return false
	}
}

func newEvent(typ string, data any) Event {
	return Event{
		Type: typ,
		Ts:   time.Now().UTC().Format(time.RFC3339),
		Data: data,
	}
}
