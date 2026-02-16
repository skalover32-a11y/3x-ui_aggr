package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/alerts"
)

type telegramSettingsResponse struct {
	BotTokenSet       bool     `json:"bot_token_set"`
	AdminChatID       string   `json:"admin_chat_id"`
	AdminChatIDs      []string `json:"admin_chat_ids"`
	AlertConnection   bool     `json:"alert_connection"`
	AlertCPU          bool     `json:"alert_cpu"`
	AlertMemory       bool     `json:"alert_memory"`
	AlertDisk         bool     `json:"alert_disk"`
	AckMuteMinutes    int      `json:"ack_mute_minutes"`
	MuteMinutes       int      `json:"mute_minutes"`
	WebhookConfigured bool     `json:"webhook_configured"`
	WebhookError      string   `json:"webhook_error,omitempty"`
}

type telegramSettingsRequest struct {
	BotToken        string   `json:"bot_token"`
	AdminChatID     string   `json:"admin_chat_id"`
	AdminChatIDs    []string `json:"admin_chat_ids"`
	AlertConnection bool     `json:"alert_connection"`
	AlertCPU        bool     `json:"alert_cpu"`
	AlertMemory     bool     `json:"alert_memory"`
	AlertDisk       bool     `json:"alert_disk"`
	AckMuteMinutes  int      `json:"ack_mute_minutes"`
	MuteMinutes     int      `json:"mute_minutes"`
}

func (h *Handler) GetTelegramSettings(c *gin.Context) {
	orgID, err := h.resolveOrgFromRequest(c, false)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	row, err := h.getTelegramSettings(c, orgID)
	if err != nil {
		respondStatus(c, http.StatusOK, telegramSettingsResponse{
			BotTokenSet:    false,
			AckMuteMinutes: defaultAckMuteMinutes,
			MuteMinutes:    defaultMuteMinutes,
		})
		return
	}
	respondStatus(c, http.StatusOK, telegramSettingsResponse{
		BotTokenSet:     row.BotTokenEnc != "",
		AdminChatID:     row.AdminChatID,
		AdminChatIDs:    splitChatIDs(row.AdminChatID),
		AlertConnection: row.AlertConnection,
		AlertCPU:        row.AlertCPU,
		AlertMemory:     row.AlertMemory,
		AlertDisk:       row.AlertDisk,
		AckMuteMinutes:  normalizePolicyValue(row.AckMuteMinutes, defaultAckMuteMinutes),
		MuteMinutes:     normalizePolicyValue(row.MuteMinutes, defaultMuteMinutes),
	})
}

func (h *Handler) UpdateTelegramSettings(c *gin.Context) {
	var req telegramSettingsRequest
	if !parseJSONBody(c, &req) {
		return
	}
	orgID, err := h.resolveOrgFromRequest(c, true)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	adminChatID := strings.TrimSpace(req.AdminChatID)
	if len(req.AdminChatIDs) > 0 {
		adminChatID = strings.Join(req.AdminChatIDs, ",")
	}
	botToken := strings.TrimSpace(req.BotToken)

	current, err := h.getTelegramSettings(c, orgID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_LOAD", "failed to load settings")
		return
	}

	if botToken == "" && current.BotTokenEnc == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_TOKEN", "bot token required")
		return
	}
	if adminChatID == "" && current.AdminChatID == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_CHAT", "admin chat id required")
		return
	}
	if adminChatID == "" {
		adminChatID = current.AdminChatID
	}
	if botToken == "" && current.BotTokenEnc != "" {
		dec, err := h.Encryptor.DecryptString(current.BotTokenEnc)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to decrypt token")
			return
		}
		botToken = strings.TrimSpace(dec)
	}

	botTokenEnc := current.BotTokenEnc
	if botToken != "" {
		enc, err := h.Encryptor.EncryptString(botToken)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to encrypt token")
			return
		}
		botTokenEnc = enc
	}
	ackMuteMinutes := req.AckMuteMinutes
	if ackMuteMinutes <= 0 {
		ackMuteMinutes = current.AckMuteMinutes
	}
	muteMinutes := req.MuteMinutes
	if muteMinutes <= 0 {
		muteMinutes = current.MuteMinutes
	}
	ackMuteMinutes, muteMinutes = normalizeAlertPolicyMinutes(ackMuteMinutes, muteMinutes)

	row := db.TelegramSettings{
		OrgID:           &orgID,
		BotTokenEnc:     botTokenEnc,
		AdminChatID:     adminChatID,
		AlertConnection: req.AlertConnection,
		AlertCPU:        req.AlertCPU,
		AlertMemory:     req.AlertMemory,
		AlertDisk:       req.AlertDisk,
		AckMuteMinutes:  ackMuteMinutes,
		MuteMinutes:     muteMinutes,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	tx := h.DB.WithContext(c.Request.Context()).Begin()
	var existing db.TelegramSettings
	err = tx.Where("org_id = ?", orgID).Order("created_at desc").First(&existing).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"org_id":           row.OrgID,
			"bot_token_enc":    row.BotTokenEnc,
			"admin_chat_id":    row.AdminChatID,
			"alert_connection": row.AlertConnection,
			"alert_cpu":        row.AlertCPU,
			"alert_memory":     row.AlertMemory,
			"alert_disk":       row.AlertDisk,
			"ack_mute_minutes": row.AckMuteMinutes,
			"mute_minutes":     row.MuteMinutes,
			"updated_at":       time.Now(),
		}
		if err := tx.Model(&db.TelegramSettings{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			_ = tx.Rollback()
			respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
			return
		}
		row = existing
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := tx.Create(&row).Error; err != nil {
			_ = tx.Rollback()
			respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
			return
		}
	default:
		_ = tx.Rollback()
		respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
		return
	}
	if row.ID != uuid.Nil {
		_ = tx.Where("id <> ? AND org_id = ?", row.ID, orgID).Delete(&db.TelegramSettings{}).Error
	}
	if err := tx.Commit().Error; err != nil {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
		return
	}
	webhookConfigured, webhookErr := h.configureTelegramWebhook(c.Request.Context(), orgID, botToken)
	h.auditEvent(c, nil, "TELEGRAM_SETTINGS_UPDATE", "ok", nil, gin.H{
		"admin_chat_id":      adminChatID,
		"alert_connection":   req.AlertConnection,
		"alert_cpu":          req.AlertCPU,
		"alert_memory":       req.AlertMemory,
		"alert_disk":         req.AlertDisk,
		"ack_mute_minutes":   ackMuteMinutes,
		"mute_minutes":       muteMinutes,
		"webhook_configured": webhookConfigured,
		"webhook_error":      webhookErr,
	}, nil)
	respondStatus(c, http.StatusOK, telegramSettingsResponse{
		BotTokenSet:       botTokenEnc != "",
		AdminChatID:       adminChatID,
		AdminChatIDs:      splitChatIDs(adminChatID),
		AlertConnection:   req.AlertConnection,
		AlertCPU:          req.AlertCPU,
		AlertMemory:       req.AlertMemory,
		AlertDisk:         req.AlertDisk,
		AckMuteMinutes:    ackMuteMinutes,
		MuteMinutes:       muteMinutes,
		WebhookConfigured: webhookConfigured,
		WebhookError:      webhookErr,
	})
}

