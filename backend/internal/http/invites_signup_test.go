package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

func seedInvite(t *testing.T, dbConn *gorm.DB, code string, expiresAt time.Time, createdBy uuid.UUID, mode string, targetOrg *uuid.UUID, role string) db.Invite {
	if mode == "" {
		mode = "create_private_stack"
	}
	if role == "" {
		role = "owner"
	}
	invite := db.Invite{
		ID:              uuid.New(),
		Code:            code,
		CreatedByUserID: createdBy,
		TargetOrgID:     targetOrg,
		Mode:            mode,
		Role:            role,
		ExpiresAt:       expiresAt,
		CreatedAt:       time.Now().UTC(),
	}
	if err := dbConn.Create(&invite).Error; err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	return invite
}

func TestSignupSuccessCreatesOrg(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	admin := createUser(t, dbConn, "admin_creator")
	invite := seedInvite(t, dbConn, "INV_TEST", time.Now().Add(1*time.Hour), admin.ID, "create_private_stack", nil, "owner")
	payload := []byte(`{"invite_code":"INV_TEST","username":"newuser","password":"password123"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("signup status: %d %s", resp.Code, resp.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if out.Token == "" || out.Username != "newuser" {
		t.Fatalf("expected token and username")
	}
	var user db.User
	if err := dbConn.First(&user, "lower(username)=lower(?)", "newuser").Error; err != nil {
		t.Fatalf("user not created: %v", err)
	}
	var org db.Organization
	if err := dbConn.First(&org, "owner_user_id = ?", user.ID).Error; err != nil {
		t.Fatalf("org not created: %v", err)
	}
	var member db.OrganizationMember
	if err := dbConn.First(&member, "org_id = ? AND user_id = ?", org.ID, user.ID).Error; err != nil {
		t.Fatalf("membership missing: %v", err)
	}
	var inv db.Invite
	if err := dbConn.First(&inv, "id = ?", invite.ID).Error; err != nil {
		t.Fatalf("invite read: %v", err)
	}
	if inv.UsedAt == nil || inv.UsedByUserID == nil {
		t.Fatalf("invite not marked used")
	}
}

func TestSignupUsedInviteFails(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	now := time.Now().UTC()
	admin := createUser(t, dbConn, "admin_creator")
	invite := seedInvite(t, dbConn, "INV_USED", now.Add(time.Hour), admin.ID, "create_private_stack", nil, "owner")
	if err := dbConn.Model(&db.Invite{}).Where("id = ?", invite.ID).Update("used_at", now).Error; err != nil {
		t.Fatalf("set used_at: %v", err)
	}
	payload := []byte(`{"invite_code":"INV_USED","username":"newuser","password":"password123"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.Code)
	}
}

func TestSignupExpiredInviteFails(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	admin := createUser(t, dbConn, "admin_creator")
	seedInvite(t, dbConn, "INV_EXP", time.Now().Add(-time.Hour), admin.ID, "create_private_stack", nil, "owner")
	payload := []byte(`{"invite_code":"INV_EXP","username":"newuser","password":"password123"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.Code)
	}
}

func TestSignupDuplicateUsernameFails(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	admin := createUser(t, dbConn, "admin_creator")
	seedInvite(t, dbConn, "INV_DUP", time.Now().Add(time.Hour), admin.ID, "create_private_stack", nil, "owner")
	_ = dbConn.Create(&db.User{ID: uuid.New(), Username: "dupe", PasswordHash: "x", Role: middleware.RoleViewer}).Error
	payload := []byte(`{"invite_code":"INV_DUP","username":"dupe","password":"password123"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", resp.Code)
	}
}

func TestAdminInviteRequiresAdmin(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)
	user := createUser(t, dbConn, "bob")
	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleAdmin)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites", bytes.NewReader([]byte(`{"expires_in_hours":24,"mode":"create_private_stack"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.Code)
	}
}

func TestAdminRevokeInvite(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)
	admin := createUser(t, dbConn, h.AdminUser)
	jwtToken := signJWT(h.JWTSecret, admin.Username, middleware.RoleAdmin)
	invite := seedInvite(t, dbConn, "INV_REVOKE", time.Now().Add(time.Hour), admin.ID, "create_private_stack", nil, "owner")
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites/"+invite.ID.String()+"/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("revoke status: %d %s", resp.Code, resp.Body.String())
	}
	var updated db.Invite
	if err := dbConn.First(&updated, "id = ?", invite.ID).Error; err != nil {
		t.Fatalf("invite read: %v", err)
	}
	if updated.UsedAt == nil {
		t.Fatalf("expected used_at set")
	}
}

