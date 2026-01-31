package httpapi

import (
	"bytes"
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

func seedInvite(t *testing.T, dbConn *gorm.DB, code string, expiresAt time.Time) db.Invite {
	invite := db.Invite{ID: uuid.New(), Code: code, Role: "owner", ExpiresAt: expiresAt, CreatedAt: time.Now().UTC()}
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
	invite := seedInvite(t, dbConn, "INV_TEST", time.Now().Add(1*time.Hour))
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
	invite := seedInvite(t, dbConn, "INV_USED", now.Add(time.Hour))
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
	seedInvite(t, dbConn, "INV_EXP", time.Now().Add(-time.Hour))
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
	seedInvite(t, dbConn, "INV_DUP", time.Now().Add(time.Hour))
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
	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleViewer)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites", bytes.NewReader([]byte(`{"expires_in_hours":24}`)))
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
	admin := createUser(t, dbConn, "alice")
	jwtToken := signJWT(h.JWTSecret, admin.Username, middleware.RoleAdmin)
	invite := seedInvite(t, dbConn, "INV_REVOKE", time.Now().Add(time.Hour))
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
	admin := createUser(t, dbConn, "alice")
	jwtToken := signJWT(h.JWTSecret, admin.Username, middleware.RoleAdmin)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/invites", bytes.NewReader([]byte(`{"expires_in_hours":24}`)))
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
