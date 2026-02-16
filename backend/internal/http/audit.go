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
	user, err := h.actorUser(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	activeOrgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	ids := make([]string, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id.String())
	}
	if nodeID != "" {
		if len(ids) == 0 {
			respondStatus(c, http.StatusOK, rows)
			return
		}
		query = query.Where("node_id IN ?", ids)
		query = query.Where("node_id = ?", nodeID)
	} else if len(ids) > 0 {
		if activeOrgID != nil {
			query = query.Where("(node_id IN ? OR (node_id IS NULL AND payload_json ->> 'org_id' = ?))", ids, activeOrgID.String())
		} else {
			query = query.Where("node_id IN ?", ids)
		}
	} else if activeOrgID != nil {
		query = query.Where("node_id IS NULL AND payload_json ->> 'org_id' = ?", activeOrgID.String())
	} else {
		respondStatus(c, http.StatusOK, rows)
		return
	}
	if err := query.Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load audit log")
		return
	}
	respondStatus(c, http.StatusOK, rows)
}
