package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type incidentResponse struct {
	ID             string     `json:"id"`
	OrgID          *string    `json:"org_id,omitempty"`
	Fingerprint    string     `json:"fingerprint"`
	AlertType      string     `json:"alert_type"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	NodeID         *string    `json:"node_id,omitempty"`
	ServiceID      *string    `json:"service_id,omitempty"`
	BotID          *string    `json:"bot_id,omitempty"`
	CheckID        *string    `json:"check_id,omitempty"`
	Title          string     `json:"title"`
	Description    *string    `json:"description,omitempty"`
	FirstSeen      time.Time  `json:"first_seen"`
	LastSeen       time.Time  `json:"last_seen"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	RecoveredAt    *time.Time `json:"recovered_at,omitempty"`
	Occurrences    int        `json:"occurrences"`
	LastError      *string    `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (h *Handler) ListIncidents(c *gin.Context) {
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}

	query := h.DB.WithContext(c.Request.Context()).Model(&db.Incident{})
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		query = query.Where("status = ?", status)
	}
	if active := strings.TrimSpace(c.Query("active")); active == "1" || strings.EqualFold(active, "true") {
		query = query.Where("status IN ('open','acked')")
	}

	var rows []db.Incident
	if err := query.Order("last_seen desc").Limit(limit).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list incidents")
		return
	}

	resp := make([]incidentResponse, 0, len(rows))
	for i := range rows {
		row := rows[i]
		if !incidentAllowed(c.Request.Context(), h.DB, nodeIDs, &row) {
			continue
		}
		resp = append(resp, incidentResponse{
			ID:             row.ID.String(),
			OrgID:          uuidToStringPtr(row.OrgID),
			Fingerprint:    row.Fingerprint,
			AlertType:      row.AlertType,
			Severity:       row.Severity,
			Status:         row.Status,
			NodeID:         uuidToStringPtr(row.NodeID),
			ServiceID:      uuidToStringPtr(row.ServiceID),
			BotID:          uuidToStringPtr(row.BotID),
			CheckID:        uuidToStringPtr(row.CheckID),
			Title:          row.Title,
			Description:    row.Description,
			FirstSeen:      row.FirstSeen,
			LastSeen:       row.LastSeen,
			AcknowledgedAt: row.AcknowledgedAt,
			AcknowledgedBy: row.AcknowledgedBy,
			RecoveredAt:    row.RecoveredAt,
			Occurrences:    row.Occurrences,
			LastError:      row.LastError,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		})
	}

	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) AckIncident(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ID", "invalid incident id")
		return
	}
	var row db.Incident
	if err := h.DB.WithContext(c.Request.Context()).First(&row, "id = ?", id).Error; err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "incident not found")
		return
	}
	nodeIDs, err := h.accessibleNodeIDs(c)
	if err != nil || !incidentAllowed(c.Request.Context(), h.DB, nodeIDs, &row) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	who := "ui"
	if user, err := h.actorUser(c); err == nil {
		who = strings.TrimSpace(user.Username)
		if who == "" {
			who = "ui"
		}
	}
	now := time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Model(&db.Incident{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status":          "acked",
		"acknowledged_at": now,
		"acknowledged_by": who,
		"updated_at":      now,
	}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to acknowledge incident")
		return
	}
	_ = h.DB.WithContext(c.Request.Context()).Model(&db.AlertState{}).Where("incident_id = ?", row.ID).Updates(map[string]any{
		"muted_until": now.Add(24 * time.Hour),
		"updated_at":  now,
	}).Error
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func incidentAllowed(ctx context.Context, dbConn *gorm.DB, nodeIDs map[uuid.UUID]struct{}, row *db.Incident) bool {
	if row == nil || dbConn == nil {
		return false
	}
	if row.NodeID != nil {
		_, ok := nodeIDs[*row.NodeID]
		return ok
	}
	if row.ServiceID != nil {
		var svc db.Service
		if err := dbConn.WithContext(ctx).Select("node_id").First(&svc, "id = ?", *row.ServiceID).Error; err == nil {
			_, ok := nodeIDs[svc.NodeID]
			return ok
		}
		return false
	}
	if row.BotID != nil {
		var bot db.Bot
		if err := dbConn.WithContext(ctx).Select("node_id").First(&bot, "id = ?", *row.BotID).Error; err == nil {
			_, ok := nodeIDs[bot.NodeID]
			return ok
		}
		return false
	}
	return false
}
