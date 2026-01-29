package httpapi

import (
	"errors"
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
	BotTokenSet     bool     `json:"bot_token_set"`
	AdminChatID     string   `json:"admin_chat_id"`
	AdminChatIDs    []string `json:"admin_chat_ids"`
	AlertConnection bool     `json:"alert_connection"`
	AlertCPU        bool     `json:"alert_cpu"`
	AlertMemory     bool     `json:"alert_memory"`
	AlertDisk       bool     `json:"alert_disk"`
}

type telegramSettingsRequest struct {
	BotToken        string   `json:"bot_token"`
	AdminChatID     string   `json:"admin_chat_id"`
	AdminChatIDs    []string `json:"admin_chat_ids"`
	AlertConnection bool     `json:"alert_connection"`
	AlertCPU        bool     `json:"alert_cpu"`
	AlertMemory     bool     `json:"alert_memory"`
	AlertDisk       bool     `json:"alert_disk"`
}

func (h *Handler) GetTelegramSettings(c *gin.Context) {
	row, err := h.getTelegramSettings(c)
	if err != nil {
		respondStatus(c, http.StatusOK, telegramSettingsResponse{
			BotTokenSet: false,
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
	})
}

func (h *Handler) UpdateTelegramSettings(c *gin.Context) {
	var req telegramSettingsRequest
	if !parseJSONBody(c, &req) {
		return
	}
	adminChatID := strings.TrimSpace(req.AdminChatID)
	if len(req.AdminChatIDs) > 0 {
		adminChatID = strings.Join(req.AdminChatIDs, ",")
	}
	botToken := strings.TrimSpace(req.BotToken)

	current, err := h.getTelegramSettings(c)
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

	botTokenEnc := current.BotTokenEnc
	if botToken != "" {
		enc, err := h.Encryptor.EncryptString(botToken)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to encrypt token")
			return
		}
		botTokenEnc = enc
	}

	row := db.TelegramSettings{
		BotTokenEnc:     botTokenEnc,
		AdminChatID:     adminChatID,
		AlertConnection: req.AlertConnection,
		AlertCPU:        req.AlertCPU,
		AlertMemory:     req.AlertMemory,
		AlertDisk:       req.AlertDisk,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	tx := h.DB.WithContext(c.Request.Context()).Begin()
	var existing db.TelegramSettings
	err = tx.Order("created_at desc").First(&existing).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"bot_token_enc":    row.BotTokenEnc,
			"admin_chat_id":    row.AdminChatID,
			"alert_connection": row.AlertConnection,
			"alert_cpu":        row.AlertCPU,
			"alert_memory":     row.AlertMemory,
			"alert_disk":       row.AlertDisk,
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
		_ = tx.Where("id <> ?", row.ID).Delete(&db.TelegramSettings{}).Error
	}
	if err := tx.Commit().Error; err != nil {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
		return
	}
	if h.Alerts != nil && strings.TrimSpace(h.PublicBaseURL) != "" {
		webhookURL := strings.TrimRight(h.PublicBaseURL, "/") + "/api/telegram/webhook"
		tokenToUse := strings.TrimSpace(botToken)
		if tokenToUse == "" {
			tokenToUse = strings.TrimSpace(botTokenEnc)
			if tokenToUse != "" {
				dec, err := h.Encryptor.DecryptString(tokenToUse)
				if err == nil {
					tokenToUse = strings.TrimSpace(dec)
				}
			}
		}
		if tokenToUse != "" {
			if err := h.Alerts.SetWebhook(c.Request.Context(), tokenToUse, webhookURL); err != nil {
				c.Error(err)
			}
		}
	}
	h.auditEvent(c, nil, "TELEGRAM_SETTINGS_UPDATE", "ok", nil, gin.H{
		"admin_chat_id":    adminChatID,
		"alert_connection": req.AlertConnection,
		"alert_cpu":        req.AlertCPU,
		"alert_memory":     req.AlertMemory,
		"alert_disk":       req.AlertDisk,
	}, nil)
	respondStatus(c, http.StatusOK, telegramSettingsResponse{
		BotTokenSet:     botTokenEnc != "",
		AdminChatID:     adminChatID,
		AdminChatIDs:    splitChatIDs(adminChatID),
		AlertConnection: req.AlertConnection,
		AlertCPU:        req.AlertCPU,
		AlertMemory:     req.AlertMemory,
		AlertDisk:       req.AlertDisk,
	})
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

func (h *Handler) getTelegramSettings(c *gin.Context) (db.TelegramSettings, error) {
	var row db.TelegramSettings
	err := h.DB.WithContext(c.Request.Context()).Order("created_at desc").First(&row).Error
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
