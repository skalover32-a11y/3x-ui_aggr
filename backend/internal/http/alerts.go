package httpapi

import (
	"context"
	"encoding/json"
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
	BotID       *string    `json:"bot_id"`
	CheckType   *string    `json:"check_type"`
	LastStatus  *string    `json:"last_status"`
	FirstSeen   time.Time  `json:"first_seen"`
	LastSeen    time.Time  `json:"last_seen"`
	Occurrences int        `json:"occurrences"`
	MutedUntil  *time.Time `json:"muted_until"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type alertMuteRequest struct {
	DurationSec int `json:"duration"`
}

type retryError struct {
	status int
	msg    string
}

func (e retryError) Error() string {
	return e.msg
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
		query = query.Where("last_status <> ?", "ok")
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
	if raw := strings.TrimSpace(c.Query("bot_id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			query = query.Where("bot_id = ?", id)
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
			BotID:       uuidToStringPtr(row.BotID),
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

func (h *Handler) MuteAlert(c *gin.Context) {
	fingerprint := strings.TrimSpace(c.Param("fingerprint"))
	if fingerprint == "" {
		respondError(c, http.StatusBadRequest, "INVALID_FINGERPRINT", "fingerprint required")
		return
	}
	durationSec := 3600
	if raw := c.Query("duration"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			durationSec = val
		}
	}
	if c.Request.ContentLength > 0 {
		var req alertMuteRequest
		if !parseJSONBody(c, &req) {
			return
		}
		if req.DurationSec > 0 {
			durationSec = req.DurationSec
		}
	}
	if durationSec <= 0 {
		durationSec = 3600
	}
	if h.Alerts == nil {
		respondError(c, http.StatusServiceUnavailable, "ALERTS_DISABLED", "alerts service not configured")
		return
	}
	if err := h.Alerts.MuteFingerprint(c.Request.Context(), fingerprint, time.Duration(durationSec)*time.Second); err != nil {
		respondError(c, http.StatusInternalServerError, "MUTE_FAILED", "failed to mute alert")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) RetryAlert(c *gin.Context) {
	fingerprint := strings.TrimSpace(c.Param("fingerprint"))
	if fingerprint == "" {
		respondError(c, http.StatusBadRequest, "INVALID_FINGERPRINT", "fingerprint required")
		return
	}
	result, err := h.runRetry(c.Request.Context(), fingerprint)
	if err != nil {
		status := http.StatusBadRequest
		if typed, ok := err.(retryError); ok {
			status = typed.status
		}
		respondError(c, status, "RETRY_FAILED", err.Error())
		return
	}
	resp := checkResultResponse{
		ID:        result.ID.String(),
		CheckID:   result.CheckID.String(),
		TS:        result.TS,
		Status:    result.Status,
		Metrics:   json.RawMessage(result.Metrics),
		Error:     result.Error,
		LatencyMS: result.LatencyMS,
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) runRetry(ctx context.Context, fingerprint string) (*db.CheckResult, error) {
	if h.Checks == nil {
		return nil, retryError{status: http.StatusServiceUnavailable, msg: "checks worker not configured"}
	}
	var state db.AlertState
	if err := h.DB.WithContext(ctx).First(&state, "fingerprint = ?", fingerprint).Error; err != nil {
		return nil, retryError{status: http.StatusNotFound, msg: "alert not found"}
	}
	if state.ServiceID == nil {
		if state.BotID == nil {
			return nil, retryError{status: http.StatusBadRequest, msg: "alert has no target"}
		}
		return h.Checks.RunNowBot(ctx, *state.BotID)
	}
	result, err := h.Checks.RunNowService(ctx, *state.ServiceID)
	if err != nil {
		return nil, retryError{status: http.StatusInternalServerError, msg: "failed to run check"}
	}
	return result, nil
}

func uuidToStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	v := id.String()
	return &v
}
