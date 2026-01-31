package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

type signupRequest struct {
	InviteCode string `json:"invite_code"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

type inviteCreateRequest struct {
	ExpiresInHours int     `json:"expires_in_hours"`
	OrgName        *string `json:"org_name"`
	Role           string  `json:"role"`
}

type inviteResponse struct {
	ID        string     `json:"id"`
	Code      string     `json:"code"`
	OrgName   *string    `json:"org_name,omitempty"`
	Role      string     `json:"role"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type signupRateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	entries map[string]*signupRateEntry
}

type signupRateEntry struct {
	count   int
	resetAt time.Time
}

func newSignupLimiter(limit int, window time.Duration) *signupRateLimiter {
	return &signupRateLimiter{
		limit:   limit,
		window:  window,
		entries: map[string]*signupRateEntry{},
	}
}

func (l *signupRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.resetAt) {
		l.entries[key] = &signupRateEntry{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	return true
}

func (l *signupRateLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = map[string]*signupRateEntry{}
}

var signupLimiter = newSignupLimiter(5, 10*time.Minute)

func (h *Handler) Signup(c *gin.Context) {
	var req signupRequest
	if !parseJSONBody(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	inviteCode := strings.TrimSpace(req.InviteCode)
	if len(username) < 3 || len(password) < 8 || inviteCode == "" {
		respondError(c, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid payload")
		return
	}
	key := strings.ToLower(c.ClientIP() + "|" + username)
	if !signupLimiter.Allow(key) {
		respondError(c, http.StatusTooManyRequests, "RATE_LIMIT", "too many requests")
		return
	}
	var invite db.Invite
	if err := h.DB.WithContext(c.Request.Context()).
		Where("code = ?", inviteCode).
		First(&invite).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusForbidden, "INVITE_INVALID", "invite invalid")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read invite")
		return
	}
	if invite.UsedAt != nil {
		respondError(c, http.StatusForbidden, "INVITE_USED", "invite already used")
		return
	}
	if time.Now().After(invite.ExpiresAt) {
		respondError(c, http.StatusForbidden, "INVITE_EXPIRED", "invite expired")
		return
	}
	var existing db.User
	if err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", username).First(&existing).Error; err == nil {
		respondError(c, http.StatusConflict, "USER_EXISTS", "username already exists")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_PASSWORD", "failed to hash password")
		return
	}
	orgName := "Personal"
	if invite.OrgName != nil && strings.TrimSpace(*invite.OrgName) != "" {
		orgName = strings.TrimSpace(*invite.OrgName)
	}
	orgRole := strings.TrimSpace(invite.Role)
	if orgRole == "" {
		orgRole = "owner"
	}
	user := db.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         middleware.RoleViewer,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	var org db.Organization
	var member db.OrganizationMember
	now := time.Now().UTC()
	if err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		org = db.Organization{ID: uuid.New(), Name: orgName, OwnerUserID: user.ID, CreatedAt: now}
		if err := tx.Create(&org).Error; err != nil {
			return err
		}
		member = db.OrganizationMember{OrgID: org.ID, UserID: user.ID, Role: orgRole, CreatedAt: now}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		return tx.Model(&db.Invite{}).
			Where("id = ? AND used_at IS NULL", invite.ID).
			Updates(map[string]any{"used_at": now, "used_by_user_id": user.ID}).Error
	}); err != nil {
		if isUniqueViolation(err) {
			respondError(c, http.StatusConflict, "USER_EXISTS", "username already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create user")
		return
	}

	token, err := h.issueAccessToken(user.Username, user.Role)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_SIGN", "failed to sign token")
		return
	}
	refreshToken, _, err := h.issueRefreshToken(c, user.Username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "REFRESH_TOKEN", "failed to issue refresh token")
		return
	}
	h.setRefreshCookie(c, refreshToken, h.RefreshTTL)
	respondStatus(c, http.StatusCreated, loginResponse{Token: token, Username: user.Username, Role: user.Role})
}

func (h *Handler) AdminCreateInvite(c *gin.Context) {
	var req inviteCreateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	expiresHours := req.ExpiresInHours
	if expiresHours <= 0 {
		expiresHours = 168
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "owner"
	}
	var created db.Invite
	actor := getActor(c)
	var userID *uuid.UUID
	if !strings.EqualFold(actor, h.AdminUser) {
		if user, err := h.findUserByActor(c, actor); err == nil {
			userID = &user.ID
		}
	}
	for i := 0; i < 5; i++ {
		code, err := generateInviteCode()
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INVITE_CODE", "failed to generate invite")
			return
		}
		created = db.Invite{
			Code:            code,
			CreatedByUserID: userID,
			Role:            role,
			OrgName:         req.OrgName,
			ExpiresAt:       time.Now().Add(time.Duration(expiresHours) * time.Hour),
			CreatedAt:       time.Now().UTC(),
		}
		if err := h.DB.WithContext(c.Request.Context()).Create(&created).Error; err != nil {
			if isUniqueViolation(err) {
				continue
			}
			respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create invite")
			return
		}
		respondStatus(c, http.StatusCreated, inviteResponse{ID: created.ID.String(), Code: created.Code, OrgName: created.OrgName, Role: created.Role, ExpiresAt: created.ExpiresAt, UsedAt: created.UsedAt, CreatedAt: created.CreatedAt})
		return
	}
	respondError(c, http.StatusInternalServerError, "INVITE_CODE", "failed to generate invite")
}

func (h *Handler) AdminListInvites(c *gin.Context) {
	active := strings.TrimSpace(c.Query("active")) == "1"
	query := h.DB.WithContext(c.Request.Context()).Model(&db.Invite{}).Order("created_at desc")
	if active {
		query = query.Where("used_at IS NULL AND expires_at > ?", time.Now())
	}
	var invites []db.Invite
	if err := query.Find(&invites).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list invites")
		return
	}
	resp := make([]inviteResponse, 0, len(invites))
	for _, inv := range invites {
		resp = append(resp, inviteResponse{ID: inv.ID.String(), Code: inv.Code, OrgName: inv.OrgName, Role: inv.Role, ExpiresAt: inv.ExpiresAt, UsedAt: inv.UsedAt, CreatedAt: inv.CreatedAt})
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) AdminRevokeInvite(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ID", "invalid invite id")
		return
	}
	now := time.Now().UTC()
	if err := h.DB.WithContext(c.Request.Context()).Model(&db.Invite{}).
		Where("id = ? AND used_at IS NULL", id).
		Updates(map[string]any{"used_at": now}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to revoke invite")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func generateInviteCode() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "INV_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique")
}
