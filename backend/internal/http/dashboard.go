package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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
