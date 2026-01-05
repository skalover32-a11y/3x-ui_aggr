package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

const webAuthnChallengeTTL = 5 * time.Minute

type WebAuthnProvider interface {
	BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error)
	CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error)
	BeginLogin(user webauthn.User, opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error)
	ValidateLogin(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error)
}

var parseAssertion = protocol.ParseCredentialRequestResponseBytes
var validateLogin = func(h *Handler, c *gin.Context, user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	return h.validateLoginRelaxed(c, user, session, parsed)
}
var errChallengeExpired = errors.New("challenge expired")

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
	Challenge  string          `json:"challenge_id"`
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
	if _, err := h.saveWebAuthnChallenge(c, username, "register", session); err != nil {
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
	challengeID, err := h.saveWebAuthnChallenge(c, username, "login", session)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_CHALLENGE", "failed to store challenge")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{
		"publicKey":    assertion.Response,
		"options":      assertion.Response,
		"challenge_id": challengeID,
	})
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
	challengeID := ""
	payload := raw
	var wrapper webAuthnVerifyRequest
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Credential) > 0 {
		payload = wrapper.Credential
		username = strings.TrimSpace(wrapper.Username)
		challengeID = strings.TrimSpace(wrapper.Challenge)
	}
	if username == "" {
		username = h.AdminUser
	}
	if challengeID == "" {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_CHALLENGE_NOT_FOUND", "missing challenge_id")
		return
	}
	authUser, err := h.loadWebAuthnUser(c, username)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_USER", "failed to load user")
		return
	}
	session, err := h.loadWebAuthnChallengeByID(c, username, challengeID)
	if err != nil {
		if errors.Is(err, errChallengeExpired) {
			respondError(c, http.StatusBadRequest, "WEBAUTHN_CHALLENGE_EXPIRED", "challenge expired")
			return
		}
		respondError(c, http.StatusBadRequest, "WEBAUTHN_CHALLENGE_NOT_FOUND", "challenge not found")
		return
	}
	defer func() {
		_ = h.deleteWebAuthnChallenge(c, challengeID)
	}()
	parsed, err := parseAssertion(payload)
	if err != nil {
		h.logWebAuthnError(c, username, "", challengeID, session.RelyingPartyID, err)
		respondError(c, http.StatusBadRequest, "WEBAUTHN_ASSERTION_INVALID", "invalid credential")
		return
	}
	credID := NormalizeCredentialID(parsed.ID)
	if credID == "" && len(parsed.RawID) > 0 {
		credID = NormalizeCredentialID(base64.RawURLEncoding.EncodeToString(parsed.RawID))
	}
	if credID == "" {
		respondError(c, http.StatusBadRequest, "WEBAUTHN_CREDENTIAL_NOT_FOUND", "credential id missing")
		return
	}
	if !h.webAuthnCredentialExists(c, username, credID) {
		respondError(c, http.StatusUnauthorized, "WEBAUTHN_CREDENTIAL_NOT_FOUND", "credential not found")
		return
	}
	cred, err := validateLogin(h, c, &authUser, session, parsed)
	if err != nil {
		code, msg := classifyWebAuthnError(err)
		h.logWebAuthnError(c, username, credID, challengeID, session.RelyingPartyID, err)
		respondError(c, http.StatusUnauthorized, code, msg)
		return
	}
	if err := h.touchWebAuthnCredential(c, username, cred); err != nil {
		respondError(c, http.StatusInternalServerError, "WEBAUTHN_SAVE", "failed to update credential")
		return
	}
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
		normalized := NormalizeCredentialID(row.CredentialID)
		idBytes, err := decodeCredentialID(normalized)
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

