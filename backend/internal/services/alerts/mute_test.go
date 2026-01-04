package alerts

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingRoundTripper struct {
	count int32
}

func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&c.count, 1)
	body := `{"ok":true,"result":{"message_id":1}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
	return resp, nil
}

func TestMuteSuppressesNotifications(t *testing.T) {
	rt := &countingRoundTripper{}
	svc := &Service{
		client:     &telegramClient{http: &http.Client{Transport: rt}},
		mutedUntil: map[string]time.Time{},
	}
	settings := &Settings{
		BotToken:     "token",
		AdminChatIDs: []string{"1"},
	}
	alert := Alert{
		Type:     AlertGeneric,
		NodeName: "node-1",
		TS:       time.Now(),
		Severity: SeverityCritical,
		Status:   "fail",
		CheckType:"HTTP",
	}
	svc.setMute(fingerprintFor(alert), time.Hour)
	svc.maybeSendAlert(context.Background(), settings, true, alert)

	if atomic.LoadInt32(&rt.count) != 0 {
		t.Fatalf("expected no telegram calls while muted, got %d", atomic.LoadInt32(&rt.count))
	}
}
