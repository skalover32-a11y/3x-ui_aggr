package httpapi

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"

	"agr_3x_ui/internal/db"
)

func TestWebAuthnAAGUIDColumnMapping(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.WebAuthnCredential{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE webauthn_credentials RESTART IDENTITY").Error

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest("POST", "/", nil)

	h := &Handler{DB: dbConn}
	cred := &webauthn.Credential{
		ID:        []byte("cred-1"),
		PublicKey: []byte("pk"),
		Authenticator: webauthn.Authenticator{
			AAGUID:    []byte{1, 2, 3, 4},
			SignCount: 1,
		},
	}
	if err := h.storeWebAuthnCredential(ctx, "admin", cred); err != nil {
		t.Fatalf("store credential: %v", err)
	}

	var colCount int
	if err := dbConn.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'webauthn_credentials' AND column_name = 'aaguid'
	`).Scan(&colCount).Error; err != nil {
		t.Fatalf("check column: %v", err)
	}
	if colCount == 0 {
		t.Fatalf("expected aaguid column")
	}

	var stored string
	if err := dbConn.Raw("SELECT aaguid FROM webauthn_credentials WHERE user_id = ?", "admin").Scan(&stored).Error; err != nil {
		t.Fatalf("select aaguid: %v", err)
	}
	if stored == "" {
		t.Fatalf("expected aaguid value to be stored")
	}
}