func (h *Handler) saveWebAuthnChallenge(c *gin.Context, username, typ string, session *webauthn.SessionData) (string, error) {
	if session == nil {
		return "", errors.New("session missing")
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
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		return "", err
	}
	return row.ID.String(), nil
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

func (h *Handler) loadWebAuthnChallengeByID(c *gin.Context, username, challengeID string) (webauthn.SessionData, error) {
	if strings.TrimSpace(challengeID) == "" {
		return webauthn.SessionData{}, gorm.ErrRecordNotFound
	}
	var row db.WebAuthnChallenge
	err := h.DB.WithContext(c.Request.Context()).
		Where("id::text = ?", challengeID).
		First(&row).Error
	if err != nil {
		return webauthn.SessionData{}, err
	}
	if strings.TrimSpace(row.UserID) != "" && strings.TrimSpace(username) != "" && row.UserID != username {
		return webauthn.SessionData{}, gorm.ErrRecordNotFound
	}
	if row.ExpiresAt.Before(time.Now()) {
		return webauthn.SessionData{}, errChallengeExpired
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return webauthn.SessionData{}, err
	}
	return session, nil
}

func (h *Handler) deleteWebAuthnChallenge(c *gin.Context, id string) error {
	return h.DB.WithContext(c.Request.Context()).Where("id::text = ?", id).Delete(&db.WebAuthnChallenge{}).Error
}

func (h *Handler) storeWebAuthnCredential(c *gin.Context, username string, cred *webauthn.Credential) error {
	if cred == nil {
		return errors.New("credential missing")
	}
	credID := NormalizeCredentialID(base64.RawURLEncoding.EncodeToString(cred.ID))
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
	credID := NormalizeCredentialID(base64.RawURLEncoding.EncodeToString(cred.ID))
	now := time.Now()
	updates := map[string]any{
		"sign_count":   int64(cred.Authenticator.SignCount),
		"last_used_at": now,
	}
	return h.DB.WithContext(c.Request.Context()).Model(&db.WebAuthnCredential{}).
		Where("user_id = ? AND credential_id = ?", username, credID).
		Updates(updates).Error
}

func NormalizeCredentialID(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	decoded, err := decodeCredentialID(raw)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(decoded)
}

func decodeCredentialID(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("empty credential id")
	}
	if data, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return data, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

func (h *Handler) webAuthnCredentialExists(c *gin.Context, username, credentialID string) bool {
	var count int64
	_ = h.DB.WithContext(c.Request.Context()).Model(&db.WebAuthnCredential{}).
		Where("user_id = ? AND credential_id = ?", username, credentialID).
		Count(&count).Error
	return count > 0
}

func classifyWebAuthnError(err error) (string, string) {
	if err == nil {
		return "WEBAUTHN_ASSERTION_INVALID", "assertion invalid"
	}
	msg := strings.ToLower(err.Error())
	var pErr *protocol.Error
	if errors.As(err, &pErr) {
		msg = strings.ToLower(pErr.Details + " " + pErr.DevInfo)
	}
	if strings.Contains(msg, "rp hash mismatch") || strings.Contains(msg, "rp id") {
		return "WEBAUTHN_RP_MISMATCH", "rp mismatch"
	}
	if strings.Contains(msg, "origin") {
		return "WEBAUTHN_ORIGIN_MISMATCH", "origin mismatch"
	}
	return "WEBAUTHN_ASSERTION_INVALID", "assertion invalid"
}

func (h *Handler) logWebAuthnError(c *gin.Context, username, credentialID, challengeID, rpID string, err error) {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(c.GetHeader("Referer"))
	}
	if err == nil {
		return
	}
	log.Printf("webauthn error user=%s credential_id=%s challenge_id=%s rp_id=%s origin=%s err=%v", username, credentialID, challengeID, rpID, origin, err)
}

func (h *Handler) webAuthnConfig() *webauthn.Config {
	if h == nil || h.WebAuthn == nil {
		return nil
	}
	if w, ok := h.WebAuthn.(*webauthn.WebAuthn); ok {
		return w.Config
	}
	if provider, ok := h.WebAuthn.(interface{ Config() *webauthn.Config }); ok {
		return provider.Config()
	}
	return nil
}

