package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/alerts"
)

type telegramUpdate struct {
	UpdateID      int                    `json:"update_id"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
}

type telegramCallbackQuery struct {
	ID      string                   `json:"id"`
	Data    string                   `json:"data"`
	From    *telegramCallbackFrom    `json:"from,omitempty"`
	Message *telegramCallbackMessage `json:"message,omitempty"`
}

type telegramCallbackMessage struct {
	Chat telegramChat `json:"chat"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramCallbackFrom struct {
	ID int64 `json:"id"`
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
	orgID, err := h.orgIDForAlert(c.Request.Context(), update.CallbackQuery.Data)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	row, err := h.getTelegramSettings(c, orgID)
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
	expectedSecret := h.telegramWebhookSecretFor(orgID, token)
	if !h.telegramWebhookAuthorized(c, expectedSecret) {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false})
		return
	}
	data := strings.TrimSpace(update.CallbackQuery.Data)
	fromID := int64(0)
	if update.CallbackQuery.From != nil {
		fromID = update.CallbackQuery.From.ID
	}
	log.Printf("telegram update_id=%d callback_id=%s from=%d data=%s", update.UpdateID, update.CallbackQuery.ID, fromID, data)
	msg := "OK"
	action, alertID, _ := alerts.ParseCallbackData(data)
	chatID := ""
	if update.CallbackQuery.Message != nil {
		if update.CallbackQuery.Message.Chat.ID != 0 {
			chatID = fmt.Sprintf("%d", update.CallbackQuery.Message.Chat.ID)
		}
	}
	if !telegramChatAllowed(chatID, row.AdminChatID) {
		log.Printf("telegram callback denied: action=%s alert_id=%s chat_id=%s", action, alertID, chatID)
		_ = h.Alerts.AnswerCallback(c.Request.Context(), token, update.CallbackQuery.ID, "forbidden")
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if action == "retry" && alertID != "" {
		if fingerprint, err := h.lookupFingerprintByAlertID(c.Request.Context(), alertID); err == nil && fingerprint != "" {
			log.Printf("telegram callback action=%s alert_id=%s chat_id=%s fingerprint=%s", action, alertID, chatID, fingerprint)
			if result, err := h.runRetry(c.Request.Context(), fingerprint); err != nil {
				msg = "Retry failed"
				_ = h.Alerts.SendMessage(c.Request.Context(), token, chatID, fmt.Sprintf("Retry failed: %s", err.Error()))
			} else {
				msg = "Retry completed"
				_ = h.Alerts.SendMessage(c.Request.Context(), token, chatID, retryStatusMessage(result))
			}
		} else {
			log.Printf("telegram callback action=%s alert_id=%s chat_id=%s fingerprint=%s", action, alertID, chatID, alertID)
			if result, err := h.runRetry(c.Request.Context(), alertID); err != nil {
				msg = "Retry failed"
				_ = h.Alerts.SendMessage(c.Request.Context(), token, chatID, fmt.Sprintf("Retry failed: %s", err.Error()))
			} else {
				msg = "Retry completed"
				_ = h.Alerts.SendMessage(c.Request.Context(), token, chatID, retryStatusMessage(result))
			}
		}
	} else {
		log.Printf("telegram callback action=%s alert_id=%s chat_id=%s", action, alertID, chatID)
		msg, _ = h.Alerts.HandleCallback(c.Request.Context(), token, data)
		if action == "ack" {
			_ = h.Alerts.SendMessage(c.Request.Context(), token, chatID, "Alert acknowledged")
		}
	}
	_ = h.Alerts.AnswerCallback(c.Request.Context(), token, update.CallbackQuery.ID, msg)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) telegramWebhookAuthorized(c *gin.Context, expectedSecret string) bool {
	if h == nil {
		return false
	}
	header := strings.TrimSpace(c.GetHeader("X-Telegram-Bot-Api-Secret-Token"))
	if header == "" {
		return false
	}
	expected := strings.TrimSpace(expectedSecret)
	if expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(header)) == 1 {
		return true
	}
	legacy := strings.TrimSpace(h.TelegramWebhookSecret)
	if legacy != "" && subtle.ConstantTimeCompare([]byte(legacy), []byte(header)) == 1 {
		return true
	}
	return false
}

