package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/db"
)

func (h *Handler) ListAuditLogs(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			limit = val
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			offset = val
		}
	}
	if offset < 0 {
		offset = 0
	}
	nodeID := strings.TrimSpace(c.Query("node_id"))
	var rows []db.AuditLog
	query := h.DB.WithContext(c.Request.Context()).Order("ts desc").Limit(limit).Offset(offset)
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	if len(nodeIDs) == 0 {
		respondStatus(c, http.StatusOK, rows)
		return
	}
	ids := make([]string, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id.String())
	}
	query = query.Where("node_id IN ?", ids)
	if nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}
	if err := query.Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load audit log")
		return
	}
	respondStatus(c, http.StatusOK, rows)
}
