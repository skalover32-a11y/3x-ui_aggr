package alerts

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

func TestHandleCallbackMuteAndAck(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.AlertState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE alert_states RESTART IDENTITY").Error

	alertID := uuid.New()
	fp := "fp-1"
	state := db.AlertState{
		Fingerprint: fp,
		AlertID:     &alertID,
		AlertType:   "connection",
		Occurrences: 1,
		LastStatus:  strPtr("fail"),
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := New(dbConn, nil, "https://example.com")

	if _, err := svc.HandleCallback(context.Background(), "token", "mute:"+alertID.String()+":60"); err != nil {
		t.Fatalf("mute: %v", err)
	}
	var updated db.AlertState
	if err := dbConn.First(&updated, "fingerprint = ?", fp).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if updated.MutedUntil == nil || updated.MutedUntil.Before(time.Now()) {
		t.Fatalf("expected muted_until to be set")
	}

	if _, err := svc.HandleCallback(context.Background(), "token", "ack:"+alertID.String()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := dbConn.First(&updated, "fingerprint = ?", fp).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if updated.LastStatus == nil || *updated.LastStatus != "ok" {
		t.Fatalf("expected last_status ok")
	}
}

func TestAlertTransitions(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.AlertState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE alert_states RESTART IDENTITY").Error

	rt := &countingRT{}
	svc := New(dbConn, nil, "https://example.com")
	svc.client = &telegramClient{http: &http.Client{Transport: rt}}
	settings := &Settings{
		BotToken:        "token",
		AdminChatIDs:    []string{"1"},
		AlertConnection: true,
	}
	nodeID := uuid.New()
	alert := Alert{
		Type:       AlertConnection,
		NodeID:     nodeID,
		NodeName:   "node",
		TargetType: "ssh",
		Severity:   SeverityCritical,
		TS:         time.Now(),
		SSHOK:      false,
		PanelOK:    true,
	}
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected 1 send, got %d", rt.sendCount)
	}
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected no repeat sends, got %d", rt.sendCount)
	}
	svc.maybeSendAlert(context.Background(), settings, false, alert)
	if rt.editCount != 1 {
		t.Fatalf("expected 1 edit for recovery, got %d", rt.editCount)
	}
}

func TestParseCallbackShort(t *testing.T) {
	action, alertID, minutes := ParseCallbackData("a:123")
	if action != "ack" || alertID != "123" || minutes != 0 {
		t.Fatalf("unexpected ack parse: %s %s %d", action, alertID, minutes)
	}
	action, alertID, minutes = ParseCallbackData("m1:abc")
	if action != "mute" || alertID != "abc" || minutes != 60 {
		t.Fatalf("unexpected mute parse: %s %s %d", action, alertID, minutes)
	}
	action, alertID, minutes = ParseCallbackData("r:zzz")
	if action != "retry" || alertID != "zzz" || minutes != 0 {
		t.Fatalf("unexpected retry parse: %s %s %d", action, alertID, minutes)
	}
	action, alertID, minutes = ParseCallbackData("o:open")
	if action != "open" || alertID != "open" || minutes != 0 {
		t.Fatalf("unexpected open parse: %s %s %d", action, alertID, minutes)
	}
}

type countingRT struct {
	sendCount int
	editCount int
}

func (rt *countingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "sendMessage") {
		rt.sendCount++
	}
	if strings.Contains(req.URL.Path, "editMessageText") {
		rt.editCount++
	}
	body := `{"ok":true,"result":{"message_id":1}}`
	if strings.Contains(req.URL.Path, "answerCallbackQuery") {
		body = `{"ok":true}`
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func strPtr(s string) *string {
	return &s
}
