package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

type loginRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	OTP          string `json:"otp"`
	RecoveryCode string `json:"recovery_code"`
}

type loginResponse struct {
	Token         string `json:"token"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	IsGlobalAdmin bool   `json:"is_global_admin"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if !parseJSONBody(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	role := middleware.RoleAdmin
	var loginUser *db.User
	if strings.EqualFold(username, h.AdminUser) {
		if password != h.AdminPass {
			respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
			return
		}
		if _, err := h.EnsureRootOrg(context.Background()); err != nil {
			respondError(c, http.StatusInternalServerError, "ROOT_ORG", "failed to initialize admin workspace")
			return
		}
		var admin db.User
		if err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", h.AdminUser).First(&admin).Error; err == nil {
			loginUser = &admin
			role = admin.Role
			username = admin.Username
		} else {
			username = h.AdminUser
		}
	} else {
		var user db.User
		err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", username).First(&user).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
				return
			}
			respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
			return
		}
		loginUser = &user
		role = user.Role
		username = user.Username
	}
	if loginUser != nil && (role == middleware.RoleAdmin || role == middleware.RoleOperator) {
		if loginUser.TOTPEnabled {
			if strings.TrimSpace(req.OTP) == "" && strings.TrimSpace(req.RecoveryCode) == "" {
				respondError(c, http.StatusUnauthorized, "TOTP_REQUIRED", "otp required")
				return
			}
			if strings.TrimSpace(req.RecoveryCode) != "" {
				if ok := h.verifyRecoveryCode(c, loginUser, req.RecoveryCode); !ok {
					respondError(c, http.StatusUnauthorized, "RECOVERY_INVALID", "recovery code invalid")
					return
				}
				if err := h.disableUserTOTP(c, loginUser); err != nil {
					respondError(c, http.StatusInternalServerError, "TOTP_DISABLE", "failed to reset 2fa")
					return
				}
			} else if !h.verifyTOTPCode(c, loginUser, req.OTP) {
				respondError(c, http.StatusUnauthorized, "TOTP_INVALID", "invalid otp")
				return
			}
		} else {
			var passkeyCount int64
			if err := h.DB.WithContext(c.Request.Context()).
				Model(&db.WebAuthnCredential{}).
				Where("user_id = ?", loginUser.Username).
				Count(&passkeyCount).Error; err != nil {
				respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read passkeys")
				return
			}
			if passkeyCount > 0 {
				respondError(c, http.StatusUnauthorized, "PASSKEY_REQUIRED", "passkey required")
				return
			}
		}
	}
	signed, err := h.issueAccessToken(username, role)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_SIGN", "failed to sign token")
		return
	}
	refreshToken, _, err := h.issueRefreshToken(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "REFRESH_TOKEN", "failed to issue refresh token")
		return
	}
	h.setRefreshCookie(c, refreshToken, h.RefreshTTL)
	respondStatus(c, http.StatusOK, loginResponse{
		Token:         signed,
		Username:      username,
		Role:          role,
		IsGlobalAdmin: strings.EqualFold(username, h.AdminUser),
	})
}
