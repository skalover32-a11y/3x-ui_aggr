package httpapi

import (
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
	Token    string `json:"token"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if !parseJSONBody(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	role := middleware.RoleAdmin
	if username == h.AdminUser {
		if password != h.AdminPass {
			respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
			return
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
		role = user.Role
		if role == middleware.RoleAdmin || role == middleware.RoleOperator {
			if user.TOTPEnabled {
				if strings.TrimSpace(req.OTP) == "" && strings.TrimSpace(req.RecoveryCode) == "" {
					respondError(c, http.StatusUnauthorized, "TOTP_REQUIRED", "otp required")
					return
				}
				if strings.TrimSpace(req.RecoveryCode) != "" {
					if ok := h.verifyRecoveryCode(c, &user, req.RecoveryCode); !ok {
						respondError(c, http.StatusUnauthorized, "RECOVERY_INVALID", "recovery code invalid")
						return
					}
					if err := h.disableUserTOTP(c, &user); err != nil {
						respondError(c, http.StatusInternalServerError, "TOTP_DISABLE", "failed to reset 2fa")
						return
					}
				} else if !h.verifyTOTPCode(c, &user, req.OTP) {
					respondError(c, http.StatusUnauthorized, "TOTP_INVALID", "invalid otp")
					return
				}
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
	respondStatus(c, http.StatusOK, loginResponse{Token: signed, Username: username, Role: role})
}
