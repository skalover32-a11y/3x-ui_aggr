package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
)

type serviceRequest struct {
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

func (h *Handler) ListServices(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
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
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req serviceRequest
	if !parseJSONBody(c, &req) {
		return
	}
	headers, err := headersToJSON(req.Headers)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_HEADERS", "invalid headers")
		return
	}
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	service := db.Service{
		NodeID:         node.ID,
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
	if err := h.DB.WithContext(c.Request.Context()).Create(&service).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create service")
		return
	}
	respondStatus(c, http.StatusCreated, toServiceResponse(&service))
}

func (h *Handler) UpdateService(c *gin.Context) {
	service, err := h.getService(c.Request.Context(), c.Param("serviceId"))
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
	service, err := h.getService(c.Request.Context(), c.Param("serviceId"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "service not found")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Delete(&db.Service{}, "id = ?", service.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete service")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
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
