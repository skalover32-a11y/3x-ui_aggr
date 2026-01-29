package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type telegramClient struct {
	http *http.Client
}

type telegramMessageResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      telegramMessage `json:"result"`
}

type telegramBasicResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

type telegramMessage struct {
	MessageID int `json:"message_id"`
}

type telegramRequest struct {
	ChatID      string          `json:"chat_id"`
	Text        string          `json:"text"`
	ParseMode   string          `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboard `json:"reply_markup,omitempty"`
}

type telegramEditRequest struct {
	ChatID      string          `json:"chat_id"`
	MessageID   int             `json:"message_id"`
	Text        string          `json:"text"`
	ParseMode   string          `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboard `json:"reply_markup,omitempty"`
}

type telegramCallbackAnswer struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
}

func newTelegramClient() *telegramClient {
	return &telegramClient{
		http: &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *telegramClient) SendMessage(ctx context.Context, token, chatID, text, parseMode string, keyboard *InlineKeyboard) (int, error) {
	reqBody := telegramRequest{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	}
	var resp telegramMessageResponse
	if err := c.do(ctx, token, "sendMessage", reqBody, &resp); err != nil {
		return 0, err
	}
	if !resp.OK {
		return 0, fmt.Errorf("telegram send failed: %s", resp.Description)
	}
	return resp.Result.MessageID, nil
}

func (c *telegramClient) EditMessage(ctx context.Context, token, chatID string, messageID int, text, parseMode string, keyboard *InlineKeyboard) error {
	reqBody := telegramEditRequest{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	}
	var resp telegramMessageResponse
	if err := c.do(ctx, token, "editMessageText", reqBody, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram edit failed: %s", resp.Description)
	}
	return nil
}

func (c *telegramClient) AnswerCallback(ctx context.Context, token, callbackID, text string) error {
	reqBody := telegramCallbackAnswer{
		CallbackQueryID: callbackID,
		Text:            text,
	}
	var resp telegramBasicResponse
	if err := c.do(ctx, token, "answerCallbackQuery", reqBody, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram callback failed: %s", resp.Description)
	}
	return nil
}

func (c *telegramClient) do(ctx context.Context, token, method string, payload any, dest any) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("telegram request failed: %s", msg)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return err
	}
	return nil
}
