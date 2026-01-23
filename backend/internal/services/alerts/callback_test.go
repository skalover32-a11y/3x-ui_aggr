package alerts

import (
	"context"
	"os"
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

func strPtr(s string) *string {
	return &s
}
