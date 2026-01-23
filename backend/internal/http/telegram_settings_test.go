package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

func TestTelegramSettingsSingleton(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.TelegramSettings{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE telegram_settings RESTART IDENTITY").Error

	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 32))
	enc, err := security.NewEncryptor(key)
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	h := &Handler{DB: dbConn, Encryptor: enc}

	save := func() {
		body, _ := json.Marshal(telegramSettingsRequest{
			BotToken:        "bot-token",
			AdminChatIDs:    []string{"123"},
			AlertConnection: true,
			AlertCPU:        true,
		})
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("PUT", "/telegram/settings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		h.UpdateTelegramSettings(c)
		if w.Code != 200 {
			t.Fatalf("unexpected status: %d", w.Code)
		}
	}

	save()
	save()

	var count int64
	if err := dbConn.Model(&db.TelegramSettings{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}

	_ = dbConn.Create(&db.TelegramSettings{BotTokenEnc: "x", AdminChatID: "1"}).Error
	_ = dbConn.Create(&db.TelegramSettings{BotTokenEnc: "y", AdminChatID: "2"}).Error
	save()
	if err := dbConn.Model(&db.TelegramSettings{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected cleanup to leave 1 row, got %d", count)
	}
}