func TestSignupRateLimit(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	payload := []byte(`{"invite_code":"INV_NONE","username":"rateuser","password":"password123"}`)
	for i := 0; i < 6; i++ {
		resp := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if i < 5 {
			r.ServeHTTP(resp, req)
		} else {
			r.ServeHTTP(resp, req)
			if resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected rate limit, got %d", resp.Code)
			}
		}
	}
}

func TestSignupInviteInvalidFails(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	payload := []byte(`{"invite_code":"INV_NONE","username":"newuser","password":"password123"}`)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.Code)
	}
}

func TestAdminCreateInvite(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	r := NewRouter(h)
	admin := createUser(t, dbConn, h.AdminUser)
	jwtToken := signJWT(h.JWTSecret, admin.Username, middleware.RoleAdmin)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites", bytes.NewReader([]byte(`{"expires_in_hours":24,"mode":"create_private_stack"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected created, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "INV_") {
		t.Fatalf("expected invite code")
	}
}

func TestOrgInviteSignupAddsMember(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	r := NewRouter(h)
	owner := createUser(t, dbConn, "owner1")
	org := db.Organization{ID: uuid.New(), Name: "CustOrg", OwnerUserID: owner.ID, CreatedAt: time.Now().UTC()}
	if err := dbConn.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := dbConn.Create(&db.OrganizationMember{OrgID: org.ID, UserID: owner.ID, Role: "owner", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("create membership: %v", err)
	}

	jwtToken := signJWT(h.JWTSecret, owner.Username, middleware.RoleAdmin)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/orgs/"+org.ID.String()+"/invites", bytes.NewReader([]byte(`{"expires_in_hours":24,"role":"viewer"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("org invite status: %d %s", resp.Code, resp.Body.String())
	}
	var inv inviteResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &inv); err != nil {
		t.Fatalf("parse invite: %v", err)
	}

	payload := []byte(`{"invite_code":"` + inv.Code + `","username":"member1","password":"password123"}`)
	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("signup status: %d %s", resp.Code, resp.Body.String())
	}
	var user db.User
	if err := dbConn.First(&user, "lower(username)=lower(?)", "member1").Error; err != nil {
		t.Fatalf("user not created: %v", err)
	}
	var member db.OrganizationMember
	if err := dbConn.First(&member, "org_id = ? AND user_id = ?", org.ID, user.ID).Error; err != nil {
		t.Fatalf("membership missing: %v", err)
	}
	if member.Role != "viewer" {
		t.Fatalf("expected viewer role, got %s", member.Role)
	}
}

func TestAdminInviteJoinRootStack(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	h := newTestHandler(t, dbConn)
	signupLimiter.reset()
	if _, err := h.EnsureRootOrg(context.Background()); err != nil {
		t.Fatalf("ensure root org: %v", err)
	}
	r := NewRouter(h)
	jwtToken := signJWT(h.JWTSecret, h.AdminUser, middleware.RoleAdmin)

	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites", bytes.NewReader([]byte(`{"expires_in_hours":24,"mode":"join_root_stack","role":"admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("admin invite status: %d %s", resp.Code, resp.Body.String())
	}
	var inv inviteResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &inv); err != nil {
		t.Fatalf("parse invite: %v", err)
	}
	if inv.TargetOrg == nil || *inv.TargetOrg == "" {
		t.Fatalf("expected target org in invite")
	}

	payload := []byte(`{"invite_code":"` + inv.Code + `","username":"partner1","password":"password123"}`)
	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("signup status: %d %s", resp.Code, resp.Body.String())
	}
	var user db.User
	if err := dbConn.First(&user, "lower(username)=lower(?)", "partner1").Error; err != nil {
		t.Fatalf("user not created: %v", err)
	}
	rootID, _ := uuid.Parse(*inv.TargetOrg)
	var member db.OrganizationMember
	if err := dbConn.First(&member, "org_id = ? AND user_id = ?", rootID, user.ID).Error; err != nil {
		t.Fatalf("membership missing: %v", err)
	}
}
