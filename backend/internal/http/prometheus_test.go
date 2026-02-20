package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

func TestPrometheusSettingsSingletonAndSecretReuse(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.PrometheusSettings{}, &db.Organization{}, &db.OrganizationMember{}, &db.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE prometheus_settings RESTART IDENTITY").Error
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
	save := func(payload prometheusSettingsRequest) {
		body, _ := json.Marshal(payload)
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPut, "/prometheus/settings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Org-ID", org.ID.String())
		c.Request = req
		c.Set("actor", user.Username)
		h.UpdatePrometheusSettings(c)
		if w.Code != http.StatusOK {
			t.Fatalf("unexpected save status: %d body=%s", w.Code, w.Body.String())
		}
	}

	save(prometheusSettingsRequest{
		Enabled:               true,
		BaseURL:               "http://prometheus.internal:9090/",
		AuthType:              "bearer",
		BearerToken:           "secret-token",
		TLSInsecureSkipVerify: true,
		TimeoutMS:             7000,
		DefaultStepSec:        30,
	})
	save(prometheusSettingsRequest{
		Enabled:               true,
		BaseURL:               "http://prometheus.internal:9090",
		AuthType:              "bearer",
		BearerToken:           "",
		TLSInsecureSkipVerify: true,
		TimeoutMS:             8000,
		DefaultStepSec:        45,
	})

	var count int64
	if err := dbConn.Model(&db.PrometheusSettings{}).Where("org_id = ?", org.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}

	var row db.PrometheusSettings
	if err := dbConn.Where("org_id = ?", org.ID).First(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.BearerTokenEnc == "" {
		t.Fatalf("expected bearer token to stay encrypted")
	}
	token, err := enc.DecryptString(row.BearerTokenEnc)
	if err != nil {
		t.Fatalf("decrypt bearer token: %v", err)
	}
	if token != "secret-token" {
		t.Fatalf("unexpected bearer token value: %q", token)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/prometheus/settings", nil)
	req.Header.Set("X-Org-ID", org.ID.String())
	c.Request = req
	c.Set("actor", user.Username)
	h.GetPrometheusSettings(c)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected get status: %d", w.Code)
	}
	var resp prometheusSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.BearerTokenSet {
		t.Fatalf("expected bearer_token_set=true")
	}
	if resp.AuthType != "bearer" {
		t.Fatalf("expected auth_type=bearer, got %q", resp.AuthType)
	}
	if resp.TimeoutMS != 8000 {
		t.Fatalf("expected timeout_ms=8000, got %d", resp.TimeoutMS)
	}
}

func TestPrometheusQueryUsesStoredCredentials(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.PrometheusSettings{}, &db.Organization{}, &db.OrganizationMember{}, &db.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE prometheus_settings RESTART IDENTITY").Error
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

	var (
		authHeaderMu sync.Mutex
		authHeader   string
	)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderMu.Lock()
		authHeader = r.Header.Get("Authorization")
		authHeaderMu.Unlock()
		if r.URL.Path != "/api/v1/query" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{
					map[string]any{
						"metric": map[string]any{"job": "node"},
						"value":  []any{float64(time.Now().Unix()), "1"},
					},
				},
			},
		})
	}))
	defer mock.Close()

	bearerEnc, err := enc.EncryptString("demo-bearer")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	row := db.PrometheusSettings{
		OrgID:                 &org.ID,
		Enabled:               true,
		BaseURL:               mock.URL,
		AuthType:              "bearer",
		BearerTokenEnc:        bearerEnc,
		TLSInsecureSkipVerify: false,
		TimeoutMS:             5000,
		DefaultStepSec:        60,
	}
	if err := dbConn.Create(&row).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}

	h := &Handler{DB: dbConn, Encryptor: enc}
	body, _ := json.Marshal(prometheusQueryRequest{
		Query:   "up",
		Instant: boolPtr(true),
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/prometheus/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", org.ID.String())
	c.Request = req
	c.Set("actor", user.Username)
	h.QueryPrometheus(c)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected query status: %d body=%s", w.Code, w.Body.String())
	}

	authHeaderMu.Lock()
	gotHeader := authHeader
	authHeaderMu.Unlock()
	if gotHeader != "Bearer demo-bearer" {
		t.Fatalf("unexpected auth header: %q", gotHeader)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
