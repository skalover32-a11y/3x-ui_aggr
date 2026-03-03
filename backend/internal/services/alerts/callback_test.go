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
	"gorm.io/datatypes"

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
		Type:     AlertCPU,
		NodeID:   nodeID,
		NodeName: "node",
		Severity: SeverityCritical,
		TS:       time.Now(),
		Metrics: AlertMetrics{
			Load1:     4.2,
			Threshold: 2.0,
		},
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
	if rt.sendCount != 2 {
		t.Fatalf("expected separate recovery message send, got sends=%d", rt.sendCount)
	}
	if rt.editCount != 0 {
		t.Fatalf("expected no edit for recovery, got %d", rt.editCount)
	}
	var updated db.AlertState
	if err := dbConn.First(&updated, "fingerprint = ?", alert.Fingerprint).Error; err != nil {
		t.Fatalf("load state: %v", err)
	}
	if updated.LastStatus == nil || *updated.LastStatus != "ok" {
		t.Fatalf("expected last_status=ok after recovery")
	}
	if len(messageIDsFromJSON(updated.LastMessageIDs)) != 0 {
		t.Fatalf("expected recovery to clear message thread ids")
	}
}

func TestAlertResendsWhenFailStateHasNoTelegramThread(t *testing.T) {
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
	status := "fail"
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
	alert.Fingerprint = fingerprintFor(alert)
	alertID := uuid.New()
	state := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    alert.Fingerprint,
		AlertType:      string(alert.Type),
		NodeID:         &nodeID,
		LastStatus:     &status,
		FirstSeen:      time.Now().Add(-6 * time.Minute),
		LastSeen:       time.Now().Add(-time.Minute),
		Occurrences:    1,
		LastMessageIDs: datatypes.JSON([]byte("[]")),
		UpdatedAt:      time.Now().Add(-time.Minute),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected one send retry for fail without thread, got %d", rt.sendCount)
	}
	var updated db.AlertState
	if err := dbConn.First(&updated, "fingerprint = ?", alert.Fingerprint).Error; err != nil {
		t.Fatalf("load updated state: %v", err)
	}
	if len(messageIDsFromJSON(updated.LastMessageIDs)) == 0 {
		t.Fatalf("expected message thread to be stored after resend")
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

func TestAlertFallsBackToSendWhenEditFails(t *testing.T) {
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

	rt := &editFailRT{}
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
	alert.Fingerprint = fingerprintFor(alert)
	status := "fail"
	state := db.AlertState{
		Fingerprint:    alert.Fingerprint,
		AlertType:      string(alert.Type),
		NodeID:         &nodeID,
		LastStatus:     &status,
		FirstSeen:      time.Now().Add(-time.Minute),
		LastSeen:       time.Now().Add(-time.Minute),
		Occurrences:    1,
		LastMessageIDs: messageIDsToJSON(map[string]int{"1": 99}),
		UpdatedAt:      time.Now().Add(-time.Minute),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.editCount != 1 {
		t.Fatalf("expected one edit attempt, got %d", rt.editCount)
	}
	if rt.sendCount != 1 {
		t.Fatalf("expected resend after edit failure, got sends=%d", rt.sendCount)
	}
}

func TestGenericAlertSendsAfterOfflineDelay(t *testing.T) {
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
		BotToken:     "token",
		AdminChatIDs: []string{"1"},
	}
	nodeID := uuid.New()
	checkID := uuid.New()
	alert := Alert{
		Type:       AlertGeneric,
		NodeID:     nodeID,
		CheckID:    checkID,
		NodeName:   "node",
		TargetType: "bot",
		CheckType:  "DOCKER",
		Target:     "remnanode",
		Severity:   SeverityCritical,
		TS:         time.Now(),
	}

	// First fail: only persist state, no Telegram.
	alert.Status = "fail"
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no send on first fail, got %d", rt.sendCount)
	}

	// Still within delay window: no send.
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no send before delay window, got %d", rt.sendCount)
	}

	// Move fail start into the past beyond delay threshold.
	if err := dbConn.Model(&db.AlertState{}).
		Where("fingerprint = ?", alert.Fingerprint).
		Update("first_seen", time.Now().Add(-6*time.Minute)).Error; err != nil {
		t.Fatalf("update first_seen: %v", err)
	}

	// Next fail after delay: send alert.
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected send after delay, got %d", rt.sendCount)
	}

	// Recovery after surfaced fail: separate recovery message.
	alert.Status = "ok"
	svc.maybeSendAlert(context.Background(), settings, false, alert)
	if rt.sendCount != 2 {
		t.Fatalf("expected recovery send after surfaced fail, got %d", rt.sendCount)
	}
}

