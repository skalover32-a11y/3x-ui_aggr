package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
)

const webAuthnChallengeTTL = 5 * time.Minute

type webAuthnUser struct {
	ID          string
	Name        string
	DisplayName string
	Creds       []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte {
	return []byte(u.ID)
}

func (u *webAuthnUser) WebAuthnName() string {
	return u.Name
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	return u.DisplayName
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Creds
}

type webAuthnRegisterOptionsRequest struct {
	OTP string `json:"otp"`
}

type webAuthnVerifyRequest struct {
	Username   string          `json:"username"`
	Credential json.RawMessage `json:"credential"`
}

type webAuthnCredentialResponse struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	AAGUID     *string    `json:"aaguid"`
	Transports []string   `json:"transports"`
}

func (h *Handler) WebAuthnRegisterOptions(c *gin.Context) {
	if h.WebAuthn == nil {
		respondError(c, http.StatusServiceUnavailable, "WEBAUTHN_DISABLED", "webauthn not configured")
		return
	}
	var req webAuthnRegisterOptionsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			respondError(c, http.StatusBadRequest, "INVALID_JSON", "invalid json")
			return
		}
	}
	user, isAdmin, err := h.currentUser(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}
	if !isAdmin && user.TOTPEnabled {
		if strings.TrimSpace(req.OTP) == "" {
			respondError(c, http.StatusUnauthorized, "TOTP_REQUIRED", "otp required")
			return
		}
		if !h.verifyTOTPCode(c, &user, req.OTP) {
			respondError(c, http.StatusUnauthorized, "TOTP_INVALID", "invalid otp")
			return
		}
	}
	username := h.AdminUser
	if !isAdmin {
		username = user.Username
	}
	authUser, err := h.loadWebAuthnUser(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_USER", "failed to load user")
		return
	}
	creation, session, err := h.WebAuthn.BeginRegistration(&authUser, webauthn.WithConveyancePreference(protocol.PreferNoAttestation))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_OPTIONS", "failed to create options")
		return
	}
	session.Expires = time.Now().Add(webAuthnChallengeTTL)
	if err := h.saveWebAuthnChallenge(c, username, "register", session); err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_CHALLENGE", "failed to store challenge")
		return
	}
	respondStatus(c, http.StatusOK, creation.Response)
}

func (h *Handler) WebAuthnRegisterVerify(c *gin.Context) {
	if h.WebAuthn == nil {
		respondError(c, http.StatusServiceUnavailable, "WEBAUTHN_DISABLED", "webauthn not configured")
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_JSON", "invalid json")
		return
	}
	username := getActor(c)
	payload := raw
	var wrapper webAuthnVerifyRequest
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Credential) > 0 {
		payload = wrapper.Credential
	}
	authUser, err := h.loadWebAuthnUser(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_USER", "failed to load user")
		return
	}
	session, challengeID, err := h.loadWebAuthnChallenge(c, username, "register")
	if err != nil {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_CHALLENGE", "challenge expired")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_PARSE", "invalid credential")
		return
	}
	cred, err := h.WebAuthn.CreateCredential(&authUser, session, parsed)
	if err != nil {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_VERIFY", "credential verify failed")
		return
	}
	if err := h.storeWebAuthnCredential(c, username, cred); err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_SAVE", "failed to store credential")
		return
	}
	_ = h.deleteWebAuthnChallenge(c, challengeID)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) WebAuthnLoginOptions(c *gin.Context) {
	if h.WebAuthn == nil {
		respondError(c, http.StatusServiceUnavailable, "WEBAUTHN_DISABLED", "webauthn not configured")
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			respondError(c, http.StatusBadRequest, "INVALID_JSON", "invalid json")
			return
		}
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = h.AdminUser
	}
	authUser, err := h.loadWebAuthnUser(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_USER", "failed to load user")
		return
	}
	if len(authUser.Creds) == 0 {
		respondError(c, http.StatusNotFound, "NO_CREDENTIALS", "no credentials registered")
		return
	}
	assertion, session, err := h.WebAuthn.BeginLogin(&authUser)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_OPTIONS", "failed to create options")
		return
	}
	session.Expires = time.Now().Add(webAuthnChallengeTTL)
	if err := h.saveWebAuthnChallenge(c, username, "login", session); err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_CHALLENGE", "failed to store challenge")
		return
	}
	respondStatus(c, http.StatusOK, assertion.Response)
}

func (h *Handler) WebAuthnLoginVerify(c *gin.Context) {
	if h.WebAuthn == nil {
		respondError(c, http.StatusServiceUnavailable, "WEBAUTHN_DISABLED", "webauthn not configured")
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_JSON", "invalid json")
		return
	}
	username := ""
	payload := raw
	var wrapper webAuthnVerifyRequest
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Credential) > 0 {
		payload = wrapper.Credential
		username = strings.TrimSpace(wrapper.Username)
	}
	if username == "" {
		username = h.AdminUser
	}
	authUser, err := h.loadWebAuthnUser(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_USER", "failed to load user")
		return
	}
	session, challengeID, err := h.loadWebAuthnChallenge(c, username, "login")
	if err != nil {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_CHALLENGE", "challenge expired")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_PARSE", "invalid credential")
		return
	}
	cred, err := h.WebAuthn.ValidateLogin(&authUser, session, parsed)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "WEBAUTHN_VERIFY", "assertion failed")
		return
	}
	if err := h.touchWebAuthnCredential(c, username, cred); err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_SAVE", "failed to update credential")
		return
	}
	_ = h.deleteWebAuthnChallenge(c, challengeID)
	role, err := h.resolveRole(c, username)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unknown user")
		return
	}
	jwtToken, err := h.issueAccessToken(username, role)
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
	respondStatus(c, http.StatusOK, loginResponse{Token: jwtToken, Username: username, Role: role})
}

