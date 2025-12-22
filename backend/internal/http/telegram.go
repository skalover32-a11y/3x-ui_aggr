package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/db"
)

type telegramSettingsResponse struct {
	BotTokenSet     bool   `json:"bot_token_set"`
	AdminChatID     string `json:"admin_chat_id"`
	AlertConnection bool   `json:"alert_connection"`
	AlertCPU        bool   `json:"alert_cpu"`
	AlertMemory     bool   `json:"alert_memory"`
	AlertDisk       bool   `json:"alert_disk"`
}

type telegramSettingsRequest struct {
	BotToken        string `json:"bot_token"`
	AdminChatID     string `json:"admin_chat_id"`
	AlertConnection bool   `json:"alert_connection"`
	AlertCPU        bool   `json:"alert_cpu"`
	AlertMemory     bool   `json:"alert_memory"`
	AlertDisk       bool   `json:"alert_disk"`
}

func (h *Handler) GetTelegramSettings(c *gin.Context) {
	var row db.TelegramSettings
	err := h.DB.WithContext(c.Request.Context()).Order("created_at desc").First(&row).Error
	if err != nil {
		respondStatus(c, http.StatusOK, telegramSettingsResponse{
			BotTokenSet: false,
		})
		return
	}
	respondStatus(c, http.StatusOK, telegramSettingsResponse{
		BotTokenSet:     row.BotTokenEnc != "",
		AdminChatID:     row.AdminChatID,
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
	botToken := strings.TrimSpace(req.BotToken)

	var current db.TelegramSettings
	_ = h.DB.WithContext(c.Request.Context()).Order("created_at desc").First(&current).Error

	if botToken == "" && current.BotTokenEnc == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_TOKEN", "bot token required")
		return
	}
	if adminChatID == "" && current.AdminChatID == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_CHAT", "admin chat id required")
		return
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
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_SAVE", "failed to save settings")
		return
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
		AlertConnection: req.AlertConnection,
		AlertCPU:        req.AlertCPU,
		AlertMemory:     req.AlertMemory,
		AlertDisk:       req.AlertDisk,
	})
}
