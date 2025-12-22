package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type telegramTestRequest struct {
	Message string `json:"message"`
}

func (h *Handler) SendTelegramTest(c *gin.Context) {
	var req telegramTestRequest
	if !parseJSONBody(c, &req) {
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = "3x-ui Aggregator test message"
	}
	settingsRow, err := h.getTelegramSettings(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "telegram settings not configured")
		return
	}
	if settingsRow.BotTokenEnc == "" || strings.TrimSpace(settingsRow.AdminChatID) == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "telegram settings not configured")
		return
	}
	token, err := h.Encryptor.DecryptString(settingsRow.BotTokenEnc)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to decrypt token")
		return
	}
	ids := splitChatIDs(settingsRow.AdminChatID)
	if len(ids) == 0 {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "admin chat ids missing")
		return
	}
	if err := sendTelegramMessage(c, token, ids, msg); err != nil {
		respondError(c, http.StatusBadGateway, "TELEGRAM_SEND", err.Error())
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}