func normalizePolicyValue(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

type telegramSetWebhookRequest struct {
	URL         string `json:"url"`
	SecretToken string `json:"secret_token,omitempty"`
}

type telegramSetWebhookResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

func (h *Handler) configureTelegramWebhook(ctx context.Context, orgID uuid.UUID, botToken string) (bool, string) {
	token := strings.TrimSpace(botToken)
	if token == "" {
		return false, "bot token missing"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(h.PublicBaseURL), "/")
	if baseURL == "" {
		return false, "PUBLIC_BASE_URL is empty"
	}
	secret := h.telegramWebhookSecretFor(orgID, token)
	if secret == "" {
		return false, "failed to generate webhook secret"
	}
	reqBody, err := json.Marshal(telegramSetWebhookRequest{
		URL:         baseURL + "/api/telegram/webhook",
		SecretToken: secret,
	})
	if err != nil {
		return false, fmt.Sprintf("marshal webhook request: %v", err)
	}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return false, fmt.Sprintf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("telegram setWebhook request failed: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Sprintf("read setWebhook response: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("setWebhook HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var body telegramSetWebhookResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return false, fmt.Sprintf("decode setWebhook response: %v", err)
	}
	if !body.OK {
		return false, fmt.Sprintf("setWebhook rejected: %s", strings.TrimSpace(body.Description))
	}
	return true, ""
}

func splitChatIDs(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	var out []string
	for _, part := range parts {
		val := strings.TrimSpace(part)
		if val != "" {
			out = append(out, val)
		}
	}
	return out
}

func (h *Handler) resolveOrgFromRequest(c *gin.Context, requireAdmin bool) (uuid.UUID, error) {
	orgIDStr := strings.TrimSpace(c.GetHeader("X-Org-ID"))
	if orgIDStr == "" {
		orgIDStr = strings.TrimSpace(c.Query("org_id"))
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return uuid.Nil, err
	}
	user, err := h.actorUser(c)
	if err != nil {
		return uuid.Nil, err
	}
	var member db.OrganizationMember
	if err := h.DB.WithContext(c.Request.Context()).
		Where("org_id = ? AND user_id = ?", orgID, user.ID).
		First(&member).Error; err != nil {
		return uuid.Nil, err
	}
	if requireAdmin && member.Role != "owner" && member.Role != "admin" {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	return orgID, nil
}

func (h *Handler) getTelegramSettings(c *gin.Context, orgID uuid.UUID) (db.TelegramSettings, error) {
	var row db.TelegramSettings
	err := h.DB.WithContext(c.Request.Context()).
		Where("org_id = ?", orgID).
		Order("created_at desc").
		First(&row).Error
	if err != nil {
		return db.TelegramSettings{}, err
	}
	return row, nil
}

func sendTelegramMessage(c *gin.Context, token string, chatIDs []string, msg string) []alerts.SendResult {
	settings := &alerts.Settings{
		BotToken:     token,
		AdminChatIDs: chatIDs,
	}
	svc := alerts.New(nil, nil, "")
	return svc.SendTestDetailed(c.Request.Context(), settings, msg)
}
