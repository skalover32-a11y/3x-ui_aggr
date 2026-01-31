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
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type serviceRequest struct {
	NodeID         *string           `json:"node_id"`
	Kind           string            `json:"kind"`
	URL            *string           `json:"url"`
	Host           *string           `json:"host"`
	Port           *int              `json:"port"`
	TLSMode        *string           `json:"tls_mode"`
	HealthPath     *string           `json:"health_path"`
	ExpectedStatus []int             `json:"expected_status"`
	Headers        map[string]string `json:"headers"`
	AuthRef        *string           `json:"auth_ref"`
	IsEnabled      *bool             `json:"is_enabled"`
}

type serviceResponse struct {
	ID             string            `json:"id"`
	NodeID         string            `json:"node_id"`
	Kind           string            `json:"kind"`
	URL            *string           `json:"url"`
	Host           *string           `json:"host"`
	Port           *int              `json:"port"`
	TLSMode        *string           `json:"tls_mode"`
	HealthPath     *string           `json:"health_path"`
	ExpectedStatus []int             `json:"expected_status"`
	Headers        map[string]string `json:"headers"`
	AuthRef        *string           `json:"auth_ref"`
	IsEnabled      bool              `json:"is_enabled"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func (h *Handler) getService(ctx context.Context, idStr string) (*db.Service, error) {
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	var service db.Service
	if err := h.DB.WithContext(ctx).First(&service, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	return &service, nil
}

func toServiceResponse(service *db.Service) serviceResponse {
	return serviceResponse{
		ID:             service.ID.String(),
		NodeID:         service.NodeID.String(),
		Kind:           service.Kind,
		URL:            service.URL,
		Host:           service.Host,
		Port:           service.Port,
		TLSMode:        service.TLSMode,
		HealthPath:     service.HealthPath,
		ExpectedStatus: intArray(service.ExpectedStatus),
		Headers:        headersFromJSON(service.Headers),
		AuthRef:        service.AuthRef,
		IsEnabled:      service.IsEnabled,
		CreatedAt:      service.CreatedAt,
		UpdatedAt:      service.UpdatedAt,
	}
}

func (h *Handler) buildServiceFromRequest(nodeID uuid.UUID, req *serviceRequest) (*db.Service, error) {
	headers, err := headersToJSON(req.Headers)
	if err != nil {
		return nil, err
	}
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	service := &db.Service{
		NodeID:         nodeID,
		Kind:           strings.TrimSpace(req.Kind),
		URL:            trimPtr(req.URL),
		Host:           trimPtr(req.Host),
		Port:           req.Port,
		TLSMode:        trimPtr(req.TLSMode),
		HealthPath:     trimPtr(req.HealthPath),
		ExpectedStatus: int64Array(req.ExpectedStatus),
		Headers:        headers,
		AuthRef:        trimPtr(req.AuthRef),
		IsEnabled:      enabled,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	return service, nil
}

func (h *Handler) createDefaultCheck(ctx context.Context, tx *gorm.DB, serviceID uuid.UUID) error {
	var count int64
	if err := tx.WithContext(ctx).Model(&db.Check{}).
		Where("target_type = ? AND target_id = ?", "service", serviceID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	row := db.Check{
		TargetType:    "service",
		TargetID:      serviceID,
		Type:          "HTTP",
		IntervalSec:   60,
		TimeoutMS:     3000,
		Retries:       1,
		Enabled:       true,
		SeverityRules: datatypes.JSON([]byte("{}")),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return tx.WithContext(ctx).Create(&row).Error
}

func (h *Handler) ListAllServices(c *gin.Context) {
	query := h.DB.WithContext(c.Request.Context()).Model(&db.Service{})
	if !h.actorIsGlobalAdmin(c) {
		user, err := h.actorUser(c)
		if err != nil {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		query = query.
			Joins("JOIN nodes ON nodes.id = services.node_id").
			Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
			Where("om.user_id = ?", user.ID).
			Where("nodes.org_id IS NOT NULL")
	}
	var rows []db.Service
	if err := query.Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list services")
		return
	}
	resp := make([]serviceResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, toServiceResponse(&rows[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateServiceGlobal(c *gin.Context) {
	var req serviceRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if req.NodeID == nil || strings.TrimSpace(*req.NodeID) == "" {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", "node_id required")
		return
	}
	node, err := h.getNodeForActor(c, strings.TrimSpace(*req.NodeID))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	service, err := h.buildServiceFromRequest(node.ID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_HEADERS", "invalid headers")
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(service).Error; err != nil {
			return err
		}
		return h.createDefaultCheck(ctx, tx, service.ID)
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create service")
		return
	}
	respondStatus(c, http.StatusCreated, toServiceResponse(service))
}

func (h *Handler) ListServices(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var rows []db.Service
	if err := h.DB.WithContext(c.Request.Context()).Where("node_id = ?", node.ID).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list services")
		return
	}
	resp := make([]serviceResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, toServiceResponse(&rows[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateService(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req serviceRequest
	if !parseJSONBody(c, &req) {
		return
	}
	service, err := h.buildServiceFromRequest(node.ID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_HEADERS", "invalid headers")
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(service).Error; err != nil {
			return err
		}
		return h.createDefaultCheck(ctx, tx, service.ID)
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create service")
		return
	}
	respondStatus(c, http.StatusCreated, toServiceResponse(service))
}

func (h *Handler) UpdateService(c *gin.Context) {
	param := c.Param("service_id")
	if param == "" {
		param = c.Param("serviceId")
	}
	service, err := h.getServiceForActor(c, param)
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	var req serviceRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.Kind) != "" {
		service.Kind = strings.TrimSpace(req.Kind)
	}
	if req.URL != nil {
		service.URL = trimPtr(req.URL)
	}
	if req.Host != nil {
		service.Host = trimPtr(req.Host)
	}
	if req.Port != nil {
		service.Port = req.Port
	}
	if req.TLSMode != nil {
		service.TLSMode = trimPtr(req.TLSMode)
	}
	if req.HealthPath != nil {
		service.HealthPath = trimPtr(req.HealthPath)
	}
	if req.AuthRef != nil {
		service.AuthRef = trimPtr(req.AuthRef)
	}
	if req.Headers != nil {
		headers, err := headersToJSON(req.Headers)
		if err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_HEADERS", "invalid headers")
			return
		}
		service.Headers = headers
	}
	if req.ExpectedStatus != nil && len(req.ExpectedStatus) > 0 {
		service.ExpectedStatus = int64Array(req.ExpectedStatus)
	}
	if req.IsEnabled != nil {
		service.IsEnabled = *req.IsEnabled
	}
	service.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(service).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update service")
		return
	}
	respondStatus(c, http.StatusOK, toServiceResponse(service))
}

func (h *Handler) DeleteService(c *gin.Context) {
	param := c.Param("service_id")
	if param == "" {
		param = c.Param("serviceId")
	}
	service, err := h.getServiceForActor(c, param)
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Exec(`
			DELETE FROM check_results
			WHERE check_id IN (
				SELECT id FROM checks WHERE target_type = 'service' AND target_id = ?
			)
		`, service.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.Check{}, "target_type = ? AND target_id = ?", "service", service.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.AlertState{}, "service_id = ?", service.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.Service{}, "id = ?", service.ID).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete service")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) RunServiceCheck(c *gin.Context) {
	service, err := h.getServiceForActor(c, c.Param("service_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	if h.Checks == nil {
		respondError(c, http.StatusServiceUnavailable, "CHECKS_DISABLED", "checks worker not configured")
		return
	}
	result, err := h.Checks.RunNowService(c.Request.Context(), service.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "RUN_FAILED", "failed to run check")
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

func (h *Handler) ListServiceResults(c *gin.Context) {
	service, err := h.getServiceForActor(c, c.Param("service_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	limit := 50
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
	minutes := 0
	if raw := c.Query("minutes"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			minutes = val
		}
	}
	var since time.Time
	if minutes > 0 {
		since = time.Now().Add(-time.Duration(minutes) * time.Minute)
	}
	var rows []db.CheckResult
	query := h.DB.WithContext(c.Request.Context()).
		Table("check_results cr").
		Select("cr.id, cr.check_id, cr.ts, cr.status, cr.metrics, cr.error, cr.latency_ms").
		Joins("JOIN checks c ON c.id = cr.check_id").
		Where("c.target_type = 'service' AND c.target_id = ?", service.ID)
	if !since.IsZero() {
		query = query.Where("cr.ts >= ?", since)
	}
	if err := query.Order("cr.ts desc").Limit(limit).Scan(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list service results")
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

func headersToJSON(values map[string]string) (datatypes.JSON, error) {
	if values == nil {
		return datatypes.JSON([]byte("{}")), nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func headersFromJSON(raw datatypes.JSON) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func trimPtr(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return &v
}

func intArray(values pq.Int64Array) []int {
	out := make([]int, 0, len(values))
	for _, v := range values {
		out = append(out, int(v))
	}
	return out
}

func int64Array(values []int) pq.Int64Array {
	out := make(pq.Int64Array, 0, len(values))
	for _, v := range values {
		out = append(out, int64(v))
	}
	return out
}