func (h *Handler) ListWebAuthnCredentials(c *gin.Context) {
	actor := getActor(c)
	var rows []db.WebAuthnCredential
	if err := h.DB.WithContext(c.Request.Context()).Where("user_id = ?", actor).Order("created_at desc").Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read credentials")
		return
	}
	out := make([]webAuthnCredentialResponse, 0, len(rows))
	for _, row := range rows {
		transports := make([]string, 0, len(row.Transports))
		for _, t := range row.Transports {
			if strings.TrimSpace(t) != "" {
				transports = append(transports, t)
			}
		}
		out = append(out, webAuthnCredentialResponse{
			ID:         row.ID.String(),
			UserID:     row.UserID,
			CreatedAt:  row.CreatedAt,
			LastUsedAt: row.LastUsedAt,
			AAGUID:     row.AAGUID,
			Transports: transports,
		})
	}
	respondStatus(c, http.StatusOK, out)
}

func (h *Handler) DeleteWebAuthnCredential(c *gin.Context) {
	actor := getActor(c)
	credID := strings.TrimSpace(c.Param("id"))
	if credID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ID", "missing credential id")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Where("user_id = ? AND id::text = ?", actor, credID).
		Delete(&db.WebAuthnCredential{}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete credential")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) loadWebAuthnUser(c *gin.Context, username string) (webAuthnUser, error) {
	var creds []db.WebAuthnCredential
	if err := h.DB.WithContext(c.Request.Context()).Where("user_id = ?", username).Find(&creds).Error; err != nil {
		return webAuthnUser{}, err
	}
	authCreds := make([]webauthn.Credential, 0, len(creds))
	for _, row := range creds {
		idBytes, err := base64.RawURLEncoding.DecodeString(row.CredentialID)
		if err != nil {
			continue
		}
		transports := make([]protocol.AuthenticatorTransport, 0, len(row.Transports))
		for _, t := range row.Transports {
			val := strings.TrimSpace(t)
			if val == "" {
				continue
			}
			transports = append(transports, protocol.AuthenticatorTransport(val))
		}
		var aaguid []byte
		if row.AAGUID != nil && strings.TrimSpace(*row.AAGUID) != "" {
			if decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(*row.AAGUID)); err == nil {
				aaguid = decoded
			}
		}
		authCreds = append(authCreds, webauthn.Credential{
			ID:        idBytes,
			PublicKey: row.PublicKey,
			Transport: transports,
			Authenticator: webauthn.Authenticator{
				SignCount: uint32(row.SignCount),
				AAGUID:    aaguid,
			},
		})
	}
	return webAuthnUser{
		ID:          username,
		Name:        username,
		DisplayName: username,
		Creds:       authCreds,
	}, nil
}

func (h *Handler) saveWebAuthnChallenge(c *gin.Context, username, typ string, session *webauthn.SessionData) error {
	if session == nil {
		return errors.New("session missing")
	}
	now := time.Now()
	_ = h.DB.WithContext(c.Request.Context()).Where("expires_at < ?", now).Delete(&db.WebAuthnChallenge{}).Error
	payload, _ := json.Marshal(session)
	row := db.WebAuthnChallenge{
		UserID:    username,
		Type:      typ,
		Challenge: session.Challenge,
		Session:   datatypes.JSON(payload),
		CreatedAt: now,
		ExpiresAt: session.Expires,
	}
	return h.DB.WithContext(c.Request.Context()).Create(&row).Error
}

func (h *Handler) loadWebAuthnChallenge(c *gin.Context, username, typ string) (webauthn.SessionData, string, error) {
	now := time.Now()
	var row db.WebAuthnChallenge
	err := h.DB.WithContext(c.Request.Context()).
		Where("user_id = ? AND type = ? AND expires_at > ?", username, typ, now).
		Order("created_at desc").
		First(&row).Error
	if err != nil {
		return webauthn.SessionData{}, "", err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return webauthn.SessionData{}, "", err
	}
	return session, row.ID.String(), nil
}

func (h *Handler) deleteWebAuthnChallenge(c *gin.Context, id string) error {
	return h.DB.WithContext(c.Request.Context()).Where("id::text = ?", id).Delete(&db.WebAuthnChallenge{}).Error
}

func (h *Handler) storeWebAuthnCredential(c *gin.Context, username string, cred *webauthn.Credential) error {
	if cred == nil {
		return errors.New("credential missing")
	}
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		val := strings.TrimSpace(string(t))
		if val != "" {
			transports = append(transports, val)
		}
	}
	var aaguid *string
	if len(cred.Authenticator.AAGUID) > 0 {
		val := base64.RawURLEncoding.EncodeToString(cred.Authenticator.AAGUID)
		aaguid = &val
	}
	row := db.WebAuthnCredential{
		UserID:       username,
		CredentialID: credID,
		PublicKey:    cred.PublicKey,
		SignCount:    int64(cred.Authenticator.SignCount),
		Transports:   transports,
		AAGUID:       aaguid,
		CreatedAt:    time.Now(),
	}
	return h.DB.WithContext(c.Request.Context()).Create(&row).Error
}

func (h *Handler) touchWebAuthnCredential(c *gin.Context, username string, cred *webauthn.Credential) error {
	if cred == nil {
		return errors.New("credential missing")
	}
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	now := time.Now()
	updates := map[string]any{
		"sign_count":   int64(cred.Authenticator.SignCount),
		"last_used_at": now,
	}
	return h.DB.WithContext(c.Request.Context()).Model(&db.WebAuthnCredential{}).
		Where("user_id = ? AND credential_id = ?", username, credID).
		Updates(updates).Error
}
