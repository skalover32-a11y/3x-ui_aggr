package ops

import (
	"sync"
	"time"
)

const (
	EventJobStatus     = "job_status"
	EventItemStatus    = "item_status"
	EventItemLogAppend = "item_log_append"
	EventItemDone      = "item_done"
	EventHeartbeat     = "heartbeat"
)

const eventBufferSize = 100

type Event struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
	Ts    string `json:"ts"`
	Data  any    `json:"data"`
}

type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[string]map[chan Event]struct{}),
	}
}

func (h *Hub) Subscribe(jobID string) (<-chan Event, func()) {
	if h == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, eventBufferSize)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[chan Event]struct{})
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.unsubscribe(jobID, ch) }
}

func (h *Hub) Publish(jobID string, event Event) {
	if h == nil {
		return
	}
	h.mu.RLock()
	subs := h.subs[jobID]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	chans := make([]chan Event, 0, len(subs))
	for ch := range subs {
		chans = append(chans, ch)
	}
	h.mu.RUnlock()

	for _, ch := range chans {
		if !trySend(ch, event) {
			h.unsubscribe(jobID, ch)
		}
	}
}

func (h *Hub) unsubscribe(jobID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[jobID]
	if subs == nil {
		return
	}
	if _, ok := subs[ch]; !ok {
		return
	}
	delete(subs, ch)
	close(ch)
	if len(subs) == 0 {
		delete(h.subs, jobID)
	}
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

func newEvent(jobID, typ string, data any) Event {
	return Event{
		Type:  typ,
		JobID: jobID,
		Ts:    time.Now().UTC().Format(time.RFC3339),
		Data:  data,
	}
}