func (h *Handler) telegramWebhookSecretFor(orgID uuid.UUID, botToken string) string {
	if h == nil {
		return ""
	}
	salt := strings.TrimSpace(h.TokenSalt)
	token := strings.TrimSpace(botToken)
	if salt == "" || token == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(salt))
	_, _ = mac.Write([]byte(orgID.String()))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(token))
	sum := mac.Sum(nil)
	return "tg_" + base64.RawURLEncoding.EncodeToString(sum)
}

func telegramChatAllowed(chatID, rawAllowed string) bool {
	id := strings.TrimSpace(chatID)
	if id == "" {
		return false
	}
	for _, allowed := range splitChatIDs(rawAllowed) {
		if id == strings.TrimSpace(allowed) {
			return true
		}
	}
	return false
}

func retryStatusMessage(result *db.CheckResult) string {
	if result == nil {
		return "Retry completed"
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "unknown"
	}
	latency := "-"
	if result.LatencyMS != nil {
		latency = fmt.Sprintf("%dms", *result.LatencyMS)
	}
	if result.Error != nil && strings.TrimSpace(*result.Error) != "" {
		return fmt.Sprintf("Retry result: %s (%s)\nError: %s", status, latency, strings.TrimSpace(*result.Error))
	}
	return fmt.Sprintf("Retry result: %s (%s)", status, latency)
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

func (h *Handler) orgIDForAlert(ctx context.Context, data string) (uuid.UUID, error) {
	if h == nil || h.DB == nil {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	action, alertID, _ := alerts.ParseCallbackData(strings.TrimSpace(data))
	if action == "" || alertID == "" {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	id, err := uuid.Parse(alertID)
	if err != nil {
		return uuid.Nil, err
	}
	var state db.AlertState
	if err := h.DB.WithContext(ctx).Where("alert_id = ?", id).First(&state).Error; err != nil {
		return uuid.Nil, err
	}
	if state.NodeID != nil {
		var node db.Node
		if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", *state.NodeID).Error; err != nil {
			return uuid.Nil, err
		}
		if node.OrgID == nil {
			return uuid.Nil, gorm.ErrRecordNotFound
		}
		return *node.OrgID, nil
	}
	if state.ServiceID != nil {
		var svc db.Service
		if err := h.DB.WithContext(ctx).Select("org_id", "node_id").First(&svc, "id = ?", *state.ServiceID).Error; err == nil {
			if svc.OrgID != uuid.Nil {
				return svc.OrgID, nil
			}
			if svc.NodeID != nil {
				var node db.Node
				if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", *svc.NodeID).Error; err == nil && node.OrgID != nil {
					return *node.OrgID, nil
				}
			}
		}
	}
	if state.BotID != nil {
		var bot db.Bot
		if err := h.DB.WithContext(ctx).Select("node_id").First(&bot, "id = ?", *state.BotID).Error; err == nil {
			var node db.Node
			if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", bot.NodeID).Error; err == nil && node.OrgID != nil {
				return *node.OrgID, nil
			}
		}
	}
	if state.IncidentID != nil {
		var incident db.Incident
		if err := h.DB.WithContext(ctx).Select("org_id").First(&incident, "id = ?", *state.IncidentID).Error; err == nil && incident.OrgID != nil {
			return *incident.OrgID, nil
		}
	}
	if strings.TrimSpace(state.Fingerprint) != "" {
		var incident db.Incident
		if err := h.DB.WithContext(ctx).Select("org_id").First(&incident, "fingerprint = ?", state.Fingerprint).Error; err == nil && incident.OrgID != nil {
			return *incident.OrgID, nil
		}
	}
	return uuid.Nil, gorm.ErrRecordNotFound
}
