package ops

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

type fakeAgentExecutor struct {
	version string
}

func (f *fakeAgentExecutor) Reboot(ctx context.Context, node *db.Node) (string, int, error) {
	return "", 0, nil
}

func (f *fakeAgentExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	if f.version == "" {
		return "update ok", 0, nil
	}
	return "panel_version: " + f.version, 0, nil
}

func (f *fakeAgentExecutor) DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error) {
	return "", 0, nil
}

func (f *fakeAgentExecutor) RestartService(ctx context.Context, node *db.Node, service string) (string, int, error) {
	return "", 0, nil
}

func TestParsePanelVersionFromLog(t *testing.T) {
	version := parsePanelVersionFromLog("foo\npanel_version: 2.8.7\nbar")
	if version != "2.8.7" {
		t.Fatalf("expected version 2.8.7, got %q", version)
	}
	version = parsePanelVersionFromLog("panel_version=3.0.1")
	if version != "3.0.1" {
		t.Fatalf("expected version 3.0.1, got %q", version)
	}
}

func TestUpdatePanelStoresVersion(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.Node{}, &db.OpsJob{}, &db.OpsJobItem{}, &db.NodeMetricsLatest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE nodes, ops_jobs, ops_job_items, node_metrics_latest RESTART IDENTITY").Error

	url := "http://127.0.0.1:9191"
	node := db.Node{
		ID:               uuid.New(),
		Name:             "node-a",
		Kind:             "PANEL",
		BaseURL:          "http://example",
		PanelUsername:    "user",
		PanelPasswordEnc: "x",
		SSHHost:          "127.0.0.1",
		SSHPort:          22,
		SSHUser:          "root",
		SSHKeyEnc:        "x",
		AgentEnabled:     true,
		AgentInstalled:   true,
		AgentURL:         &url,
		IsEnabled:        true,
		SSHEnabled:       true,
	}
	if err := dbConn.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	exec := &fakeAgentExecutor{version: "2.8.7"}
	svc := &Service{DB: dbConn, Executor: exec, AgentExecutor: exec, Hub: NewHub()}

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		Type:     JobTypeUpdatePanel,
		NodeIDs:  []string{node.ID.String()},
		AllNodes: false,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	svc.pickAndRun(context.Background())

	var updated db.Node
	if err := dbConn.First(&updated, "id = ?", node.ID).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if updated.PanelVersion == nil || *updated.PanelVersion != "2.8.7" {
		t.Fatalf("expected panel_version 2.8.7, got %v", updated.PanelVersion)
	}
	if updated.VersionsCheckedAt == nil || time.Since(*updated.VersionsCheckedAt) > time.Minute {
		t.Fatalf("expected versions_checked_at to be recent")
	}

	var metric db.NodeMetricsLatest
	if err := dbConn.First(&metric, "node_id = ?", node.ID).Error; err != nil {
		t.Fatalf("load node_metrics_latest: %v", err)
	}
	if metric.PanelVersion == nil || *metric.PanelVersion != "2.8.7" {
		t.Fatalf("expected metrics panel_version 2.8.7, got %v", metric.PanelVersion)
	}

	var item db.OpsJobItem
	if err := dbConn.First(&item, "job_id = ?", job.ID).Error; err != nil {
		t.Fatalf("load job item: %v", err)
	}
	if item.Status != JobSuccess {
		t.Fatalf("expected job item success, got %s", item.Status)
	}
}
