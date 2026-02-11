package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"

	"golang.org/x/crypto/bcrypt"

	"github.com/pquerna/otp/totp"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/security"
)

func TestLoginWithTOTPFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.User{}, &db.RefreshToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE users, auth_refresh_tokens RESTART IDENTITY").Error

	enc, err := security.NewEncryptor(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "operator1",
	})
	if err != nil {
		t.Fatalf("totp generate: %v", err)
	}
	secretEnc, err := enc.EncryptString(key.Secret())
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("strong-pass-1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("password hash: %v", err)
	}
	user := db.User{
		Username:     "operator1",
		PasswordHash: string(hash),
		Role:         middleware.RoleOperator,
		TOTPEnabled:  true,
		TOTPSecret:   &secretEnc,
	}
	if err := dbConn.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	h := &Handler{
		DB:         dbConn,
		Encryptor:  enc,
		AdminUser:  "envadmin",
		AdminPass:  "envpass",
		JWTSecret:  []byte("secret"),
		JWTExpiry:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	}
	r := NewRouter(h)

	noOtpBody, _ := json.Marshal(map[string]string{
		"username": "operator1",
		"password": "strong-pass-1",
	})
	noOtpResp := httptest.NewRecorder()
	noOtpReq, _ := http.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(noOtpBody))
	noOtpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(noOtpResp, noOtpReq)
	if noOtpResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without otp, got %d: %s", noOtpResp.Code, noOtpResp.Body.String())
	}
	var noOtpPayload map[string]any
	_ = json.Unmarshal(noOtpResp.Body.Bytes(), &noOtpPayload)
	if code, _ := noOtpPayload["error"].(map[string]any)["code"].(string); code != "TOTP_REQUIRED" {
		t.Fatalf("expected TOTP_REQUIRED, got %v", noOtpPayload)
	}

	otpCode, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("generate otp: %v", err)
	}
	otpBody, _ := json.Marshal(map[string]string{
		"username": "operator1",
		"password": "strong-pass-1",
		"otp":      otpCode,
	})
	otpResp := httptest.NewRecorder()
	otpReq, _ := http.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(otpBody))
	otpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(otpResp, otpReq)
	if otpResp.Code != http.StatusOK {
		t.Fatalf("expected 200 with otp, got %d: %s", otpResp.Code, otpResp.Body.String())
	}
	var otpPayload map[string]any
	if err := json.Unmarshal(otpResp.Body.Bytes(), &otpPayload); err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if token, _ := otpPayload["token"].(string); token == "" {
		t.Fatalf("expected token in response")
	}
}
