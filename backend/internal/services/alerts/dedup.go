package alerts

import (
	"strings"
	"sync"
	"time"
)

type dedupEntry struct {
	lastSent    time.Time
	lastSeen    time.Time
	occurrences int
	messageIDs  map[string]int
	alertID     string
	alert       Alert
}

type Deduper struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*dedupEntry
}

func NewDeduper(ttl time.Duration) *Deduper {
	return &Deduper{
		ttl:     ttl,
		entries: make(map[string]*dedupEntry),
	}
}

func (d *Deduper) Track(alert Alert, now time.Time) (bool, *dedupEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.entries[alert.Fingerprint]
	if entry == nil || now.Sub(entry.lastSent) > d.ttl {
		entry = &dedupEntry{
			lastSent:    now,
			lastSeen:    now,
			occurrences: 1,
			messageIDs:  map[string]int{},
			alertID:     alert.AlertID,
			alert:       alert,
		}
		d.entries[alert.Fingerprint] = entry
		return true, entry
	}
	entry.occurrences++
	entry.lastSeen = now
	entry.alert = alert
	return false, entry
}

func (d *Deduper) RecordSend(fingerprint string, messageIDs map[string]int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.entries[fingerprint]
	if entry == nil {
		return
	}
	if entry.messageIDs == nil {
		entry.messageIDs = map[string]int{}
	}
	for chatID, msgID := range messageIDs {
		entry.messageIDs[chatID] = msgID
	}
}

func (d *Deduper) UpdateAlert(fingerprint string, alert Alert) *dedupEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.entries[fingerprint]
	if entry == nil {
		return nil
	}
	entry.alert = alert
	return entry
}

func (d *Deduper) MessageIDs(fingerprint string) map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.entries[fingerprint]
	if entry == nil {
		return nil
	}
	out := make(map[string]int, len(entry.messageIDs))
	for k, v := range entry.messageIDs {
		out[k] = v
	}
	return out
}

func (d *Deduper) ClearByPrefix(prefix string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.entries {
		if strings.HasPrefix(key, prefix) {
			delete(d.entries, key)
		}
	}
}
