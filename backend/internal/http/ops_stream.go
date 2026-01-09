package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (h *Handler) OpsJobStream(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	jobID := c.Param("id")
	if _, err := h.Ops.GetJob(c.Request.Context(), jobID); err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(wsReadLimit)

	events, unsubscribe := h.Ops.Subscribe(jobID)
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
				"type":   "heartbeat",
				"job_id": jobID,
				"ts":     time.Now().UTC().Format(time.RFC3339),
				"data":   map[string]any{},
			}
			if err := writeEvent(hb); err != nil {
				return
			}
		}
	}
}
