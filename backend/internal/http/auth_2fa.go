package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

const totpIssuer = "Server Monitoring Aggregator"

type totpStatusResponse struct {
	Enabled   bool `json:"enabled"`
	Required  bool `json:"required"`
	SecretSet bool `json:"secret_set"`
}

type totpSetupResponse struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
	QRPNG      string `json:"qr_png"`
}

type totpVerifyRequest struct {
	Code string `json:"code"`
}

type totpDisableRequest struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

type recoveryRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) GetTOTPStatus(c *gin.Context) {
	user, isEnvAdmin, err := h.currentUser(c)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_READ", "failed to read user")
		return
	}
	if isEnvAdmin {
		respondStatus(c, http.StatusOK, totpStatusResponse{Enabled: false, Required: false, SecretSet: false})
		return
	}
	required := user.Role == middleware.RoleAdmin || user.Role == middleware.RoleOperator
	secretSet := user.TOTPSecret != nil && strings.TrimSpace(*user.TOTPSecret) != ""
	respondStatus(c, http.StatusOK, totpStatusResponse{
		Enabled:   user.TOTPEnabled,
		Required:  required,
		SecretSet: secretSet,
	})
}

func (h *Handler) SetupTOTP(c *gin.Context) {
	user, isEnvAdmin, err := h.currentUser(c)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_READ", "failed to read user")
		return
	}
	if isEnvAdmin {
		respondError(c, http.StatusBadRequest, "TOTP_ENV_ADMIN", "2fa not available for env admin")
		return
	}
	if user.Role != middleware.RoleAdmin && user.Role != middleware.RoleOperator {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "2fa not allowed for this role")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: user.Username,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_SETUP", "failed to generate secret")
		return
	}
	enc, err := h.Encryptor.EncryptString(key.Secret())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_SECRET", "failed to encrypt secret")
		return
	}
	user.TOTPSecret = &enc
	user.TOTPEnabled = false
	user.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_SAVE", "failed to save secret")
		return
	}
	qrPNG, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_QR", "failed to build qr")
		return
	}
	h.auditEvent(c, nil, "TOTP_SETUP", "ok", nil, gin.H{"user": user.Username}, nil)
	respondStatus(c, http.StatusOK, totpSetupResponse{
		Secret:     key.Secret(),
		OtpauthURL: key.URL(),
		QRPNG:      "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG),
	})
}

