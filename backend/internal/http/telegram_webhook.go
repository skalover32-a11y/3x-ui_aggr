package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/alerts"
)

type telegramUpdate struct {
	UpdateID      int                    `json:"update_id"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
}

type telegramCallbackQuery struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

func (h *Handler) TelegramWebhook(c *gin.Context) {
	if h == nil || h.Alerts == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	var update telegramUpdate
	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if update.CallbackQuery == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	row, err := h.getTelegramSettings(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	tokenEnc := strings.TrimSpace(row.BotTokenEnc)
	if tokenEnc == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	token, err := h.Encryptor.DecryptString(tokenEnc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	data := strings.TrimSpace(update.CallbackQuery.Data)
	msg := "OK"
	action, alertID, _ := alerts.ParseCallbackData(data)
	if action == "retry" && alertID != "" {
		if fingerprint, err := h.lookupFingerprintByAlertID(c.Request.Context(), alertID); err == nil && fingerprint != "" {
			if _, err := h.runRetry(c.Request.Context(), fingerprint); err != nil {
				msg = "Retry failed"
			} else {
				msg = "Retry queued"
			}
		} else {
			if _, err := h.runRetry(c.Request.Context(), alertID); err != nil {
				msg = "Retry failed"
			} else {
				msg = "Retry queued"
			}
		}
	} else {
		msg, _ = h.Alerts.HandleCallback(c.Request.Context(), token, data)
	}
	_ = h.Alerts.AnswerCallback(c.Request.Context(), token, update.CallbackQuery.ID, msg)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) lookupFingerprintByAlertID(ctx context.Context, alertID string) (string, error) {
	if h == nil || h.DB == nil || strings.TrimSpace(alertID) == "" {
		return "", nil
	}
	id, err := uuid.Parse(alertID)
	if err != nil {
		return "", err
	}
	var state db.AlertState
	if err := h.DB.WithContext(ctx).Where("alert_id = ?", id).First(&state).Error; err != nil {
		return "", err
	}
	return state.Fingerprint, nil
}
