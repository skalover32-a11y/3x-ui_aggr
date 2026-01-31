package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/agentauth"
)

func setupOrgTestDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		t.Fatalf("pgcrypto: %v", err)
	}
	if err := dbConn.Exec("DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'org_role') THEN CREATE TYPE org_role AS ENUM ('owner','admin','viewer'); END IF; END $$;").Error; err != nil {
		t.Fatalf("org_role: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.User{}, &db.Organization{}, &db.OrganizationMember{}, &db.Node{}, &db.NodeRegistrationToken{}, &db.AgentCredential{}, &db.Invite{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE organizations, organization_members, node_registration_tokens, agent_credentials, nodes, users RESTART IDENTITY CASCADE").Error
	return dbConn
}

func newTestHandler(t *testing.T, dbConn *gorm.DB) *Handler {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	enc, err := security.NewEncryptor(key)
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	return &Handler{
		DB:            dbConn,
		Encryptor:     enc,
		JWTSecret:     []byte("test"),
		AdminUser:     "env_admin",
		AdminPass:     "password",
		TokenSalt:     "salt",
		PublicBaseURL: "https://example.test",
	}
}

func signJWT(secret []byte, username, role string) string {
	claims := middleware.Claims{
		Role: role,
		User: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	return token
}

func createUser(t *testing.T, dbConn *gorm.DB, username string) db.User {
	user := db.User{ID: uuid.New(), Username: username, PasswordHash: "x", Role: middleware.RoleAdmin}
	if err := dbConn.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestOrgsCreateAndList(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	user := createUser(t, dbConn, "alice")
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)

	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleAdmin)
	body := []byte(`{"name":"Org One"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/orgs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create org status: %d %s", resp.Code, resp.Body.String())
	}
	var orgResp orgResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &orgResp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if orgResp.ID == "" {
		t.Fatalf("expected org id")
	}
	var member db.OrganizationMember
	if err := dbConn.First(&member, "org_id = ? AND user_id = ?", orgResp.ID, user.ID).Error; err != nil {
		t.Fatalf("missing membership: %v", err)
	}

	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list orgs status: %d %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), orgResp.ID) {
		t.Fatalf("expected org in list")
	}
}

func TestOrgNodeCreateAndRegister(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	user := createUser(t, dbConn, "alice")
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)
	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleAdmin)

	org := db.Organization{ID: uuid.New(), Name: "Org", OwnerUserID: user.ID}
	if err := dbConn.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	member := db.OrganizationMember{OrgID: org.ID, UserID: user.ID, Role: "owner"}
	if err := dbConn.Create(&member).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}

	createBody := []byte(`{"name":"Node A","kind":"HOST","ssh_host":"127.0.0.1","ssh_port":22,"ssh_user":"root","ssh_key":"key"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/orgs/"+org.ID.String()+"/nodes", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create node status: %d %s", resp.Code, resp.Body.String())
	}
	var payload orgNodeCreateResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !strings.HasPrefix(payload.RegistrationToken, "REG_") {
		t.Fatalf("expected registration token")
	}
	if !strings.Contains(payload.InstallCommand, payload.Node.ID) {
		t.Fatalf("expected install command")
	}
	var reg db.NodeRegistrationToken
	if err := dbConn.First(&reg, "node_id = ?", payload.Node.ID).Error; err != nil {
		t.Fatalf("reg token not saved: %v", err)
	}
	if reg.UsedAt != nil {
		t.Fatalf("expected used_at nil")
	}
	if reg.ExpiresAt.Before(time.Now().Add(19 * time.Minute)) {
		t.Fatalf("expected ttl about 20 minutes")
	}
	if reg.TokenHash == payload.RegistrationToken {
		t.Fatalf("expected hashed token")
	}

	registerReq := agentRegisterRequest{NodeID: payload.Node.ID, RegistrationToken: payload.RegistrationToken}
	buf, _ := json.Marshal(registerReq)
	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/agent/register", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("register status: %d %s", resp.Code, resp.Body.String())
	}
	var regResp agentRegisterResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("parse register resp: %v", err)
	}
	if !strings.HasPrefix(regResp.AgentToken, "AGENT_") {
		t.Fatalf("expected agent token")
	}
	var cred db.AgentCredential
	if err := dbConn.First(&cred, "node_id = ?", payload.Node.ID).Error; err != nil {
		t.Fatalf("agent credential missing: %v", err)
	}
	expectedHash := agentauth.HashToken(regResp.AgentToken, h.TokenSalt)
	if cred.TokenHash != expectedHash {
		t.Fatalf("token hash mismatch")
	}

	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/agent/register", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code == http.StatusOK {
		t.Fatalf("expected second register to fail")
	}
}

