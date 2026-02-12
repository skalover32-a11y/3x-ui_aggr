package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type refreshResponse struct {
	Token         string `json:"token"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	IsGlobalAdmin bool   `json:"is_global_admin"`
}

func (h *Handler) Refresh(c *gin.Context) {
	if !requireXHR(c) {
		respondError(c, http.StatusBadRequest, "CSRF_REQUIRED", "missing csrf header")
		return
	}
	raw := h.readRefreshCookie(c)
	if raw == "" {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing refresh token")
		return
	}
	hash := hashRefreshToken(raw)
	var tokenRow db.RefreshToken
	err := h.DB.WithContext(c.Request.Context()).Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now()).
		First(&tokenRow).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid refresh token")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read refresh token")
		return
	}
	role, err := h.resolveRole(c, tokenRow.UserID)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unknown user")
		return
	}
	if strings.EqualFold(tokenRow.UserID, h.AdminUser) {
		if _, err := h.EnsureRootOrg(context.Background()); err != nil {
			respondError(c, http.StatusInternalServerError, "ROOT_ORG", "failed to initialize admin workspace")
			return
		}
	}
	jwtToken, err := h.issueAccessToken(tokenRow.UserID, role)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_SIGN", "failed to sign token")
		return
	}
	now := time.Now()
	expiry := now.Add(h.RefreshTTL)
	updates := map[string]any{
		"last_used_at": now,
		"expires_at":   expiry,
	}
	_ = h.DB.WithContext(c.Request.Context()).Model(&db.RefreshToken{}).Where("id = ?", tokenRow.ID).Updates(updates).Error
	h.setRefreshCookie(c, raw, h.RefreshTTL)
	respondStatus(c, http.StatusOK, refreshResponse{
		Token:         jwtToken,
		Username:      tokenRow.UserID,
		Role:          role,
		IsGlobalAdmin: strings.EqualFold(tokenRow.UserID, h.AdminUser),
	})
}

func (h *Handler) Logout(c *gin.Context) {
	if !requireXHR(c) {
		respondError(c, http.StatusBadRequest, "CSRF_REQUIRED", "missing csrf header")
		return
	}
	raw := h.readRefreshCookie(c)
	if raw != "" {
		hash := hashRefreshToken(raw)
		now := time.Now()
		_ = h.DB.WithContext(c.Request.Context()).Model(&db.RefreshToken{}).
			Where("token_hash = ? AND revoked_at IS NULL", hash).
			Update("revoked_at", now).Error
	}
	h.clearRefreshCookie(c)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}
