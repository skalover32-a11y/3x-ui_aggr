package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	"agr_3x_ui/internal/services/checks"
)

type serviceRequest struct {
	Name           *string           `json:"name"`
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
	AuthUsername   *string           `json:"auth_username"`
	AuthPassword   *string           `json:"auth_password"`
	IsEnabled      *bool             `json:"is_enabled"`
}

type serviceResponse struct {
	ID              string            `json:"id"`
	OrgID           string            `json:"org_id"`
	NodeID          *string           `json:"node_id,omitempty"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	URL             *string           `json:"url"`
	Host            *string           `json:"host"`
	Port            *int              `json:"port"`
	TLSMode         *string           `json:"tls_mode"`
	HealthPath      *string           `json:"health_path"`
	ExpectedStatus  []int             `json:"expected_status"`
	Headers         map[string]string `json:"headers"`
	AuthRef         *string           `json:"auth_ref"`
	AuthUsername    *string           `json:"auth_username,omitempty"`
	AuthPasswordSet bool              `json:"auth_password_set"`
	IsEnabled       bool              `json:"is_enabled"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
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
	var nodeID *string
	if service.NodeID != nil {
		val := service.NodeID.String()
		nodeID = &val
	}
	return serviceResponse{
		ID:              service.ID.String(),
		OrgID:           service.OrgID.String(),
		NodeID:          nodeID,
		Name:            strings.TrimSpace(service.Name),
		Kind:            service.Kind,
		URL:             service.URL,
		Host:            service.Host,
		Port:            service.Port,
		TLSMode:         service.TLSMode,
		HealthPath:      service.HealthPath,
		ExpectedStatus:  intArray(service.ExpectedStatus),
		Headers:         headersFromJSON(service.Headers),
		AuthRef:         service.AuthRef,
		AuthUsername:    service.AuthUsername,
		AuthPasswordSet: service.AuthPasswordEnc != nil && strings.TrimSpace(*service.AuthPasswordEnc) != "",
		IsEnabled:       service.IsEnabled,
		CreatedAt:       service.CreatedAt,
		UpdatedAt:       service.UpdatedAt,
	}
}

