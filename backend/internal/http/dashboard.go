package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"agr_3x_ui/internal/services/dashboard"
)

func (h *Handler) GetDashboardSummary(c *gin.Context) {
	if h.Dashboard == nil {
		respondError(c, http.StatusServiceUnavailable, "DASHBOARD_DISABLED", "dashboard service not configured")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	scopedIDs := make([]uuid.UUID, 0, len(nodeIDs))
	for id := range nodeIDs {
		scopedIDs = append(scopedIDs, id)
	}
	data, err := h.Dashboard.SummaryForNodeIDs(c.Request.Context(), scopedIDs)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DASHBOARD_SUMMARY", "failed to load summary")
		return
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
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	scopedIDs := make([]uuid.UUID, 0, len(nodeIDs))
	for id := range nodeIDs {
		scopedIDs = append(scopedIDs, id)
	}
	users, err := h.Dashboard.ActiveUsersForNodeIDs(c.Request.Context(), scopedIDs, limit, search)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DASHBOARD_USERS", "failed to load active users")
		return
	}
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
