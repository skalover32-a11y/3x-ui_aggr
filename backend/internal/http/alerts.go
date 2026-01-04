package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

type alertStateResponse struct {
	Fingerprint string     `json:"fingerprint"`
	AlertType   string     `json:"alert_type"`
	NodeID      *string    `json:"node_id"`
	ServiceID   *string    `json:"service_id"`
	CheckType   *string    `json:"check_type"`
	LastStatus  *string    `json:"last_status"`
	FirstSeen   time.Time  `json:"first_seen"`
	LastSeen    time.Time  `json:"last_seen"`
	Occurrences int        `json:"occurrences"`
	MutedUntil  *time.Time `json:"muted_until"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (h *Handler) ListAlerts(c *gin.Context) {
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			limit = val
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	query := h.DB.WithContext(c.Request.Context()).Model(&db.AlertState{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("last_status = ?", status)
	}
	if active := strings.TrimSpace(c.Query("active")); active == "true" || active == "1" {
		query = query.Where("last_status = ?", "active")
	}
	if raw := strings.TrimSpace(c.Query("node_id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			query = query.Where("node_id = ?", id)
		}
	}
	if raw := strings.TrimSpace(c.Query("service_id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			query = query.Where("service_id = ?", id)
		}
	}

	var rows []db.AlertState
	if err := query.Order("last_seen desc").Limit(limit).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list alerts")
		return
	}
	resp := make([]alertStateResponse, 0, len(rows))
	for i := range rows {
		row := rows[i]
		resp = append(resp, alertStateResponse{
			Fingerprint: row.Fingerprint,
			AlertType:   row.AlertType,
			NodeID:      uuidToStringPtr(row.NodeID),
			ServiceID:   uuidToStringPtr(row.ServiceID),
			CheckType:   row.CheckType,
			LastStatus:  row.LastStatus,
			FirstSeen:   row.FirstSeen,
			LastSeen:    row.LastSeen,
			Occurrences: row.Occurrences,
			MutedUntil:  row.MutedUntil,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

func uuidToStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	v := id.String()
	return &v
}
