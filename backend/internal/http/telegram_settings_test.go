package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
	if err := dbConn.AutoMigrate(&db.TelegramSettings{}, &db.Organization{}, &db.OrganizationMember{}, &db.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE telegram_settings RESTART IDENTITY").Error
	_ = dbConn.Exec("TRUNCATE organization_members RESTART IDENTITY CASCADE").Error
	_ = dbConn.Exec("TRUNCATE organizations RESTART IDENTITY CASCADE").Error
	_ = dbConn.Exec("TRUNCATE users RESTART IDENTITY CASCADE").Error

	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 32))
	enc, err := security.NewEncryptor(key)
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	user := db.User{ID: uuid.New(), Username: "tester", Role: "admin"}
	if err := dbConn.Create(&user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	org := db.Organization{ID: uuid.New(), Name: "Test Org", OwnerUserID: user.ID}
	if err := dbConn.Create(&org).Error; err != nil {
		t.Fatalf("org: %v", err)
	}
	member := db.OrganizationMember{OrgID: org.ID, UserID: user.ID, Role: "owner"}
	if err := dbConn.Create(&member).Error; err != nil {
		t.Fatalf("member: %v", err)
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
		req.Header.Set("X-Org-ID", org.ID.String())
		c.Request = req
		c.Set("actor", user.Username)
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

	_ = dbConn.Create(&db.TelegramSettings{OrgID: &org.ID, BotTokenEnc: "x", AdminChatID: "1"}).Error
	_ = dbConn.Create(&db.TelegramSettings{OrgID: &org.ID, BotTokenEnc: "y", AdminChatID: "2"}).Error
	save()
	if err := dbConn.Model(&db.TelegramSettings{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected cleanup to leave 1 row, got %d", count)
	}
}
