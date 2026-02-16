package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/alerts"
	"agr_3x_ui/internal/services/checks"
)

func setupTelegramWebhookTestDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		t.Fatalf("pgcrypto: %v", err)
	}
	if err := dbConn.AutoMigrate(
		&db.Node{},
		&db.Service{},
		&db.Check{},
		&db.CheckResult{},
		&db.AlertState{},
		&db.Incident{},
		&db.TelegramSettings{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE check_results, checks, services, incidents, alert_states, telegram_settings, nodes RESTART IDENTITY CASCADE").Error
	return dbConn
}

func newTelegramWebhookHandler(t *testing.T, dbConn *gorm.DB) *Handler {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32))
	enc, err := security.NewEncryptor(key)
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	return &Handler{
		DB:        dbConn,
		Encryptor: enc,
		Alerts:    alerts.New(dbConn, enc, "https://panel.example"),
		Checks:    checks.New(dbConn, nil, nil, enc, time.Minute),
		TokenSalt: "webhook-test-salt",
	}
}

func seedWebhookNodeAndSettings(t *testing.T, dbConn *gorm.DB, h *Handler, adminChatID string) (uuid.UUID, db.Node, string) {
	orgID := uuid.New()
	node := db.Node{
		ID:               uuid.New(),
		OrgID:            &orgID,
		Name:             "node-1",
		Kind:             "HOST",
		BaseURL:          "https://node.example.test",
		PanelUsername:    "admin",
		PanelPasswordEnc: "enc",
		SSHHost:          "127.0.0.1",
		SSHPort:          22,
		SSHUser:          "root",
		SSHAuthMethod:    "key",
		SSHKeyEnc:        "key",
		VerifyTLS:        true,
		IsEnabled:        true,
		SSHEnabled:       true,
	}
	if err := dbConn.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	token := "telegram-test-token"
	tokenEnc, err := h.Encryptor.EncryptString(token)
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	settings := db.TelegramSettings{
		OrgID:           &orgID,
		BotTokenEnc:     tokenEnc,
		AdminChatID:     adminChatID,
		AlertConnection: true,
		AlertCPU:        true,
		AlertMemory:     true,
		AlertDisk:       true,
	}
	if err := dbConn.Create(&settings).Error; err != nil {
		t.Fatalf("create telegram settings: %v", err)
	}
	return orgID, node, token
}