func TestGenericAlertDelayResetsAfterInterimOK(t *testing.T) {
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
		BotToken:     "token",
		AdminChatIDs: []string{"1"},
	}
	nodeID := uuid.New()
	checkID := uuid.New()
	alert := Alert{
		Type:           AlertGeneric,
		NodeID:         nodeID,
		CheckID:        checkID,
		NodeName:       "node",
		TargetType:     "service",
		CheckType:      "HTTP",
		Target:         "http://example.local/health",
		Severity:       SeverityCritical,
		TS:             time.Now(),
		RecoverAfterOK: 2,
	}

	// Start fail window.
	alert.Status = "fail"
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no send on initial fail, got %d", rt.sendCount)
	}

	// Simulate that fail window is already old.
	if err := dbConn.Model(&db.AlertState{}).
		Where("fingerprint = ?", alert.Fingerprint).
		Update("first_seen", time.Now().Add(-6*time.Minute)).Error; err != nil {
		t.Fatalf("update first_seen: %v", err)
	}

	// Single OK is not enough to recover (recover_after_ok=2), but it must reset fail delay.
	alert.Status = "ok"
	svc.maybeSendAlert(context.Background(), settings, false, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no recovery message for unsurfaced fail, got %d", rt.sendCount)
	}

	// Immediate fail after interim OK must not alert yet (delay restarted).
	alert.Status = "fail"
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no send after interim ok reset, got %d", rt.sendCount)
	}

	var state db.AlertState
	if err := dbConn.First(&state, "fingerprint = ?", alert.Fingerprint).Error; err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.FirstSeen.Before(time.Now().Add(-2 * time.Minute)) {
		t.Fatalf("expected first_seen reset on fail after interim ok, got %s", state.FirstSeen.Format(time.RFC3339))
	}
}

func TestGenericAlertRequiresMinConsecutiveFails(t *testing.T) {
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
	svc := New(
		dbConn,
		nil,
		"https://example.com",
		WithPolicy(Policy{
			OfflineDelay:        0,
			MinConsecutiveFails: 2,
		}),
	)
	svc.client = &telegramClient{http: &http.Client{Transport: rt}}
	settings := &Settings{
		BotToken:     "token",
		AdminChatIDs: []string{"1"},
	}
	alert := Alert{
		Type:       AlertGeneric,
		NodeID:     uuid.New(),
		CheckID:    uuid.New(),
		NodeName:   "node",
		TargetType: "service",
		CheckType:  "HTTP",
		Target:     "http://example.local/health",
		Severity:   SeverityCritical,
		TS:         time.Now(),
		Status:     "fail",
	}

	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no send on first fail, got %d", rt.sendCount)
	}
	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected send on second consecutive fail, got %d", rt.sendCount)
	}
}

func TestConnectionRecoveryRequiresConsecutiveOK(t *testing.T) {
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
		BotToken:     "token",
		AdminChatIDs: []string{"1"},
	}
	nodeID := uuid.New()
	alert := Alert{
		Type:       AlertConnection,
		NodeID:     nodeID,
		NodeName:   "node",
		TargetType: "ssh",
		Severity:   SeverityCritical,
		TS:         time.Now(),
		SSHOK:      true,
		PanelOK:    true,
		Status:     "ok",
	}
	alert.Fingerprint = fingerprintFor(alert)
	fail := "fail"
	state := db.AlertState{
		Fingerprint:    alert.Fingerprint,
		AlertType:      string(alert.Type),
		NodeID:         &nodeID,
		LastStatus:     &fail,
		FirstSeen:      time.Now().Add(-10 * time.Minute),
		LastSeen:       time.Now().Add(-time.Minute),
		Occurrences:    5,
		OKStreak:       0,
		LastMessageIDs: messageIDsToJSON(map[string]int{"1": 99}),
		UpdatedAt:      time.Now().Add(-time.Minute),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	svc.maybeSendAlert(context.Background(), settings, false, alert)
	svc.maybeSendAlert(context.Background(), settings, false, alert)
	if rt.sendCount != 0 {
		t.Fatalf("expected no recovery before 3 consecutive ok, got sends=%d", rt.sendCount)
	}

	svc.maybeSendAlert(context.Background(), settings, false, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected recovery on third consecutive ok, got sends=%d", rt.sendCount)
	}
}

func TestAlertResendsWhenChatListChanged(t *testing.T) {
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
		AdminChatIDs:    []string{"new-chat"},
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
	alert.Fingerprint = fingerprintFor(alert)
	status := "fail"
	state := db.AlertState{
		Fingerprint:    alert.Fingerprint,
		AlertType:      string(alert.Type),
		NodeID:         &nodeID,
		LastStatus:     &status,
		FirstSeen:      time.Now().Add(-time.Minute),
		LastSeen:       time.Now().Add(-time.Minute),
		Occurrences:    1,
		LastMessageIDs: messageIDsToJSON(map[string]int{"old-chat": 42}),
		UpdatedAt:      time.Now().Add(-time.Minute),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	svc.maybeSendAlert(context.Background(), settings, true, alert)
	if rt.sendCount != 1 {
		t.Fatalf("expected resend for changed chat list, got sends=%d", rt.sendCount)
	}
}

type editFailRT struct {
	sendCount int
	editCount int
}

func (rt *editFailRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "editMessageText") {
		rt.editCount++
		return &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"message to edit not found"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	if strings.Contains(req.URL.Path, "sendMessage") {
		rt.sendCount++
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":123}}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}
