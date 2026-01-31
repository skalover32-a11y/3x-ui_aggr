package httpapi

import (
	"agr_3x_ui/internal/db"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type checkRequest struct {
	Type          string           `json:"type"`
	IntervalSec   *int             `json:"interval_sec"`
	TimeoutMS     *int             `json:"timeout_ms"`
	Retries       *int             `json:"retries"`
	Enabled       *bool            `json:"enabled"`
	SeverityRules *json.RawMessage `json:"severity_rules"`
}

type checkResponse struct {
	ID            string          `json:"id"`
	TargetType    string          `json:"target_type"`
	TargetID      string          `json:"target_id"`
	Type          string          `json:"type"`
	IntervalSec   int             `json:"interval_sec"`
	TimeoutMS     int             `json:"timeout_ms"`
	Retries       int             `json:"retries"`
	Enabled       bool            `json:"enabled"`
	SeverityRules json.RawMessage `json:"severity_rules"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type checkResultResponse struct {
	ID        string          `json:"id"`
	CheckID   string          `json:"check_id"`
	TS        time.Time       `json:"ts"`
	Status    string          `json:"status"`
	Metrics   json.RawMessage `json:"metrics"`
	Error     *string         `json:"error"`
	LatencyMS *int            `json:"latency_ms"`
}

func (h *Handler) getCheck(ctx context.Context, idStr string) (*db.Check, error) {
	checkID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	var check db.Check
	if err := h.DB.WithContext(ctx).First(&check, "id = ?", checkID).Error; err != nil {
		return nil, err
	}
	return &check, nil
}

func toCheckResponse(check *db.Check) checkResponse {
	return checkResponse{
		ID:            check.ID.String(),
		TargetType:    check.TargetType,
		TargetID:      check.TargetID.String(),
		Type:          check.Type,
		IntervalSec:   check.IntervalSec,
		TimeoutMS:     check.TimeoutMS,
		Retries:       check.Retries,
		Enabled:       check.Enabled,
		SeverityRules: json.RawMessage(check.SeverityRules),
		CreatedAt:     check.CreatedAt,
		UpdatedAt:     check.UpdatedAt,
	}
}

func (h *Handler) ListNodeChecks(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var rows []db.Check
	if err := h.DB.WithContext(c.Request.Context()).Where("target_type = ? AND target_id = ?", "node", node.ID).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list checks")
		return
	}
	resp := make([]checkResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, toCheckResponse(&rows[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) ListServiceChecks(c *gin.Context) {
	service, err := h.getServiceForActor(c, c.Param("service_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	var rows []db.Check
	if err := h.DB.WithContext(c.Request.Context()).Where("target_type = ? AND target_id = ?", "service", service.ID).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list checks")
		return
	}
	resp := make([]checkResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, toCheckResponse(&rows[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateNodeCheck(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req checkRequest
	if !parseJSONBody(c, &req) {
		return
	}
	severity, err := parseSeverityRules(req.SeverityRules)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_RULES", "invalid severity rules")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	interval := 60
	if req.IntervalSec != nil {
		interval = *req.IntervalSec
	}
	timeout := 3000
	if req.TimeoutMS != nil {
		timeout = *req.TimeoutMS
	}
	retries := 1
	if req.Retries != nil {
		retries = *req.Retries
	}
	row := db.Check{
		TargetType:    "node",
		TargetID:      node.ID,
		Type:          strings.TrimSpace(req.Type),
		IntervalSec:   interval,
		TimeoutMS:     timeout,
		Retries:       retries,
		Enabled:       enabled,
		SeverityRules: severity,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create check")
		return
	}
	respondStatus(c, http.StatusCreated, toCheckResponse(&row))
}

func (h *Handler) CreateServiceCheck(c *gin.Context) {
	service, err := h.getServiceForActor(c, c.Param("service_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	var req checkRequest
	if !parseJSONBody(c, &req) {
		return
	}
	severity, err := parseSeverityRules(req.SeverityRules)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_RULES", "invalid severity rules")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	interval := 60
	if req.IntervalSec != nil {
		interval = *req.IntervalSec
	}
	timeout := 3000
	if req.TimeoutMS != nil {
		timeout = *req.TimeoutMS
	}
	retries := 1
	if req.Retries != nil {
		retries = *req.Retries
	}
	row := db.Check{
		TargetType:    "service",
		TargetID:      service.ID,
		Type:          strings.TrimSpace(req.Type),
		IntervalSec:   interval,
		TimeoutMS:     timeout,
		Retries:       retries,
		Enabled:       enabled,
		SeverityRules: severity,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create check")
		return
	}
	respondStatus(c, http.StatusCreated, toCheckResponse(&row))
}

func (h *Handler) UpdateCheck(c *gin.Context) {
	check, err := h.getCheckForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "check not found")
		return
	}
	var req checkRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.Type) != "" {
		check.Type = strings.TrimSpace(req.Type)
	}
	if req.IntervalSec != nil {
		check.IntervalSec = *req.IntervalSec
	}
	if req.TimeoutMS != nil {
		check.TimeoutMS = *req.TimeoutMS
	}
	if req.Retries != nil {
		check.Retries = *req.Retries
	}
	if req.Enabled != nil {
		check.Enabled = *req.Enabled
	}
	if req.SeverityRules != nil {
		severity, err := parseSeverityRules(req.SeverityRules)
		if err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_RULES", "invalid severity rules")
			return
		}
		check.SeverityRules = severity
	}
	check.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(check).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update check")
		return
	}
	respondStatus(c, http.StatusOK, toCheckResponse(check))
}

func (h *Handler) DeleteCheck(c *gin.Context) {
	check, err := h.getCheckForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "check not found")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Delete(&db.CheckResult{}, "check_id = ?", check.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete check results")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Delete(&db.Check{}, "id = ?", check.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete check")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) ListCheckResults(c *gin.Context) {
	check, err := h.getCheckForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "check not found")
		return
	}
	limit := 100
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
	var rows []db.CheckResult
	if err := h.DB.WithContext(c.Request.Context()).Where("check_id = ?", check.ID).Order("ts desc").Limit(limit).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list check results")
		return
	}
	resp := make([]checkResultResponse, 0, len(rows))
	for i := range rows {
		row := rows[i]
		resp = append(resp, checkResultResponse{
			ID:        row.ID.String(),
			CheckID:   row.CheckID.String(),
			TS:        row.TS,
			Status:    row.Status,
			Metrics:   json.RawMessage(row.Metrics),
			Error:     row.Error,
			LatencyMS: row.LatencyMS,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

func parseSeverityRules(raw *json.RawMessage) (datatypes.JSON, error) {
	if raw == nil || len(*raw) == 0 {
		return datatypes.JSON([]byte("{}")), nil
	}
	if !json.Valid(*raw) {
		return nil, errors.New("invalid json")
	}
	return datatypes.JSON(*raw), nil
}
