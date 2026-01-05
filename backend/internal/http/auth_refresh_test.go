package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"agr_3x_ui/internal/db"
)

func TestLoginIssuesRefreshToken(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.RefreshToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE auth_refresh_tokens RESTART IDENTITY").Error

	h := &Handler{
		DB:         dbConn,
		AdminUser:  "admin",
		AdminPass:  "admin123",
		JWTSecret:  []byte("secret"),
		JWTExpiry:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	}
	r := NewRouter(h)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin123",
	})
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	foundCookie := false
	cookieValue := ""
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == refreshCookieName {
			foundCookie = true
			cookieValue = cookie.Value
		}
	}
	if !foundCookie {
		t.Fatalf("expected refresh cookie to be set")
	}
	if cookieValue == "" {
		t.Fatalf("expected refresh cookie value to be non-empty")
	}
	var count int64
	if err := dbConn.Model(&db.RefreshToken{}).Count(&count).Error; err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected refresh token row")
	}
	var rawCount int64
	if err := dbConn.Raw("SELECT COUNT(*) FROM auth_refresh_tokens").Scan(&rawCount).Error; err != nil {
		t.Fatalf("count auth_refresh_tokens: %v", err)
	}
	if rawCount == 0 {
		t.Fatalf("expected auth_refresh_tokens row")
	}
}
