package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

func TestLoginRequiresPasskeyWhenRegistered(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.User{}, &db.WebAuthnCredential{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE users, webauthn_credentials RESTART IDENTITY CASCADE").Error

	hash, err := bcrypt.GenerateFromPassword([]byte("strong-pass-2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("password hash: %v", err)
	}
	user := db.User{
		Username:     "admin2",
		PasswordHash: string(hash),
		Role:         middleware.RoleAdmin,
		TOTPEnabled:  false,
	}
	if err := dbConn.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := dbConn.Create(&db.WebAuthnCredential{
		UserID:       user.Username,
		CredentialID: "cred-1",
		PublicKey:    []byte("pk"),
		SignCount:    1,
		CreatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed passkey: %v", err)
	}

	h := &Handler{
		DB:         dbConn,
		AdminUser:  "envadmin",
		AdminPass:  "envpass",
		JWTSecret:  []byte("secret"),
		JWTExpiry:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	}
	r := NewRouter(h)

	body, _ := json.Marshal(map[string]string{
		"username": user.Username,
		"password": "strong-pass-2",
	})
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &payload)
	errObj, _ := payload["error"].(map[string]any)
	if code, _ := errObj["code"].(string); code != "PASSKEY_REQUIRED" {
		t.Fatalf("expected PASSKEY_REQUIRED, got %v", payload)
	}
}
