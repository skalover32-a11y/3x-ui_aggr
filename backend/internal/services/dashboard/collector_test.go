package dashboard

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

func TestComputeAggregateAvgPingOnlineOnly(t *testing.T) {
	pingFast := int64(10)
	pingSlow := int64(100)
	nodes := []DashboardNode{
		{AgentOnline: true, AgentInstalled: true, PingMs: &pingFast},
		{AgentOnline: false, AgentInstalled: true, PingMs: &pingSlow},
		{AgentOnline: true, AgentInstalled: true, PingMs: nil},
	}
	agg := computeAggregate(nodes)
	if agg.AvgPingMs == nil {
		t.Fatalf("expected avg ping, got nil")
	}
	if math.Abs(*agg.AvgPingMs-10) > 0.01 {
		t.Fatalf("expected avg ping 10, got %.2f", *agg.AvgPingMs)
	}
}

func TestComputeTrafficTotalForRange(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	dbConn, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbConn.AutoMigrate(&db.NodeMetric{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = dbConn.Exec("TRUNCATE node_metrics").Error

	now := time.Now().UTC()
	nodeA := uuid.New()
	nodeB := uuid.New()
	rows := []db.NodeMetric{
		{NodeID: nodeA, TS: now.Add(-50 * time.Minute), NetRxBytes: int64Ptr(100), NetTxBytes: int64Ptr(50)},
		{NodeID: nodeA, TS: now.Add(-40 * time.Minute), NetRxBytes: int64Ptr(200), NetTxBytes: int64Ptr(70)},
		{NodeID: nodeA, TS: now.Add(-30 * time.Minute), NetRxBytes: int64Ptr(150), NetTxBytes: int64Ptr(10)},
		{NodeID: nodeA, TS: now.Add(-20 * time.Minute), NetRxBytes: int64Ptr(170), NetTxBytes: int64Ptr(20)},
		{NodeID: nodeB, TS: now.Add(-40 * time.Minute), NetRxBytes: int64Ptr(10), NetTxBytes: int64Ptr(5)},
		{NodeID: nodeB, TS: now.Add(-10 * time.Minute), NetRxBytes: int64Ptr(30), NetTxBytes: int64Ptr(15)},
	}
	if err := dbConn.Create(&rows).Error; err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	svc := &Service{DB: dbConn}
	total, err := svc.computeTrafficTotalForRange(context.Background(), nil, 24*time.Hour)
	if err != nil {
		t.Fatalf("compute traffic: %v", err)
	}
	if total == nil {
		t.Fatalf("expected total, got nil")
	}
	if *total != 180 {
		t.Fatalf("expected total 180, got %d", *total)
	}
}

func int64Ptr(val int64) *int64 {
	return &val
}
