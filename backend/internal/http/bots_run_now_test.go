package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/services/checks"
)

func TestRunBotCheckEndpoint(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.Node{}, &db.Bot{}, &db.Check{}, &db.CheckResult{}, &db.AlertState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE check_results, checks, bots, nodes RESTART IDENTITY CASCADE").Error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := db.Node{
		ID:               uuid.New(),
		Name:             "node-1",
		Kind:             "HOST",
		BaseURL:          "",
		PanelUsername:    "",
		PanelPasswordEnc: "enc",
		SSHHost:          "127.0.0.1",
		SSHPort:          22,
		SSHUser:          "root",
		SSHKeyEnc:        "key",
		VerifyTLS:        true,
		IsEnabled:        true,
		SSHEnabled:       true,
		SSHAuthMethod:    "key",
	}
	if err := dbConn.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
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
		DB:        dbConn,
		Checks:    checks.New(dbConn, nil, nil, nil, time.Second),
		JWTSecret: secret,
	}
	r := NewRouter(h)

	createBody := map[string]any{
		"name":            "bot-1",
		"kind":            "HTTP",
		"health_url":      srv.URL,
		"health_path":     "/",
		"expected_status": []int{200},
	}
	bodyBytes, _ := json.Marshal(createBody)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/nodes/"+node.ID.String()+"/bots", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	var created botResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse bot response: %v", err)
	}

	botID, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatalf("parse bot id: %v", err)
	}

	var check db.Check
	if err := dbConn.Where("target_type = ? AND target_id = ?", "bot", botID).First(&check).Error; err != nil {
		t.Fatalf("expected auto-created check: %v", err)
	}

	resp = httptest.NewRecorder()
	request, _ := http.NewRequest(http.MethodPost, "/api/bots/"+created.ID+"/run-now", bytes.NewReader([]byte("{}")))
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
