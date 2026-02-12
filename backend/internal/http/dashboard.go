package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/dashboard"
)

func (h *Handler) GetDashboardSummary(c *gin.Context) {
	if h.Dashboard == nil {
		respondError(c, http.StatusServiceUnavailable, "DASHBOARD_DISABLED", "dashboard service not configured")
		return
	}
	data, err := h.Dashboard.Summary(c.Request.Context())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DASHBOARD_SUMMARY", "failed to load summary")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	if nodesRaw, ok := data["nodes"]; ok {
		if nodes, ok := nodesRaw.([]dashboard.DashboardNode); ok {
			filtered := make([]dashboard.DashboardNode, 0, len(nodes))
			for _, node := range nodes {
				if _, ok := nodeIDs[node.NodeID]; ok {
					filtered = append(filtered, node)
				}
			}
			data["nodes"] = filtered
			agg := computeAggregateScoped(filtered)
			traffic24h, traffic7d := computeTrafficTotalsScoped(c.Request.Context(), h.DB, nodeIDs)
			agg.TotalTraffic24h = traffic24h
			agg.TotalTraffic7d = traffic7d
			agg.ActiveAlertsCount = countActiveAlertsScoped(c.Request.Context(), h.DB, nodeIDs)
			agg.ActiveUsers = countActiveUsersScoped(c.Request.Context(), h.DB, nodeIDs)
			data["aggregate"] = agg
		}
	}
	respondStatus(c, http.StatusOK, data)
}

func (h *Handler) GetDashboardActiveUsers(c *gin.Context) {
	if h.Dashboard == nil {
		respondError(c, http.StatusServiceUnavailable, "DASHBOARD_DISABLED", "dashboard service not configured")
		return
	}
	limit := parseIntQuery(c, "limit", 200)
	search := strings.TrimSpace(c.Query("search"))
	users, err := h.Dashboard.ActiveUsers(c.Request.Context(), limit, search)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DASHBOARD_USERS", "failed to load active users")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	filtered := make([]dashboard.DashboardActiveUser, 0, len(users))
	for _, row := range users {
		if _, ok := nodeIDs[row.NodeID]; ok {
			filtered = append(filtered, row)
		}
	}
	users = filtered
	respondStatus(c, http.StatusOK, users)
}

func (h *Handler) DashboardStream(c *gin.Context) {
	if h.Dashboard == nil {
		respondError(c, http.StatusServiceUnavailable, "DASHBOARD_DISABLED", "dashboard service not configured")
		return
	}
	_, role, err := h.authenticateWS(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}
	if !canDashboardRole(role) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(wsReadLimit)

	events, unsubscribe := h.Dashboard.Subscribe()
	defer unsubscribe()

	writeMu := sync.Mutex{}
	writeEvent := func(payload any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return ws.WriteMessage(websocket.TextMessage, data)
	}

	if snapshot, err := h.Dashboard.LoadSnapshot(c.Request.Context()); err == nil {
		snapshot = filterDashboardSnapshot(snapshot, nodeIDs)
		_ = writeEvent(map[string]any{
			"type": "snapshot",
			"ts":   time.Now().UTC().Format(time.RFC3339),
			"data": snapshot,
		})
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-done:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if !dashboardEventAllowed(event, nodeIDs) {
				continue
			}
			if err := writeEvent(event); err != nil {
				return
			}
		case <-heartbeat.C:
			hb := map[string]any{
				"type": "heartbeat",
				"ts":   time.Now().UTC().Format(time.RFC3339),
				"data": map[string]any{},
			}
			if err := writeEvent(hb); err != nil {
				return
			}
		}
	}
}

func canDashboardRole(role string) bool {
	return role == "admin" || role == "operator" || role == "viewer"
}

func computeAggregateScoped(nodes []dashboard.DashboardNode) dashboard.AggregateSummary {
	var agg dashboard.AggregateSummary
	var cpuCount int
	var cpuSum float64
	var pingCount int
	var pingSum float64
	var totalConnections int64
	for _, node := range nodes {
		agg.NodesTotal++
		if node.AgentInstalled {
			agg.AgentsTotal++
		}
		if node.AgentOnline {
			agg.NodesOnline++
			agg.AgentsActive++
		}
		if node.ServiceRunning != nil {
			if *node.ServiceRunning {
				agg.ServicesOnline++
			}
		} else if node.AgentOnline {
			agg.ServicesOnline++
		}
		if node.CPUPct != nil {
			cpuSum += *node.CPUPct
			cpuCount++
		}
		if node.PingMs != nil {
			pingSum += float64(*node.PingMs)
			pingCount++
		}
		if node.NetRxBps != nil {
			agg.TotalRxBps += *node.NetRxBps
		}
		if node.NetTxBps != nil {
			agg.TotalTxBps += *node.NetTxBps
		}
		if node.TCPConnections != nil {
			totalConnections += *node.TCPConnections
		}
		if node.UDPConnections != nil {
			totalConnections += *node.UDPConnections
		}
	}
	if cpuCount > 0 {
		agg.AvgCPU = cpuSum / float64(cpuCount)
	}
	if pingCount > 0 {
		val := pingSum / float64(pingCount)
		agg.AvgPingMs = &val
	}
	agg.TotalConnections = &totalConnections
	agg.NodesOffline = agg.NodesTotal - agg.NodesOnline
	return agg
}

