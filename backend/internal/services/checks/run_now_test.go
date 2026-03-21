package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
)

func TestRunNowService(t *testing.T) {
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
	t.Cleanup(srv.Close)
	orgID := uuid.New()

	node := db.Node{
		ID:               uuid.New(),
		OrgID:            &orgID,
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
	service := db.Service{
		ID:             uuid.New(),
		OrgID:          orgID,
		NodeID:         &node.ID,
		Name:           "svc-1",
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

	worker := &Worker{DB: dbConn}
	result, err := worker.RunNowService(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("run now: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("expected ok result, got %s", result.Status)
	}
	if result.CheckID != check.ID {
		t.Fatalf("expected check_id %s, got %s", check.ID, result.CheckID)
	}
}
