package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type telegramUpdate struct {
	UpdateID      int                   `json:"update_id"`
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
	msg, _ := h.Alerts.HandleCallback(c.Request.Context(), token, update.CallbackQuery.Data)
	_ = h.Alerts.AnswerCallback(c.Request.Context(), token, update.CallbackQuery.ID, msg)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
