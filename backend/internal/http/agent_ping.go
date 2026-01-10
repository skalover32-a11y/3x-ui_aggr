package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AgentPing(c *gin.Context) {
	nodeID := c.Query("node_id")
	now := time.Now().UTC()
	if nodeID == "" {
		respondStatus(c, http.StatusOK, gin.H{"ok": true, "now": now})
		return
	}
	node, err := h.getNode(c.Request.Context(), nodeID)
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	online := computeAgentOnline(node.AgentLastSeenAt, 90*time.Second)
	respondStatus(c, http.StatusOK, gin.H{
		"ok":           true,
		"node_id":      node.ID.String(),
		"online":       online,
		"last_seen_at": node.AgentLastSeenAt,
		"now":          now,
	})
}