func callbackPayload(data string, chatID int64) []byte {
	payload := map[string]any{
		"update_id": 1001,
		"callback_query": map[string]any{
			"id":   "cb-1",
			"data": data,
			"from": map[string]any{"id": 42},
			"message": map[string]any{
				"chat": map[string]any{"id": chatID},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func callTelegramWebhook(t *testing.T, h *Handler, payload []byte, secret string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(secret) != "" {
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	}
	c.Request = req
	h.TelegramWebhook(c)
	return w
}

type recordedTelegramRequest struct {
	Method string
	Body   string
}

type telegramMockTransport struct {
	mu       sync.Mutex
	requests []recordedTelegramRequest
}

func (rt *telegramMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	method := req.URL.Path
	if idx := strings.LastIndex(method, "/"); idx >= 0 {
		method = method[idx+1:]
	}
	rt.mu.Lock()
	rt.requests = append(rt.requests, recordedTelegramRequest{
		Method: method,
		Body:   string(body),
	})
	rt.mu.Unlock()

	respBody := `{"ok":true}`
	if method == "sendMessage" || method == "editMessageText" {
		respBody = `{"ok":true,"result":{"message_id":1}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(respBody)),
	}, nil
}

func (rt *telegramMockTransport) count(method string) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	total := 0
	for _, req := range rt.requests {
		if req.Method == method {
			total++
		}
	}
	return total
}

func (rt *telegramMockTransport) lastBody(method string) string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for i := len(rt.requests) - 1; i >= 0; i-- {
		if rt.requests[i].Method == method {
			return rt.requests[i].Body
		}
	}
	return ""
}

func installTelegramTransport(t *testing.T) *telegramMockTransport {
	rt := &telegramMockTransport{}
	prev := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() {
		http.DefaultTransport = prev
	})
	return rt
}

func TestTelegramWebhookAckUpdatesIncident(t *testing.T) {
	dbConn := setupTelegramWebhookTestDB(t)
	h := newTelegramWebhookHandler(t, dbConn)
	rt := installTelegramTransport(t)

	orgID, node, token := seedWebhookNodeAndSettings(t, dbConn, h, "12345")
	alertID := uuid.New()
	incident := db.Incident{
		ID:          uuid.New(),
		OrgID:       &orgID,
		Fingerprint: "fp-ack",
		AlertType:   "connection",
		Severity:    "critical",
		Status:      "open",
		NodeID:      &node.ID,
		Title:       "Node offline",
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		Occurrences: 1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := dbConn.Create(&incident).Error; err != nil {
		t.Fatalf("create incident: %v", err)
	}
	status := "fail"
	state := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    "fp-ack",
		IncidentID:     &incident.ID,
		AlertType:      "connection",
		NodeID:         &node.ID,
		LastStatus:     &status,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
		Occurrences:    1,
		LastMessageIDs: datatypes.JSON([]byte("[]")),
		UpdatedAt:      time.Now(),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("create alert state: %v", err)
	}

	secret := h.telegramWebhookSecretFor(orgID, token)
	resp := callTelegramWebhook(t, h, callbackPayload("a:"+alertID.String(), 12345), secret)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var updated db.Incident
	if err := dbConn.First(&updated, "id = ?", incident.ID).Error; err != nil {
		t.Fatalf("load incident: %v", err)
	}
	if updated.Status != "acked" {
		t.Fatalf("expected incident status acked, got %s", updated.Status)
	}
	if updated.AcknowledgedBy == nil || *updated.AcknowledgedBy != "telegram" {
		t.Fatalf("expected acknowledged_by=telegram, got %#v", updated.AcknowledgedBy)
	}
	if rt.count("answerCallbackQuery") != 1 {
		t.Fatalf("expected one answerCallbackQuery call, got %d", rt.count("answerCallbackQuery"))
	}
	if rt.count("sendMessage") == 0 {
		t.Fatalf("expected sendMessage call for ack confirmation")
	}
}

func TestTelegramWebhookRetryRunsServiceCheck(t *testing.T) {
	dbConn := setupTelegramWebhookTestDB(t)
	h := newTelegramWebhookHandler(t, dbConn)
	rt := installTelegramTransport(t)

	orgID, node, token := seedWebhookNodeAndSettings(t, dbConn, h, "555")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	targetURL := target.URL
	service := db.Service{
		ID:             uuid.New(),
		NodeID:         node.ID,
		Kind:           "CUSTOM_HTTP",
		URL:            &targetURL,
		ExpectedStatus: pq.Int64Array{200},
		Headers:        datatypes.JSON([]byte("{}")),
		IsEnabled:      true,
	}
	if err := dbConn.Create(&service).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	check := db.Check{
		ID:             uuid.New(),
		TargetType:     "service",
		TargetID:       service.ID,
		Type:           "HTTP",
		IntervalSec:    60,
		TimeoutMS:      1000,
		Retries:        0,
		FailAfterSec:   300,
		RecoverAfterOK: 2,
		Enabled:        true,
		SeverityRules:  datatypes.JSON([]byte("{}")),
	}
	if err := dbConn.Create(&check).Error; err != nil {
		t.Fatalf("create check: %v", err)
	}

	alertID := uuid.New()
	status := "fail"
	state := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    "fp-retry",
		AlertType:      "generic",
		NodeID:         &node.ID,
		ServiceID:      &service.ID,
		LastStatus:     &status,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
		Occurrences:    1,
		LastMessageIDs: datatypes.JSON([]byte("[]")),
		UpdatedAt:      time.Now(),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("create alert state: %v", err)
	}

	secret := h.telegramWebhookSecretFor(orgID, token)
	resp := callTelegramWebhook(t, h, callbackPayload("r:"+alertID.String(), 555), secret)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var result db.CheckResult
	if err := dbConn.Where("check_id = ?", check.ID).Order("ts desc").First(&result).Error; err != nil {
		t.Fatalf("load check result: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("expected check result status ok, got %s", result.Status)
	}
	if rt.count("answerCallbackQuery") != 1 {
		t.Fatalf("expected one answerCallbackQuery call, got %d", rt.count("answerCallbackQuery"))
	}
	if rt.count("sendMessage") == 0 {
		t.Fatalf("expected sendMessage call with retry result")
	}
	if body := rt.lastBody("sendMessage"); !strings.Contains(body, "Retry result") {
		t.Fatalf("expected retry result text in sendMessage body, got %s", body)
	}
}

func TestTelegramWebhookRejectsInvalidSecret(t *testing.T) {
	dbConn := setupTelegramWebhookTestDB(t)
	h := newTelegramWebhookHandler(t, dbConn)
	rt := installTelegramTransport(t)

	_, node, _ := seedWebhookNodeAndSettings(t, dbConn, h, "777")
	alertID := uuid.New()
	status := "fail"
	state := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    "fp-auth",
		AlertType:      "connection",
		NodeID:         &node.ID,
		LastStatus:     &status,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
		Occurrences:    1,
		LastMessageIDs: datatypes.JSON([]byte("[]")),
		UpdatedAt:      time.Now(),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("create alert state: %v", err)
	}

	resp := callTelegramWebhook(t, h, callbackPayload("a:"+alertID.String(), 777), "bad-secret")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid secret, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(rt.requests) != 0 {
		t.Fatalf("expected no telegram API calls on auth failure, got %d", len(rt.requests))
	}
}

func TestTelegramWebhookDeniesUnknownChat(t *testing.T) {
	dbConn := setupTelegramWebhookTestDB(t)
	h := newTelegramWebhookHandler(t, dbConn)
	rt := installTelegramTransport(t)

	orgID, node, token := seedWebhookNodeAndSettings(t, dbConn, h, "111")
	alertID := uuid.New()
	incident := db.Incident{
		ID:          uuid.New(),
		OrgID:       &orgID,
		Fingerprint: "fp-chat",
		AlertType:   "connection",
		Severity:    "critical",
		Status:      "open",
		NodeID:      &node.ID,
		Title:       "Node offline",
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		Occurrences: 1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := dbConn.Create(&incident).Error; err != nil {
		t.Fatalf("create incident: %v", err)
	}
	status := "fail"
	state := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    "fp-chat",
		IncidentID:     &incident.ID,
		AlertType:      "connection",
		NodeID:         &node.ID,
		LastStatus:     &status,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
		Occurrences:    1,
		LastMessageIDs: datatypes.JSON([]byte("[]")),
		UpdatedAt:      time.Now(),
	}
	if err := dbConn.Create(&state).Error; err != nil {
		t.Fatalf("create alert state: %v", err)
	}

	secret := h.telegramWebhookSecretFor(orgID, token)
	resp := callTelegramWebhook(t, h, callbackPayload("a:"+alertID.String(), 222), secret)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var updated db.Incident
	if err := dbConn.First(&updated, "id = ?", incident.ID).Error; err != nil {
		t.Fatalf("load incident: %v", err)
	}
	if updated.Status != "open" {
		t.Fatalf("expected incident to remain open, got %s", updated.Status)
	}
	if rt.count("answerCallbackQuery") != 1 {
		t.Fatalf("expected one forbidden answer callback, got %d", rt.count("answerCallbackQuery"))
	}
	if rt.count("sendMessage") != 0 {
		t.Fatalf("expected no sendMessage on forbidden chat, got %d", rt.count("sendMessage"))
	}
	if body := rt.lastBody("answerCallbackQuery"); !strings.Contains(body, "\"forbidden\"") {
		t.Fatalf("expected forbidden callback answer, got %s", body)
	}
}
