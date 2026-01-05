package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

func TestWebAuthnTablesAndEndpoint(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.WebAuthnCredential{}, &db.WebAuthnChallenge{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var credReg string
	if err := dbConn.Raw("SELECT to_regclass('public.webauthn_credentials')").Scan(&credReg).Error; err != nil {
		t.Fatalf("to_regclass credentials: %v", err)
	}
	if credReg == "" {
		t.Fatalf("expected webauthn_credentials table")
	}
	var chalReg string
	if err := dbConn.Raw("SELECT to_regclass('public.webauthn_challenges')").Scan(&chalReg).Error; err != nil {
		t.Fatalf("to_regclass challenges: %v", err)
	}
	if chalReg == "" {
		t.Fatalf("expected webauthn_challenges table")
	}

	secret := []byte("test")
	claims := middleware.Claims{
		Role: "admin",
		User: "tester",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	h := &Handler{
		DB:        dbConn,
		JWTSecret: secret,
	}
	r := NewRouter(h)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/auth/webauthn/credentials", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}
