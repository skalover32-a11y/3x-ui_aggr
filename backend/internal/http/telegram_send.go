package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type telegramTestRequest struct {
	Message      string   `json:"message"`
	AdminChatIDs []string `json:"admin_chat_ids"`
	BotToken     string   `json:"bot_token"`
}

func (h *Handler) SendTelegramTest(c *gin.Context) {
	var req telegramTestRequest
	if !parseJSONBody(c, &req) {
		return
	}
	orgID, err := h.resolveOrgFromRequest(c, true)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = "server monitoring Aggregator test message"
	}
	settingsRow, _ := h.getTelegramSettings(c, orgID)
	ids := req.AdminChatIDs
	if len(ids) == 0 {
		ids = splitChatIDs(settingsRow.AdminChatID)
	}
	if len(ids) == 0 {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "admin chat ids missing")
		return
	}
	token := strings.TrimSpace(req.BotToken)
	if token == "" && settingsRow.BotTokenEnc != "" {
		dec, err := h.Encryptor.DecryptString(settingsRow.BotTokenEnc)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "TELEGRAM_TOKEN", "failed to decrypt token")
			return
		}
		token = strings.TrimSpace(dec)
	}
	if token == "" {
		respondError(c, http.StatusBadRequest, "TELEGRAM_SETTINGS", "bot token missing")
		return
	}
	results := sendTelegramMessage(c, token, ids, msg)
	okCount := 0
	for _, res := range results {
		if res.OK {
			okCount++
		}
	}
	respondStatus(c, http.StatusOK, gin.H{
		"ok":      okCount == len(results),
		"sent":    okCount,
		"total":   len(results),
		"results": results,
	})
}