func filterDashboardSnapshot(snapshot map[string]any, nodeIDs map[uuid.UUID]struct{}) map[string]any {
	if len(nodeIDs) == 0 {
		snapshot["nodes"] = []dashboard.DashboardNode{}
		snapshot["active_users"] = []dashboard.DashboardActiveUser{}
		return snapshot
	}
	if rawNodes, ok := snapshot["nodes"]; ok {
		if nodes, ok := rawNodes.([]dashboard.DashboardNode); ok {
			filtered := make([]dashboard.DashboardNode, 0, len(nodes))
			for _, node := range nodes {
				if _, allowed := nodeIDs[node.NodeID]; allowed {
					filtered = append(filtered, node)
				}
			}
			snapshot["nodes"] = filtered
		}
	}
	if rawUsers, ok := snapshot["active_users"]; ok {
		if users, ok := rawUsers.([]dashboard.DashboardActiveUser); ok {
			filtered := make([]dashboard.DashboardActiveUser, 0, len(users))
			for _, user := range users {
				if _, allowed := nodeIDs[user.NodeID]; allowed {
					filtered = append(filtered, user)
				}
			}
			snapshot["active_users"] = filtered
		}
	}
	return snapshot
}

func dashboardEventAllowed(event dashboard.Event, nodeIDs map[uuid.UUID]struct{}) bool {
	switch event.Type {
	case dashboard.EventNodeMetricsUpdate, dashboard.EventActiveUsersUpdate:
		data, ok := event.Data.(map[string]any)
		if !ok {
			return false
		}
		idRaw, ok := data["node_id"]
		if !ok {
			return false
		}
		nodeID, ok := parseUUIDAny(idRaw)
		if !ok {
			return false
		}
		_, allowed := nodeIDs[nodeID]
		return allowed
	default:
		return true
	}
}

func parseUUIDAny(value any) (uuid.UUID, bool) {
	switch v := value.(type) {
	case string:
		id, err := uuid.Parse(strings.TrimSpace(v))
		if err != nil {
			return uuid.Nil, false
		}
		return id, true
	case uuid.UUID:
		return v, true
	default:
		return uuid.Nil, false
	}
}

func countActiveAlertsScoped(ctx context.Context, dbConn *gorm.DB, nodeIDs map[uuid.UUID]struct{}) int {
	if dbConn == nil || len(nodeIDs) == 0 {
		return 0
	}
	ids := make([]uuid.UUID, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	var count int64
	if err := dbConn.WithContext(ctx).
		Model(&db.AlertState{}).
		Where("node_id IN ?", ids).
		Where("last_status <> ?", "ok").
		Count(&count).Error; err != nil {
		return 0
	}
	return int(count)
}

func countActiveUsersScoped(ctx context.Context, dbConn *gorm.DB, nodeIDs map[uuid.UUID]struct{}) int {
	if dbConn == nil || len(nodeIDs) == 0 {
		return 0
	}
	ids := make([]uuid.UUID, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	var count int64
	if err := dbConn.WithContext(ctx).
		Table("active_users_latest").
		Where("node_id IN ?", ids).
		Count(&count).Error; err != nil {
		return 0
	}
	return int(count)
}

type scopedTrafficPoint struct {
	NodeID uuid.UUID
	TS     time.Time
	NetRx  *int64
	NetTx  *int64
}

func computeTrafficTotalsScoped(ctx context.Context, dbConn *gorm.DB, nodeIDs map[uuid.UUID]struct{}) (*int64, *int64) {
	if dbConn == nil || len(nodeIDs) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	traffic24h := computeTrafficTotalForRangeScoped(ctx, dbConn, ids, 24*time.Hour)
	traffic7d := computeTrafficTotalForRangeScoped(ctx, dbConn, ids, 7*24*time.Hour)
	return traffic24h, traffic7d
}

func computeTrafficTotalForRangeScoped(ctx context.Context, dbConn *gorm.DB, nodeIDs []uuid.UUID, window time.Duration) *int64 {
	if dbConn == nil || len(nodeIDs) == 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-window)
	var rows []scopedTrafficPoint
	if err := dbConn.WithContext(ctx).
		Table("node_metrics").
		Select("node_id, ts, net_rx_bytes as net_rx, net_tx_bytes as net_tx").
		Where("node_id IN ?", nodeIDs).
		Where("ts >= ?", cutoff).
		Where("net_rx_bytes IS NOT NULL AND net_tx_bytes IS NOT NULL").
		Order("node_id, ts").
		Scan(&rows).Error; err != nil {
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	var total int64
	var current uuid.UUID
	var prevRx, prevTx *int64
	first := true
	for _, row := range rows {
		if first || row.NodeID != current {
			current = row.NodeID
			prevRx, prevTx = row.NetRx, row.NetTx
			first = false
			continue
		}
		if prevRx != nil && row.NetRx != nil {
			if delta := *row.NetRx - *prevRx; delta > 0 {
				total += delta
			}
		}
		if prevTx != nil && row.NetTx != nil {
			if delta := *row.NetTx - *prevTx; delta > 0 {
				total += delta
			}
		}
		prevRx, prevTx = row.NetRx, row.NetTx
	}
	return &total
}

func parseIntQuery(c *gin.Context, key string, fallback int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return val
}
