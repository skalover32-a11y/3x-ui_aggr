package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

type fakeWebAuthnRegister struct {
	calls     int
	challenge []byte
}

func (f *fakeWebAuthnRegister) BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	f.calls++
	challengeBytes := f.challenge
	if len(challengeBytes) == 0 {
		challengeBytes = []byte("register-challenge")
	}
	creation := &protocol.CredentialCreation{
		Response: protocol.PublicKeyCredentialCreationOptions{
			Challenge: protocol.URLEncodedBase64(challengeBytes),
			RelyingParty: protocol.RelyingPartyEntity{
				CredentialEntity: protocol.CredentialEntity{
					Name: "Example",
				},
				ID: "example.com",
			},
			User: protocol.UserEntity{
				CredentialEntity: protocol.CredentialEntity{
					Name: "admin",
				},
				ID:          protocol.URLEncodedBase64([]byte("admin")),
				DisplayName: "admin",
			},
			Parameters: []protocol.CredentialParameter{
				{Type: protocol.PublicKeyCredentialType, Algorithm: -7},
			},
		},
	}
	session := &webauthn.SessionData{
		Challenge:      base64.RawURLEncoding.EncodeToString(challengeBytes),
		RelyingPartyID: "example.com",
		UserID:         []byte("admin"),
		Expires:        time.Now().Add(5 * time.Minute),
	}
	return creation, session, nil
}

func (f *fakeWebAuthnRegister) CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	return nil, nil
}

func (f *fakeWebAuthnRegister) BeginLogin(user webauthn.User, opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return nil, nil, nil
}

func (f *fakeWebAuthnRegister) ValidateLogin(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	return nil, nil
}

func TestWebAuthnRegisterOptionsReuse(t *testing.T) {
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
	_ = dbConn.Exec("TRUNCATE webauthn_challenges, webauthn_credentials RESTART IDENTITY").Error

	secret := []byte("test")
	claims := middleware.Claims{
		Role: "admin",
		User: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	fake := &fakeWebAuthnRegister{}
	h := &Handler{
		DB:                  dbConn,
		JWTSecret:           secret,
		WebAuthn:            fake,
		WebAuthnRegisterTTL: 5 * time.Minute,
	}
	r := NewRouter(h)

	callOptions := func() (string, string) {
		resp := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/auth/webauthn/register/options", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("options status: %d %s", resp.Code, resp.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("parse options: %v", err)
		}
		challengeID, _ := payload["challenge_id"].(string)
		pubKey, _ := payload["publicKey"].(map[string]any)
		challenge, _ := pubKey["challenge"].(string)
		return challengeID, challenge
	}

	firstID, firstChallenge := callOptions()
	secondID, secondChallenge := callOptions()

	if firstID == "" || secondID == "" {
		t.Fatalf("expected challenge_id in response")
	}
	if firstID != secondID {
		t.Fatalf("expected same challenge_id, got %s and %s", firstID, secondID)
	}
	if firstChallenge != secondChallenge {
		t.Fatalf("expected same challenge, got %s and %s", firstChallenge, secondChallenge)
	}
	if fake.calls != 1 {
		t.Fatalf("expected BeginRegistration called once, got %d", fake.calls)
	}
}

func TestWebAuthnRegisterVerifyExpired(t *testing.T) {
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
	_ = dbConn.Exec("TRUNCATE webauthn_challenges, webauthn_credentials RESTART IDENTITY").Error

	sessionPayload, _ := json.Marshal(webauthn.SessionData{
		Challenge: "expired",
		Expires:   time.Now().Add(-time.Minute),
	})
	row := db.WebAuthnChallenge{
		UserID:    "admin",
		Type:      "register",
		Challenge: "expired",
		Session:   datatypes.JSON(sessionPayload),
		Options:   datatypes.JSON([]byte(`{"challenge":"expired"}`)),
		CreatedAt: time.Now().Add(-2 * time.Minute),
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := dbConn.Create(&row).Error; err != nil {
		t.Fatalf("seed challenge: %v", err)
	}

	secret := []byte("test")
	claims := middleware.Claims{
		Role: "admin",
		User: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	fake := &fakeWebAuthnRegister{}
	h := &Handler{
		DB:        dbConn,
		JWTSecret: secret,
		WebAuthn:  fake,
	}
	r := NewRouter(h)

	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/auth/webauthn/register/verify", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("verify status: %d %s", resp.Code, resp.Body.String())
	}
	var out ErrorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse error response: %v", err)
	}
	if out.Error.Code != "WEBAUTHN_CHALLENGE_EXPIRED" {
		t.Fatalf("expected WEBAUTHN_CHALLENGE_EXPIRED, got %s", out.Error.Code)
	}
}