func TestAgentRevokeAndHeartbeat(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	user := createUser(t, dbConn, "alice")
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)
	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleAdmin)

	org := db.Organization{ID: uuid.New(), Name: "Org", OwnerUserID: user.ID}
	if err := dbConn.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	member := db.OrganizationMember{OrgID: org.ID, UserID: user.ID, Role: "owner"}
	if err := dbConn.Create(&member).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}

	createBody := []byte(`{"name":"Node A","kind":"HOST","ssh_host":"127.0.0.1","ssh_port":22,"ssh_user":"root","ssh_key":"key"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/orgs/"+org.ID.String()+"/nodes", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create node status: %d %s", resp.Code, resp.Body.String())
	}
	var payload orgNodeCreateResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	registerReq := agentRegisterRequest{NodeID: payload.Node.ID, RegistrationToken: payload.RegistrationToken}
	buf, _ := json.Marshal(registerReq)
	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/agent/register", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("register status: %d %s", resp.Code, resp.Body.String())
	}
	var regResp agentRegisterResponse
	_ = json.Unmarshal(resp.Body.Bytes(), &regResp)

	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/orgs/"+org.ID.String()+"/nodes/"+payload.Node.ID+"/agent/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("revoke status: %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/agent/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+regResp.AgentToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized after revoke, got %d", resp.Code)
	}
}

func TestOrgIsolation(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	userA := createUser(t, dbConn, "alice")
	userB := createUser(t, dbConn, "bob")
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)

	orgA := db.Organization{ID: uuid.New(), Name: "OrgA", OwnerUserID: userA.ID}
	orgB := db.Organization{ID: uuid.New(), Name: "OrgB", OwnerUserID: userB.ID}
	if err := dbConn.Create(&orgA).Error; err != nil {
		t.Fatalf("create orgA: %v", err)
	}
	if err := dbConn.Create(&orgB).Error; err != nil {
		t.Fatalf("create orgB: %v", err)
	}
	if err := dbConn.Create(&db.OrganizationMember{OrgID: orgA.ID, UserID: userA.ID, Role: "owner"}).Error; err != nil {
		t.Fatalf("member A: %v", err)
	}
	if err := dbConn.Create(&db.OrganizationMember{OrgID: orgB.ID, UserID: userB.ID, Role: "owner"}).Error; err != nil {
		t.Fatalf("member B: %v", err)
	}

	node := db.Node{
		ID:               uuid.New(),
		OrgID:            &orgA.ID,
		Name:             "Node",
		Kind:             "HOST",
		Tags:             pq.StringArray{},
		Capabilities:     datatypes.JSON([]byte(`{}`)),
		AllowedRoots:     pq.StringArray{},
		AgentEnabled:     false,
		AgentInsecureTLS: false,
		IsEnabled:        true,
		SSHEnabled:       true,
		SSHAuthMethod:    "key",
		BaseURL:          "",
		PanelUsername:    "",
		PanelPasswordEnc: func() string { enc, _ := h.Encryptor.EncryptString(""); return enc }(),
		SSHHost:          "127.0.0.1",
		SSHPort:          22,
		SSHUser:          "root",
		SSHKeyEnc:        func() string { enc, _ := h.Encryptor.EncryptString(""); return enc }(),
		VerifyTLS:        true,
	}
	if err := dbConn.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	jwtToken := signJWT(h.JWTSecret, userB.Username, middleware.RoleAdmin)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/orgs/"+orgA.ID.String()+"/nodes/"+node.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.Code)
	}
}