func (h *Handler) buildServiceFromRequest(orgID uuid.UUID, nodeID *uuid.UUID, req *serviceRequest) (*db.Service, error) {
	headers, err := headersToJSON(req.Headers)
	if err != nil {
		return nil, err
	}
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	authUsername := trimPtr(req.AuthUsername)
	var authPasswordEnc *string
	if req.AuthPassword != nil {
		rawPassword := strings.TrimSpace(*req.AuthPassword)
		if rawPassword != "" {
			if authUsername == nil {
				return nil, fmt.Errorf("auth username required")
			}
			enc, err := h.encryptString(rawPassword)
			if err != nil {
				return nil, err
			}
			authPasswordEnc = &enc
		}
	}
	service := &db.Service{
		OrgID:           orgID,
		NodeID:          nodeID,
		Name:            serviceNameOrDefault(trimPtr(req.Name), strings.TrimSpace(req.Kind), trimPtr(req.URL), trimPtr(req.Host), req.Port),
		Kind:            strings.TrimSpace(req.Kind),
		URL:             trimPtr(req.URL),
		Host:            trimPtr(req.Host),
		Port:            req.Port,
		TLSMode:         trimPtr(req.TLSMode),
		HealthPath:      trimPtr(req.HealthPath),
		ExpectedStatus:  int64Array(req.ExpectedStatus),
		Headers:         headers,
		AuthRef:         trimPtr(req.AuthRef),
		AuthUsername:    authUsername,
		AuthPasswordEnc: authPasswordEnc,
		IsEnabled:       enabled,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	return service, nil
}

func serviceNameOrDefault(name *string, kind string, rawURL *string, host *string, port *int) string {
	if name != nil && strings.TrimSpace(*name) != "" {
		return strings.TrimSpace(*name)
	}
	if rawURL != nil && strings.TrimSpace(*rawURL) != "" {
		return strings.TrimSpace(*rawURL)
	}
	hostValue := ""
	if host != nil {
		hostValue = strings.TrimSpace(*host)
	}
	if hostValue != "" {
		if port != nil && *port > 0 {
			return fmt.Sprintf("%s:%d", hostValue, *port)
		}
		return hostValue
	}
	kindValue := strings.TrimSpace(kind)
	if kindValue == "" {
		return "service"
	}
	return strings.ToLower(kindValue)
}

func (h *Handler) createDefaultCheck(ctx context.Context, tx *gorm.DB, service *db.Service) error {
	var count int64
	if err := tx.WithContext(ctx).Model(&db.Check{}).
		Where("target_type = ? AND target_id = ?", "service", service.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	row := db.Check{
		TargetType:     "service",
		TargetID:       service.ID,
		Type:           checks.ServiceCheckType(service.Kind),
		IntervalSec:    60,
		TimeoutMS:      3000,
		Retries:        1,
		FailAfterSec:   300,
		RecoverAfterOK: 2,
		Enabled:        true,
		SeverityRules:  datatypes.JSON([]byte("{}")),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	return tx.WithContext(ctx).Create(&row).Error
}

func (h *Handler) syncServiceChecks(ctx context.Context, tx *gorm.DB, service *db.Service) error {
	if service == nil {
		return nil
	}
	desiredType := checks.ServiceCheckType(service.Kind)
	result := tx.WithContext(ctx).
		Model(&db.Check{}).
		Where("target_type = ? AND target_id = ? AND lower(type) IN ?", "service", service.ID, []string{"http", "https", "tcp", "ftp"}).
		Updates(map[string]any{
			"type":       desiredType,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return h.createDefaultCheck(ctx, tx, service)
}

func (h *Handler) ListAllServices(c *gin.Context) {
	query := h.DB.WithContext(c.Request.Context()).Model(&db.Service{})
	user, err := h.actorUser(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	query = query.
		Joins("JOIN organization_members om ON om.org_id = services.org_id").
		Where("om.user_id = ?", user.ID)
	if orgID != nil {
		query = query.Where("services.org_id = ?", *orgID)
	}
	var rows []db.Service
	if err := query.Order("services.created_at desc").Find(&rows).Error; err != nil {
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
	user, err := h.actorUser(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil || orgID == nil || *orgID == uuid.Nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "organization required")
		return
	}
	var nodeID *uuid.UUID
	if req.NodeID != nil && strings.TrimSpace(*req.NodeID) != "" {
		node, err := h.getNodeForActor(c, strings.TrimSpace(*req.NodeID))
		if err != nil {
			respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
			return
		}
		if node.OrgID == nil || *node.OrgID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "INVALID_ORG", "node organization missing")
			return
		}
		*orgID = *node.OrgID
		nodeID = &node.ID
	}
	service, err := h.buildServiceFromRequest(*orgID, nodeID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_SERVICE", err.Error())
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(service).Error; err != nil {
			return err
		}
		return h.createDefaultCheck(ctx, tx, service)
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
	if node.OrgID == nil || *node.OrgID == uuid.Nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "node organization missing")
		return
	}
	nodeID := node.ID
	service, err := h.buildServiceFromRequest(*node.OrgID, &nodeID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_SERVICE", err.Error())
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(service).Error; err != nil {
			return err
		}
		return h.createDefaultCheck(ctx, tx, service)
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
	if req.Name != nil {
		service.Name = serviceNameOrDefault(trimPtr(req.Name), service.Kind, service.URL, service.Host, service.Port)
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
	if req.AuthUsername != nil {
		service.AuthUsername = trimPtr(req.AuthUsername)
		if service.AuthUsername == nil {
			service.AuthPasswordEnc = nil
		}
	}
	if req.AuthPassword != nil {
		rawPassword := strings.TrimSpace(*req.AuthPassword)
		if rawPassword != "" {
			if service.AuthUsername == nil {
				respondError(c, http.StatusBadRequest, "INVALID_SERVICE", "auth username required")
				return
			}
			enc, encErr := h.encryptString(rawPassword)
			if encErr != nil {
				respondError(c, http.StatusInternalServerError, "SERVICE_AUTH", "failed to encrypt password")
				return
			}
			service.AuthPasswordEnc = &enc
		}
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
	service.Name = serviceNameOrDefault(&service.Name, service.Kind, service.URL, service.Host, service.Port)
	service.UpdatedAt = time.Now()
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Save(service).Error; err != nil {
			return err
		}
		return h.syncServiceChecks(ctx, tx, service)
	}); err != nil {
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
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "failed to run check"
		}
		respondError(c, http.StatusInternalServerError, "RUN_FAILED", msg)
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