func (h *Handler) VerifyTOTP(c *gin.Context) {
	user, isEnvAdmin, err := h.currentUser(c)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_READ", "failed to read user")
		return
	}
	if isEnvAdmin {
		respondError(c, http.StatusBadRequest, "TOTP_ENV_ADMIN", "2fa not available for env admin")
		return
	}
	var req totpVerifyRequest
	if !parseJSONBody(c, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		respondError(c, http.StatusBadRequest, "TOTP_CODE", "code required")
		return
	}
	if !h.verifyTOTPCode(c, &user, code) {
		respondError(c, http.StatusUnauthorized, "TOTP_INVALID", "invalid otp")
		return
	}
	user.TOTPEnabled = true
	user.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_SAVE", "failed to enable 2fa")
		return
	}
	h.auditEvent(c, nil, "TOTP_ENABLE", "ok", nil, gin.H{"user": user.Username}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) DisableTOTP(c *gin.Context) {
	user, isEnvAdmin, err := h.currentUser(c)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_READ", "failed to read user")
		return
	}
	if isEnvAdmin {
		respondError(c, http.StatusBadRequest, "TOTP_ENV_ADMIN", "2fa not available for env admin")
		return
	}
	var req totpDisableRequest
	if !parseJSONBody(c, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	recovery := strings.TrimSpace(req.RecoveryCode)
	if code == "" && recovery == "" {
		respondError(c, http.StatusBadRequest, "TOTP_CODE", "code required")
		return
	}
	if code != "" {
		if !h.verifyTOTPCode(c, &user, code) {
			respondError(c, http.StatusUnauthorized, "TOTP_INVALID", "invalid otp")
			return
		}
	} else {
		if ok := h.verifyRecoveryCode(c, &user, recovery); !ok {
			respondError(c, http.StatusUnauthorized, "RECOVERY_INVALID", "recovery code invalid")
			return
		}
	}
	if err := h.disableUserTOTP(c, &user); err != nil {
		respondError(c, http.StatusInternalServerError, "TOTP_DISABLE", "failed to disable 2fa")
		return
	}
	h.auditEvent(c, nil, "TOTP_DISABLE", "ok", nil, gin.H{"user": user.Username}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) SendRecoveryCode(c *gin.Context) {
	var req recoveryRequest
	if !parseJSONBody(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		respondError(c, http.StatusBadRequest, "INVALID_CREDENTIALS", "username and password required")
		return
	}
	if username == h.AdminUser {
		respondError(c, http.StatusBadRequest, "TOTP_ENV_ADMIN", "2fa not available for env admin")
		return
	}
	var user db.User
	err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", username).First(&user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
		return
	}
	if user.Role != middleware.RoleAdmin && user.Role != middleware.RoleOperator {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "2fa not available for this role")
		return
	}
	orgID, err := h.firstOrgForUser(c, user.ID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "organization not found")
		return
	}
	settingsRow, _ := h.getTelegramSettings(c, orgID)
	ids := splitChatIDs(settingsRow.AdminChatID)
	if len(ids) == 0 {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "admin chat ids missing")
		return
	}
	if settingsRow.BotTokenEnc == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "bot token missing")
		return
	}
	token, err := h.Encryptor.DecryptString(settingsRow.BotTokenEnc)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to decrypt token")
		return
	}
	code, hash, err := generateRecoveryCode()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "RECOVERY_CODE", "failed to generate recovery code")
		return
	}
	exp := time.Now().Add(10 * time.Minute)
	user.RecoveryHash = &hash
	user.RecoveryExp = &exp
	user.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "RECOVERY_SAVE", "failed to save recovery code")
		return
	}
	msg := fmt.Sprintf("Recovery code for user %s: %s (valid 10 minutes)", user.Username, code)
	results := sendTelegramMessage(c, strings.TrimSpace(token), ids, msg)
	okCount := 0
	for _, res := range results {
		if res.OK {
			okCount++
		}
	}
	h.auditEvent(c, nil, "TOTP_RECOVERY_SENT", "ok", nil, gin.H{"user": user.Username}, nil)
	respondStatus(c, http.StatusOK, gin.H{
		"ok":    okCount == len(results),
		"sent":  okCount,
		"total": len(results),
	})
}

func (h *Handler) currentUser(c *gin.Context) (db.User, bool, error) {
	actor := c.GetString("actor")
	if actor == "" {
		return db.User{}, false, fmt.Errorf("missing actor")
	}
	if actor == h.AdminUser {
		return db.User{}, true, nil
	}
	var user db.User
	err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", actor).First(&user).Error
	if err != nil {
		return db.User{}, false, err
	}
	return user, false, nil
}

func (h *Handler) verifyTOTPCode(c *gin.Context, user *db.User, code string) bool {
	if user.TOTPSecret == nil || strings.TrimSpace(*user.TOTPSecret) == "" {
		return false
	}
	dec, err := h.Encryptor.DecryptString(*user.TOTPSecret)
	if err != nil {
		return false
	}
	code = strings.TrimSpace(code)
	ok, err := totp.ValidateCustom(code, strings.TrimSpace(dec), time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false
	}
	return ok
}

func (h *Handler) verifyRecoveryCode(c *gin.Context, user *db.User, code string) bool {
	if user.RecoveryHash == nil || strings.TrimSpace(*user.RecoveryHash) == "" {
		return false
	}
	if user.RecoveryExp == nil || time.Now().After(*user.RecoveryExp) {
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(*user.RecoveryHash), []byte(strings.TrimSpace(code))) != nil {
		return false
	}
	return true
}

func (h *Handler) disableUserTOTP(c *gin.Context, user *db.User) error {
	user.TOTPEnabled = false
	user.TOTPSecret = nil
	user.RecoveryHash = nil
	user.RecoveryExp = nil
	user.UpdatedAt = time.Now()
	return h.DB.WithContext(c.Request.Context()).Save(user).Error
}

func generateRecoveryCode() (string, string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	val := binary.BigEndian.Uint32(buf[:])
	code := fmt.Sprintf("%08d", val%100000000)
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return code, string(hashBytes), nil
}

