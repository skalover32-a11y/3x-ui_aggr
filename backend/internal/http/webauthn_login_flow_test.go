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

	"agr_3x_ui/internal/db"
)

type fakeWebAuthn struct {
	challenge string
	credID    []byte
	signCount uint32
}

func (f *fakeWebAuthn) BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	return nil, nil, nil
}

func (f *fakeWebAuthn) CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	return nil, nil
}

func (f *fakeWebAuthn) BeginLogin(user webauthn.User, opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	challengeBytes := []byte("challenge")
	if f.challenge == "" {
		f.challenge = base64.RawURLEncoding.EncodeToString(challengeBytes)
	}
	assertion := &protocol.CredentialAssertion{
		Response: protocol.PublicKeyCredentialRequestOptions{
			Challenge:      protocol.URLEncodedBase64(challengeBytes),
			Timeout:        60000,
			RelyingPartyID: "example.com",
		},
	}
	session := &webauthn.SessionData{
		Challenge:      f.challenge,
		RelyingPartyID: "example.com",
		UserID:         []byte("admin"),
		Expires:        time.Now().Add(time.Minute),
	}
	return assertion, session, nil
}

func (f *fakeWebAuthn) ValidateLogin(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	return &webauthn.Credential{
		ID: f.credID,
		Authenticator: webauthn.Authenticator{
			SignCount: f.signCount,
		},
	}, nil
}

func TestWebAuthnLoginFlowWithChallengeID(t *testing.T) {
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

	credIDBytes := []byte("cred-123")
	credID := NormalizeCredentialID(base64.RawURLEncoding.EncodeToString(credIDBytes))
	if err := dbConn.Create(&db.WebAuthnCredential{
		UserID:       "admin",
		CredentialID: credID,
		PublicKey:    []byte("pk"),
		SignCount:    1,
	}).Error; err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	fake := &fakeWebAuthn{
		credID:    credIDBytes,
		signCount: 2,
	}
	h := &Handler{
		DB:       dbConn,
		WebAuthn: fake,
	}
	r := NewRouter(h)

	optionsResp := httptest.NewRecorder()
	optionsReq, _ := http.NewRequest(http.MethodPost, "/api/auth/webauthn/login/options", bytes.NewReader([]byte(`{"username":"admin"}`)))
	optionsReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(optionsResp, optionsReq)
	if optionsResp.Code != http.StatusOK {
		t.Fatalf("options status: %d %s", optionsResp.Code, optionsResp.Body.String())
	}
	var optsPayload map[string]any
	if err := json.Unmarshal(optionsResp.Body.Bytes(), &optsPayload); err != nil {
		t.Fatalf("parse options: %v", err)
	}
	challengeID, _ := optsPayload["challenge_id"].(string)
	if challengeID == "" {
		t.Fatalf("expected challenge_id in response")
	}

	oldParser := parseAssertion
	parseAssertion = func(data []byte) (*protocol.ParsedCredentialAssertionData, error) {
		return &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				ParsedCredential: protocol.ParsedCredential{
					ID: credID,
				},
				RawID: credIDBytes,
			},
		}, nil
	}
	defer func() { parseAssertion = oldParser }()

	verifyBody := map[string]any{
		"username":     "admin",
		"challenge_id": challengeID,
		"credential": map[string]any{
			"id": credID,
		},
	}
	verifyBytes, _ := json.Marshal(verifyBody)
	verifyResp := httptest.NewRecorder()
	verifyReq, _ := http.NewRequest(http.MethodPost, "/api/auth/webauthn/login/verify", bytes.NewReader(verifyBytes))
	verifyReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(verifyResp, verifyReq)
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("verify status: %d %s", verifyResp.Code, verifyResp.Body.String())
	}

	var updated db.WebAuthnCredential
	if err := dbConn.Where("user_id = ? AND credential_id = ?", "admin", credID).First(&updated).Error; err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if updated.SignCount != int64(fake.signCount) {
		t.Fatalf("expected sign_count %d, got %d", fake.signCount, updated.SignCount)
	}
}
