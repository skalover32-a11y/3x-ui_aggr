package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/services/checks"
)

func TestRunServiceCheckEndpoint(t *testing.T) {
	dbConn := setupOrgTestDB(t)
	user := createUser(t, dbConn, "tester")
	org := db.Organization{ID: uuid.New(), Name: "Org One", OwnerUserID: user.ID}
	if err := dbConn.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	member := db.OrganizationMember{OrgID: org.ID, UserID: user.ID, Role: "owner"}
	if err := dbConn.Create(&member).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := db.Node{
		ID:               uuid.New(),
		OrgID:            &org.ID,
		Name:             "node-1",
		BaseURL:          "http://example.com",
		PanelUsername:    "admin",
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

	h := newTestHandler(t, dbConn)
	h.Checks = checks.New(dbConn, nil, nil, h.Encryptor, time.Second)
	r := NewRouter(h)
	jwtToken := signJWT(h.JWTSecret, user.Username, middleware.RoleAdmin)

	createBody := map[string]any{
		"node_id":         node.ID.String(),
		"name":            "svc",
		"kind":            "CUSTOM_HTTP",
		"url":             srv.URL,
		"health_path":     "/",
		"expected_status": []int{200},
	}
	bodyBytes, _ := json.Marshal(createBody)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/services", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", org.ID.String())
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var created serviceResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse service response: %v", err)
	}

	serviceID, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatalf("parse service id: %v", err)
	}

	var check db.Check
	if err := dbConn.Where("target_type = ? AND target_id = ?", "service", serviceID).First(&check).Error; err != nil {
		t.Fatalf("expected auto-created check: %v", err)
	}

	resp = httptest.NewRecorder()
	request, _ := http.NewRequest(http.MethodPost, "/api/services/"+created.ID+"/run", bytes.NewReader([]byte("{}")))
	request.Header.Set("Authorization", "Bearer "+jwtToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Org-ID", org.ID.String())
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
