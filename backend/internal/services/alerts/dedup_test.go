package alerts

import (
	"testing"
	"time"
)

func TestDeduperTrack(t *testing.T) {
	d := NewDeduper(10 * time.Minute)
	alert := Alert{
		Type:        AlertCPU,
		NodeName:    "NODE",
		AlertID:     "a1",
		Fingerprint: "cpu|node|",
	}
	now := time.Now()
	send, entry := d.Track(alert, now)
	if !send || entry == nil || entry.occurrences != 1 {
		t.Fatalf("expected first send with occurrences=1")
	}
	send, entry = d.Track(alert, now.Add(2*time.Minute))
	if send || entry == nil || entry.occurrences != 2 {
		t.Fatalf("expected dedup send=false occurrences=2")
	}
	send, entry = d.Track(alert, now.Add(11*time.Minute))
	if !send || entry == nil || entry.occurrences != 1 {
		t.Fatalf("expected send after TTL reset")
	}
}
