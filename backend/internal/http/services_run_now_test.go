package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/services/checks"
)

func TestRunServiceCheckEndpoint(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.Node{}, &db.Service{}, &db.Check{}, &db.CheckResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE check_results, checks, services, nodes RESTART IDENTITY CASCADE").Error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := db.Node{
		ID:              uuid.New(),
		Name:            "node-1",
		BaseURL:         "http://example.com",
		PanelUsername:   "admin",
		PanelPasswordEnc: "enc",
		SSHHost:         "127.0.0.1",
		SSHPort:         22,
		SSHUser:         "root",
		SSHKeyEnc:       "key",
		VerifyTLS:       true,
		IsEnabled:       true,
		SSHEnabled:      true,
		SSHAuthMethod:   "key",
	}
	if err := dbConn.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	service := db.Service{
		ID:             uuid.New(),
		NodeID:         node.ID,
		Kind:           "CUSTOM_HTTP",
		URL:            stringPtr(srv.URL),
		HealthPath:     stringPtr("/"),
		ExpectedStatus: pq.Int64Array{200},
		Headers:        datatypes.JSON([]byte("{}")),
		IsEnabled:      true,
	}
	if err := dbConn.Create(&service).Error; err != nil {
		t.Fatalf("create service: %v", err)
	}
	check := db.Check{
		ID:            uuid.New(),
		TargetType:    "service",
		TargetID:      service.ID,
		Type:          "HTTP",
		IntervalSec:   60,
		TimeoutMS:     1000,
		Retries:       0,
		Enabled:       true,
		SeverityRules: datatypes.JSON([]byte("{}")),
	}
	if err := dbConn.Create(&check).Error; err != nil {
		t.Fatalf("create check: %v", err)
	}

	secret := []byte("test")
	claims := middleware.Claims{
		Role: "admin",
		User: "tester",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	h := &Handler{
		DB:     dbConn,
		Checks: checks.New(dbConn, nil, nil, nil, time.Second),
		JWTSecret: secret,
	}
	r := NewRouter(h)

	resp := httptest.NewRecorder()
	request, _ := http.NewRequest(http.MethodPost, "/api/services/"+service.ID.String()+"/run", bytes.NewReader([]byte("{}")))
	request.Header.Set("Authorization", "Bearer "+jwtToken)
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, request)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var results []db.CheckResult
	if err := dbConn.Where("check_id = ?", check.ID).Find(&results).Error; err != nil {
		t.Fatalf("query results: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected check_results row")
	}
}

func stringPtr(value string) *string {
	v := value
	return &v
}