func (h *Handler) validateLoginRelaxed(c *gin.Context, user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	if user == nil {
		return nil, protocol.ErrBadRequest.WithDetails("user missing")
	}
	if parsed == nil {
		return nil, protocol.ErrBadRequest.WithDetails("assertion missing")
	}
	if !bytes.Equal(user.WebAuthnID(), session.UserID) {
		return nil, protocol.ErrBadRequest.WithDetails("ID mismatch for User and Session")
	}
	if !session.Expires.IsZero() && session.Expires.Before(time.Now()) {
		return nil, protocol.ErrBadRequest.WithDetails("Session has Expired")
	}
	credentials := user.WebAuthnCredentials()
	if len(session.AllowedCredentialIDs) > 0 {
		credentialsOwned := true
		for _, allowed := range session.AllowedCredentialIDs {
			found := false
			for _, credential := range credentials {
				if bytes.Equal(credential.ID, allowed) {
					found = true
					break
				}
			}
			if !found {
				credentialsOwned = false
				break
			}
		}
		if !credentialsOwned {
			return nil, protocol.ErrBadRequest.WithDetails("User does not own all credentials from the allowedCredentialList")
		}
		found := false
		for _, allowed := range session.AllowedCredentialIDs {
			if bytes.Equal(parsed.RawID, allowed) {
				found = true
				break
			}
		}
		if !found {
			return nil, protocol.ErrBadRequest.WithDetails("User does not own the credential returned")
		}
	}
	if len(parsed.Response.UserHandle) > 0 && !bytes.Equal(parsed.Response.UserHandle, user.WebAuthnID()) {
		return nil, protocol.ErrBadRequest.WithDetails("userHandle and User ID do not match")
	}
	var credential webauthn.Credential
	found := false
	for _, c := range credentials {
		if bytes.Equal(c.ID, parsed.RawID) {
			credential = c
			found = true
			break
		}
	}
	if !found {
		return nil, protocol.ErrBadRequest.WithDetails("Unable to find the credential for the returned credential ID")
	}
	cfg := h.webAuthnConfig()
	if cfg == nil {
		return nil, protocol.ErrBadRequest.WithDetails("webauthn config missing")
	}
	if cfg.MDS != nil {
		aaguid, err := uuid.FromBytes(credential.Authenticator.AAGUID)
		if err != nil {
			return nil, protocol.ErrBadRequest.WithDetails("Failed to decode AAGUID").WithInfo(fmt.Sprintf("Error occurred decoding AAGUID from the credential record: %s", err))
		}
		if err := protocol.ValidateMetadata(context.Background(), aaguid, cfg.MDS); err != nil {
			return nil, protocol.ErrBadRequest.WithDetails("Failed to validate credential record metadata").WithInfo(fmt.Sprintf("Error occurred validating authenticator metadata from the credential record: %s", err))
		}
	}
	shouldVerifyUser := session.UserVerification == protocol.VerificationRequired
	appID, err := parsed.GetAppID(session.Extensions, credential.AttestationType)
	if err != nil {
		return nil, err
	}
	if err := parsed.Verify(session.Challenge, cfg.RPID, cfg.RPOrigins, cfg.RPTopOrigins, cfg.RPTopOriginVerificationMode, appID, shouldVerifyUser, credential.PublicKey); err != nil {
		return nil, err
	}
	credential.Authenticator.UpdateCounter(parsed.Response.AuthenticatorData.Counter)
	if !parsed.Response.AuthenticatorData.Flags.HasBackupEligible() && parsed.Response.AuthenticatorData.Flags.HasBackupState() {
		return nil, protocol.ErrBadRequest.WithDetails("Invalid flag combination: BE=0 and BS=1")
	}
	credential.Flags.UserPresent = parsed.Response.AuthenticatorData.Flags.HasUserPresent()
	credential.Flags.UserVerified = parsed.Response.AuthenticatorData.Flags.HasUserVerified()
	credential.Flags.BackupEligible = parsed.Response.AuthenticatorData.Flags.HasBackupEligible()
	credential.Flags.BackupState = parsed.Response.AuthenticatorData.Flags.HasBackupState()
	return &credential, nil
}
