package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

const (
	defaultPromObsMode              = "embedded"
	defaultPromObsURL               = "http://prometheus:9090"
	defaultPromObsReloadMethod      = "http"
	defaultPromObsContainerName     = "prometheus"
	defaultPromObsScheme            = "http"
	defaultPromObsMetricsPath       = "/metrics"
	defaultPromObsInterval          = "15s"
	defaultPromObsTimeout           = "5s"
	defaultPromObsReloadTimeout     = 10 * time.Second
	defaultPromObsTargetTestTimeout = 5 * time.Second
)

type promObservabilitySettingsResponse struct {
	Mode                string            `json:"mode"`
	PromURL             string            `json:"prom_url"`
	ReloadMethod        string            `json:"reload_method"`
	PromContainerName   string            `json:"prom_container_name"`
	DefaultScheme       string            `json:"default_scheme"`
	DefaultMetricsPath  string            `json:"default_metrics_path"`
	DefaultInterval     string            `json:"default_interval"`
	DefaultTimeout      string            `json:"default_timeout"`
	DefaultLabels       map[string]string `json:"default_labels"`
	AllowExternalReload bool              `json:"allow_external_reload"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type promObservabilitySettingsRequest struct {
	Mode                string            `json:"mode"`
	PromURL             string            `json:"prom_url"`
	ReloadMethod        string            `json:"reload_method"`
	PromContainerName   string            `json:"prom_container_name"`
	DefaultScheme       string            `json:"default_scheme"`
	DefaultMetricsPath  string            `json:"default_metrics_path"`
	DefaultInterval     string            `json:"default_interval"`
	DefaultTimeout      string            `json:"default_timeout"`
	DefaultLabels       map[string]string `json:"default_labels"`
	AllowExternalReload *bool             `json:"allow_external_reload"`
}

type promObservabilityTargetResponse struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Scheme         string            `json:"scheme"`
	Address        string            `json:"address"`
	MetricsPath    string            `json:"metrics_path"`
	Interval       string            `json:"interval"`
	Timeout        string            `json:"timeout"`
	Labels         map[string]string `json:"labels"`
	Enabled        bool              `json:"enabled"`
	AuthType       string            `json:"auth_type"`
	AuthUsername   string            `json:"auth_username"`
	AuthConfigured bool              `json:"auth_configured"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type promObservabilityTargetCreateRequest struct {
	Name            string            `json:"name"`
	Scheme          string            `json:"scheme"`
	Address         string            `json:"address"`
	MetricsPath     string            `json:"metrics_path"`
	Interval        string            `json:"interval"`
	Timeout         string            `json:"timeout"`
	Labels          map[string]string `json:"labels"`
	Enabled         *bool             `json:"enabled"`
	AuthType        string            `json:"auth_type"`
	AuthUsername    string            `json:"auth_username"`
	AuthPassword    string            `json:"auth_password"`
	AuthBearerToken string            `json:"auth_bearer_token"`
}

type promObservabilityTargetUpdateRequest struct {
	Name            *string            `json:"name"`
	Scheme          *string            `json:"scheme"`
	Address         *string            `json:"address"`
	MetricsPath     *string            `json:"metrics_path"`
	Interval        *string            `json:"interval"`
	Timeout         *string            `json:"timeout"`
	Labels          *map[string]string `json:"labels"`
	Enabled         *bool              `json:"enabled"`
	AuthType        *string            `json:"auth_type"`
	AuthUsername    *string            `json:"auth_username"`
	AuthPassword    *string            `json:"auth_password"`
	AuthBearerToken *string            `json:"auth_bearer_token"`
}

type promObservabilityTargetTestRequest struct {
	TargetID        string `json:"target_id"`
	Scheme          string `json:"scheme"`
	Address         string `json:"address"`
	MetricsPath     string `json:"metrics_path"`
	Timeout         string `json:"timeout"`
	AuthType        string `json:"auth_type"`
	AuthUsername    string `json:"auth_username"`
	AuthPassword    string `json:"auth_password"`
	AuthBearerToken string `json:"auth_bearer_token"`
	SkipTLSVerify   bool   `json:"skip_tls_verify"`
}

type promSDTargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func (h *Handler) GetPromObservabilitySettings(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	row, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}
	respondStatus(c, http.StatusOK, promObservabilitySettingsToResponse(row))
}

func (h *Handler) UpdatePromObservabilitySettings(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	var req promObservabilitySettingsRequest
	if !parseJSONBody(c, &req) {
		return
	}
	current, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}

	mode := normalizePromObsMode(req.Mode)
	if strings.TrimSpace(req.Mode) == "" {
		mode = normalizePromObsMode(current.Mode)
	}

	reloadMethod := normalizePromObsReloadMethod(req.ReloadMethod)
	if strings.TrimSpace(req.ReloadMethod) == "" {
		reloadMethod = normalizePromObsReloadMethod(current.ReloadMethod)
	}

	promURL := strings.TrimSpace(req.PromURL)
	if promURL == "" {
		promURL = strings.TrimSpace(current.PromURL)
	}
	if mode == defaultPromObsMode && promURL == "" {
		promURL = defaultPromObsURL
	}
	if promURL != "" {
		if _, err := validatePrometheusBaseURL(promURL); err != nil {
			respondError(c, http.StatusBadRequest, "PROM_SETTINGS_URL", err.Error())
			return
		}
		promURL = strings.TrimRight(promURL, "/")
	}

	containerName := strings.TrimSpace(req.PromContainerName)
	if containerName == "" {
		containerName = strings.TrimSpace(current.PromContainerName)
	}
	if containerName == "" {
		containerName = defaultPromObsContainerName
	}

	defaultScheme := normalizePromObsScheme(req.DefaultScheme)
	if strings.TrimSpace(req.DefaultScheme) == "" {
		defaultScheme = normalizePromObsScheme(current.DefaultScheme)
	}

	defaultPath := sanitizePromObsMetricsPath(req.DefaultMetricsPath, "")
	if strings.TrimSpace(req.DefaultMetricsPath) == "" {
		defaultPath = sanitizePromObsMetricsPath(current.DefaultMetricsPath, "")
	}

	defaultInterval, err := normalizePromObsDuration(req.DefaultInterval, current.DefaultInterval, defaultPromObsInterval)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_SETTINGS_INTERVAL", err.Error())
		return
	}
	defaultTimeout, err := normalizePromObsDuration(req.DefaultTimeout, current.DefaultTimeout, defaultPromObsTimeout)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_SETTINGS_TIMEOUT", err.Error())
		return
	}

	defaultLabels := decodePromStringMap(current.DefaultLabels)
	if req.DefaultLabels != nil {
		defaultLabels = sanitizePromStringMap(req.DefaultLabels)
	}

	allowExternalReload := current.AllowExternalReload
	if req.AllowExternalReload != nil {
		allowExternalReload = *req.AllowExternalReload
	}

	updates := map[string]any{
		"mode":                  mode,
		"prom_url":              promURL,
		"reload_method":         reloadMethod,
		"prom_container_name":   containerName,
		"default_scheme":        defaultScheme,
		"default_metrics_path":  defaultPath,
		"default_interval":      defaultInterval,
		"default_timeout":       defaultTimeout,
		"default_labels":        encodePromStringMap(defaultLabels),
		"allow_external_reload": allowExternalReload,
		"updated_at":            time.Now().UTC(),
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Model(&db.PromSetting{}).
		Where("org_id = ?", orgID).
		Updates(updates).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_SAVE", "failed to save settings")
		return
	}

	row, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}

	if _, _, err := h.rebuildPromSDForOrg(c.Request.Context(), orgID); err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_WRITE", "failed to write service discovery file")
		return
	}

	respondStatus(c, http.StatusOK, promObservabilitySettingsToResponse(row))
}

func (h *Handler) ListPromObservabilityTargets(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	var targets []db.PromTarget
	if err := h.DB.WithContext(c.Request.Context()).
		Where("org_id = ?", orgID).
		Order("created_at DESC").
		Find(&targets).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_TARGETS_LIST", "failed to load targets")
		return
	}
	resp := make([]promObservabilityTargetResponse, 0, len(targets))
	for i := range targets {
		resp = append(resp, promObservabilityTargetToResponse(targets[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}
func (h *Handler) CreatePromObservabilityTarget(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	var req promObservabilityTargetCreateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	settings, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_NAME", "name is required")
		return
	}
	address := strings.TrimSpace(req.Address)
	if address == "" {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_ADDRESS", "address is required")
		return
	}
	scheme := normalizePromObsScheme(firstNonEmpty(req.Scheme, settings.DefaultScheme))
	metricsPath := sanitizePromObsMetricsPath(req.MetricsPath, settings.DefaultMetricsPath)
	interval, err := normalizePromObsDuration(req.Interval, settings.DefaultInterval, defaultPromObsInterval)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_INTERVAL", err.Error())
		return
	}
	timeout, err := normalizePromObsDuration(req.Timeout, settings.DefaultTimeout, defaultPromObsTimeout)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_TIMEOUT", err.Error())
		return
	}
	authType := normalizePromObsAuthType(req.AuthType)
	authUsername := strings.TrimSpace(req.AuthUsername)
	authPasswordEnc := ""
	authBearerEnc := ""
	switch authType {
	case "basic":
		if authUsername == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_username is required for basic auth")
			return
		}
		password := strings.TrimSpace(req.AuthPassword)
		if password == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_password is required for basic auth")
			return
		}
		enc, err := h.encryptString(password)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to encrypt auth password")
			return
		}
		authPasswordEnc = enc
	case "bearer":
		token := strings.TrimSpace(req.AuthBearerToken)
		if token == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_bearer_token is required for bearer auth")
			return
		}
		enc, err := h.encryptString(token)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to encrypt bearer token")
			return
		}
		authBearerEnc = enc
	default:
		authUsername = ""
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row := db.PromTarget{
		OrgID:           orgID,
		Name:            name,
		Scheme:          scheme,
		Address:         address,
		MetricsPath:     metricsPath,
		Interval:        interval,
		Timeout:         timeout,
		Labels:          encodePromStringMap(req.Labels),
		Enabled:         enabled,
		AuthType:        authType,
		AuthUsername:    authUsername,
		AuthPasswordEnc: authPasswordEnc,
		AuthBearerEnc:   authBearerEnc,
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		if isPromUniqueViolation(err) {
			respondError(c, http.StatusConflict, "PROM_TARGET_DUPLICATE", "target name already exists in organization")
			return
		}
		respondError(c, http.StatusInternalServerError, "PROM_TARGET_CREATE", "failed to create target")
		return
	}
	if _, _, err := h.rebuildPromSDForOrg(c.Request.Context(), orgID); err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_WRITE", "failed to write service discovery file")
		return
	}
	respondStatus(c, http.StatusCreated, promObservabilityTargetToResponse(row))
}

func (h *Handler) UpdatePromObservabilityTarget(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(c.Param("targetId")))
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_ID", "invalid target id")
		return
	}
	var req promObservabilityTargetUpdateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	settings, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}
	target, err := h.getPromObservabilityTarget(c.Request.Context(), orgID, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "PROM_TARGET_NOT_FOUND", "target not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "PROM_TARGET_LOAD", "failed to load target")
		return
	}

	name := strings.TrimSpace(target.Name)
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_NAME", "name is required")
		return
	}
	address := strings.TrimSpace(target.Address)
	if req.Address != nil {
		address = strings.TrimSpace(*req.Address)
	}
	if address == "" {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_ADDRESS", "address is required")
		return
	}
	scheme := normalizePromObsScheme(target.Scheme)
	if req.Scheme != nil {
		scheme = normalizePromObsScheme(*req.Scheme)
	}
	metricsPath := sanitizePromObsMetricsPath(target.MetricsPath, settings.DefaultMetricsPath)
	if req.MetricsPath != nil {
		metricsPath = sanitizePromObsMetricsPath(*req.MetricsPath, settings.DefaultMetricsPath)
	}
	interval, err := normalizePromObsDuration(target.Interval, settings.DefaultInterval, defaultPromObsInterval)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_INTERVAL", err.Error())
		return
	}
	if req.Interval != nil {
		interval, err = normalizePromObsDuration(*req.Interval, settings.DefaultInterval, defaultPromObsInterval)
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_INTERVAL", err.Error())
			return
		}
	}
	timeout, err := normalizePromObsDuration(target.Timeout, settings.DefaultTimeout, defaultPromObsTimeout)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_TIMEOUT", err.Error())
		return
	}
	if req.Timeout != nil {
		timeout, err = normalizePromObsDuration(*req.Timeout, settings.DefaultTimeout, defaultPromObsTimeout)
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_TIMEOUT", err.Error())
			return
		}
	}

	labels := decodePromStringMap(target.Labels)
	if req.Labels != nil {
		labels = sanitizePromStringMap(*req.Labels)
	}

	enabled := target.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	currentAuthType := normalizePromObsAuthType(target.AuthType)
	authType := currentAuthType
	if req.AuthType != nil {
		authType = normalizePromObsAuthType(*req.AuthType)
	}
	authUsername := strings.TrimSpace(target.AuthUsername)
	authPasswordEnc := strings.TrimSpace(target.AuthPasswordEnc)
	authBearerEnc := strings.TrimSpace(target.AuthBearerEnc)
	authTypeChanged := authType != currentAuthType
	if authTypeChanged {
		if authType != "basic" {
			authUsername = ""
			authPasswordEnc = ""
		}
		if authType != "bearer" {
			authBearerEnc = ""
		}
	}

	switch authType {
	case "none":
		authUsername = ""
		authPasswordEnc = ""
		authBearerEnc = ""
	case "basic":
		if req.AuthUsername != nil {
			authUsername = strings.TrimSpace(*req.AuthUsername)
		}
		if authUsername == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_username is required for basic auth")
			return
		}
		if req.AuthPassword != nil {
			raw := strings.TrimSpace(*req.AuthPassword)
			if raw == "" {
				if authPasswordEnc == "" {
					respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_password is required for basic auth")
					return
				}
			} else {
				enc, err := h.encryptString(raw)
				if err != nil {
					respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to encrypt auth password")
					return
				}
				authPasswordEnc = enc
			}
		}
		if authPasswordEnc == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_password is required for basic auth")
			return
		}
		authBearerEnc = ""
	case "bearer":
		if req.AuthBearerToken != nil {
			raw := strings.TrimSpace(*req.AuthBearerToken)
			if raw == "" {
				if authBearerEnc == "" {
					respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_bearer_token is required for bearer auth")
					return
				}
			} else {
				enc, err := h.encryptString(raw)
				if err != nil {
					respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to encrypt bearer token")
					return
				}
				authBearerEnc = enc
			}
		}
		if authBearerEnc == "" {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_AUTH", "auth_bearer_token is required for bearer auth")
			return
		}
		authUsername = ""
		authPasswordEnc = ""
	}

	updates := map[string]any{
		"name":                  name,
		"scheme":                scheme,
		"address":               address,
		"metrics_path":          metricsPath,
		"interval":              interval,
		"timeout":               timeout,
		"labels":                encodePromStringMap(labels),
		"enabled":               enabled,
		"auth_type":             authType,
		"auth_username":         authUsername,
		"auth_password_enc":     authPasswordEnc,
		"auth_bearer_token_enc": authBearerEnc,
		"updated_at":            time.Now().UTC(),
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Model(&db.PromTarget{}).
		Where("id = ? AND org_id = ?", targetID, orgID).
		Updates(updates).Error; err != nil {
		if isPromUniqueViolation(err) {
			respondError(c, http.StatusConflict, "PROM_TARGET_DUPLICATE", "target name already exists in organization")
			return
		}
		respondError(c, http.StatusInternalServerError, "PROM_TARGET_UPDATE", "failed to update target")
		return
	}

	updated, err := h.getPromObservabilityTarget(c.Request.Context(), orgID, targetID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_TARGET_LOAD", "failed to load target")
		return
	}
	if _, _, err := h.rebuildPromSDForOrg(c.Request.Context(), orgID); err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_WRITE", "failed to write service discovery file")
		return
	}
	respondStatus(c, http.StatusOK, promObservabilityTargetToResponse(updated))
}

func (h *Handler) DeletePromObservabilityTarget(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	targetID, err := uuid.Parse(strings.TrimSpace(c.Param("targetId")))
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_ID", "invalid target id")
		return
	}
	res := h.DB.WithContext(c.Request.Context()).
		Where("id = ? AND org_id = ?", targetID, orgID).
		Delete(&db.PromTarget{})
	if res.Error != nil {
		respondError(c, http.StatusInternalServerError, "PROM_TARGET_DELETE", "failed to delete target")
		return
	}
	if res.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, "PROM_TARGET_NOT_FOUND", "target not found")
		return
	}
	if _, _, err := h.rebuildPromSDForOrg(c.Request.Context(), orgID); err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_WRITE", "failed to write service discovery file")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}
func (h *Handler) GetPromObservabilitySD(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	groups, path, err := h.rebuildPromSDForOrg(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_WRITE", "failed to build service discovery file")
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SD_READ", "failed to read service discovery file")
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(path)))
		c.Data(http.StatusOK, "application/json", raw)
		return
	}
	respondStatus(c, http.StatusOK, gin.H{
		"path":    path,
		"targets": groups,
		"raw":     json.RawMessage(raw),
	})
}

func (h *Handler) ReloadPromObservability(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	settings, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}
	mode := normalizePromObsMode(settings.Mode)
	method := normalizePromObsReloadMethod(settings.ReloadMethod)

	switch method {
	case "manual":
		respondStatus(c, http.StatusOK, gin.H{
			"ok":            false,
			"mode":          mode,
			"reload_method": method,
			"message":       "manual reload selected; apply reload in Prometheus manually",
		})
		return
	case "docker_hup":
		if mode != defaultPromObsMode {
			respondStatus(c, http.StatusOK, gin.H{
				"ok":            false,
				"mode":          mode,
				"reload_method": method,
				"message":       "docker_hup is available only in embedded mode",
			})
			return
		}
		container := strings.TrimSpace(settings.PromContainerName)
		if container == "" {
			container = defaultPromObsContainerName
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), defaultPromObsReloadTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", "kill", "-s", "HUP", container).CombinedOutput()
		if err != nil {
			respondStatus(c, http.StatusOK, gin.H{
				"ok":            false,
				"mode":          mode,
				"reload_method": method,
				"message":       strings.TrimSpace(string(out)),
				"error":         err.Error(),
			})
			return
		}
		respondStatus(c, http.StatusOK, gin.H{
			"ok":            true,
			"mode":          mode,
			"reload_method": method,
			"message":       strings.TrimSpace(string(out)),
		})
		return
	default:
		promURL := strings.TrimRight(strings.TrimSpace(settings.PromURL), "/")
		if promURL == "" && mode == defaultPromObsMode {
			promURL = defaultPromObsURL
		}
		if mode == "external" && !settings.AllowExternalReload {
			respondStatus(c, http.StatusOK, gin.H{
				"ok":            false,
				"mode":          mode,
				"reload_method": method,
				"message":       "external reload is disabled by org settings",
			})
			return
		}
		if promURL == "" {
			respondStatus(c, http.StatusOK, gin.H{
				"ok":            false,
				"mode":          mode,
				"reload_method": method,
				"message":       "prom_url is empty; reload manually or set prom_url",
			})
			return
		}
		statusCode, body, err := doPromObservabilityHTTPReload(c.Request.Context(), promURL)
		if err != nil {
			respondStatus(c, http.StatusOK, gin.H{
				"ok":            false,
				"mode":          mode,
				"reload_method": method,
				"prom_url":      promURL,
				"status_code":   statusCode,
				"message":       body,
				"error":         err.Error(),
			})
			return
		}
		respondStatus(c, http.StatusOK, gin.H{
			"ok":            true,
			"mode":          mode,
			"reload_method": method,
			"prom_url":      promURL,
			"status_code":   statusCode,
			"message":       body,
		})
	}
}

func (h *Handler) TestPromObservabilityTarget(c *gin.Context) {
	orgID, err := promOrgIDFromContext(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
		return
	}
	var req promObservabilityTargetTestRequest
	if !parseJSONBody(c, &req) {
		return
	}
	settings, err := h.ensurePromObservabilitySettings(c.Request.Context(), orgID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROM_SETTINGS_LOAD", "failed to load settings")
		return
	}

	scheme := normalizePromObsScheme(firstNonEmpty(req.Scheme, settings.DefaultScheme))
	address := strings.TrimSpace(req.Address)
	metricsPath := sanitizePromObsMetricsPath(req.MetricsPath, settings.DefaultMetricsPath)
	timeoutRaw := firstNonEmpty(req.Timeout, settings.DefaultTimeout)
	timeoutRaw, err = normalizePromObsDuration(timeoutRaw, settings.DefaultTimeout, defaultPromObsTimeout)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_TEST_TIMEOUT", err.Error())
		return
	}
	timeoutDur, _ := time.ParseDuration(timeoutRaw)
	if timeoutDur <= 0 {
		timeoutDur = defaultPromObsTargetTestTimeout
	}
	authType := normalizePromObsAuthType(req.AuthType)
	authUsername := strings.TrimSpace(req.AuthUsername)
	authPassword := strings.TrimSpace(req.AuthPassword)
	authBearer := strings.TrimSpace(req.AuthBearerToken)

	if strings.TrimSpace(req.TargetID) != "" {
		targetID, err := uuid.Parse(strings.TrimSpace(req.TargetID))
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_ID", "invalid target id")
			return
		}
		target, err := h.getPromObservabilityTarget(c.Request.Context(), orgID, targetID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "PROM_TARGET_NOT_FOUND", "target not found")
				return
			}
			respondError(c, http.StatusInternalServerError, "PROM_TARGET_LOAD", "failed to load target")
			return
		}
		scheme = normalizePromObsScheme(target.Scheme)
		address = strings.TrimSpace(target.Address)
		metricsPath = sanitizePromObsMetricsPath(target.MetricsPath, settings.DefaultMetricsPath)
		timeoutRaw, err = normalizePromObsDuration(target.Timeout, settings.DefaultTimeout, defaultPromObsTimeout)
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROM_TARGET_TEST_TIMEOUT", err.Error())
			return
		}
		timeoutDur, _ = time.ParseDuration(timeoutRaw)
		if timeoutDur <= 0 {
			timeoutDur = defaultPromObsTargetTestTimeout
		}
		authType = normalizePromObsAuthType(target.AuthType)
		authUsername = strings.TrimSpace(target.AuthUsername)
		switch authType {
		case "basic":
			authPassword, err = h.decryptString(target.AuthPasswordEnc)
			if err != nil {
				respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to decrypt auth password")
				return
			}
		case "bearer":
			authBearer, err = h.decryptString(target.AuthBearerEnc)
			if err != nil {
				respondError(c, http.StatusInternalServerError, "PROM_TARGET_AUTH", "failed to decrypt bearer token")
				return
			}
		}
	}

	if address == "" {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_ADDRESS", "address is required")
		return
	}
	targetURL := fmt.Sprintf("%s://%s%s", scheme, address, metricsPath)
	if _, err := url.Parse(targetURL); err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_URL", "invalid target url")
		return
	}

	client := &http.Client{
		Timeout: timeoutDur,
		Transport: &http.Transport{
			TLSClientConfig: insecureTLS(!req.SkipTLSVerify),
		},
	}
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROM_TARGET_URL", "invalid target url")
		return
	}
	switch authType {
	case "basic":
		httpReq.SetBasicAuth(authUsername, authPassword)
	case "bearer":
		if authBearer != "" {
			httpReq.Header.Set("Authorization", "Bearer "+authBearer)
		}
	}

	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		respondStatus(c, http.StatusOK, gin.H{
			"ok":         false,
			"url":        targetURL,
			"latency_ms": time.Since(start).Milliseconds(),
			"error":      err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	previewLines := promMetricsPreview(raw, 50)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	respondStatus(c, http.StatusOK, gin.H{
		"ok":            ok,
		"url":           targetURL,
		"status_code":   resp.StatusCode,
		"status":        resp.Status,
		"latency_ms":    time.Since(start).Milliseconds(),
		"timeout":       timeoutRaw,
		"preview_lines": previewLines,
		"preview":       strings.Join(previewLines, "\n"),
		"error":         "",
	})
}
func promOrgIDFromContext(c *gin.Context) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.GetString("org_id"))
	if raw == "" {
		raw = strings.TrimSpace(c.Param("orgId"))
	}
	return uuid.Parse(raw)
}

func promObservabilitySettingsToResponse(row db.PromSetting) promObservabilitySettingsResponse {
	return promObservabilitySettingsResponse{
		Mode:                normalizePromObsMode(row.Mode),
		PromURL:             strings.TrimSpace(row.PromURL),
		ReloadMethod:        normalizePromObsReloadMethod(row.ReloadMethod),
		PromContainerName:   strings.TrimSpace(row.PromContainerName),
		DefaultScheme:       normalizePromObsScheme(row.DefaultScheme),
		DefaultMetricsPath:  sanitizePromObsMetricsPath(row.DefaultMetricsPath, defaultPromObsMetricsPath),
		DefaultInterval:     coalescePromObsDuration(row.DefaultInterval, defaultPromObsInterval),
		DefaultTimeout:      coalescePromObsDuration(row.DefaultTimeout, defaultPromObsTimeout),
		DefaultLabels:       decodePromStringMap(row.DefaultLabels),
		AllowExternalReload: row.AllowExternalReload,
		UpdatedAt:           row.UpdatedAt,
	}
}

func promObservabilityTargetToResponse(row db.PromTarget) promObservabilityTargetResponse {
	authType := normalizePromObsAuthType(row.AuthType)
	authConfigured := false
	switch authType {
	case "basic":
		authConfigured = strings.TrimSpace(row.AuthPasswordEnc) != ""
	case "bearer":
		authConfigured = strings.TrimSpace(row.AuthBearerEnc) != ""
	}
	return promObservabilityTargetResponse{
		ID:             row.ID.String(),
		Name:           strings.TrimSpace(row.Name),
		Scheme:         normalizePromObsScheme(row.Scheme),
		Address:        strings.TrimSpace(row.Address),
		MetricsPath:    sanitizePromObsMetricsPath(row.MetricsPath, defaultPromObsMetricsPath),
		Interval:       coalescePromObsDuration(row.Interval, defaultPromObsInterval),
		Timeout:        coalescePromObsDuration(row.Timeout, defaultPromObsTimeout),
		Labels:         decodePromStringMap(row.Labels),
		Enabled:        row.Enabled,
		AuthType:       authType,
		AuthUsername:   strings.TrimSpace(row.AuthUsername),
		AuthConfigured: authConfigured,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (h *Handler) getPromObservabilityTarget(ctx context.Context, orgID, targetID uuid.UUID) (db.PromTarget, error) {
	var row db.PromTarget
	err := h.DB.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgID, targetID).
		First(&row).Error
	if err != nil {
		return db.PromTarget{}, err
	}
	return row, nil
}

func (h *Handler) ensurePromObservabilitySettings(ctx context.Context, orgID uuid.UUID) (db.PromSetting, error) {
	var row db.PromSetting
	err := h.DB.WithContext(ctx).Where("org_id = ?", orgID).First(&row).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return db.PromSetting{}, err
	}
	row = db.PromSetting{
		OrgID:               orgID,
		Mode:                defaultPromObsMode,
		PromURL:             defaultPromObsURL,
		ReloadMethod:        defaultPromObsReloadMethod,
		PromContainerName:   defaultPromObsContainerName,
		DefaultScheme:       defaultPromObsScheme,
		DefaultMetricsPath:  defaultPromObsMetricsPath,
		DefaultInterval:     defaultPromObsInterval,
		DefaultTimeout:      defaultPromObsTimeout,
		DefaultLabels:       encodePromStringMap(map[string]string{}),
		AllowExternalReload: false,
	}
	createErr := h.DB.WithContext(ctx).Create(&row).Error
	if createErr == nil {
		return row, nil
	}
	if err := h.DB.WithContext(ctx).Where("org_id = ?", orgID).First(&row).Error; err != nil {
		return db.PromSetting{}, createErr
	}
	return row, nil
}

func (h *Handler) rebuildPromSDForOrg(ctx context.Context, orgID uuid.UUID) ([]promSDTargetGroup, string, error) {
	settings, err := h.ensurePromObservabilitySettings(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	var targets []db.PromTarget
	if err := h.DB.WithContext(ctx).
		Where("org_id = ? AND enabled = ?", orgID, true).
		Order("created_at ASC").
		Find(&targets).Error; err != nil {
		return nil, "", err
	}

	defaultLabels := decodePromStringMap(settings.DefaultLabels)
	groups := make([]promSDTargetGroup, 0, len(targets))
	for i := range targets {
		target := targets[i]
		address := strings.TrimSpace(target.Address)
		if address == "" {
			continue
		}
		labels := make(map[string]string, len(defaultLabels)+8)
		for k, v := range defaultLabels {
			labels[k] = v
		}
		for k, v := range decodePromStringMap(target.Labels) {
			labels[k] = v
		}
		labels["org_id"] = orgID.String()
		labels["target_name"] = strings.TrimSpace(target.Name)
		labels["__scheme__"] = normalizePromObsScheme(target.Scheme)
		labels["__metrics_path__"] = sanitizePromObsMetricsPath(target.MetricsPath, settings.DefaultMetricsPath)
		labels["__scrape_interval__"] = coalescePromObsDuration(target.Interval, settings.DefaultInterval)
		labels["__scrape_timeout__"] = coalescePromObsDuration(target.Timeout, settings.DefaultTimeout)
		groups = append(groups, promSDTargetGroup{
			Targets: []string{address},
			Labels:  sanitizePromStringMap(labels),
		})
	}

	path := h.promSDFilePath(orgID)
	if err := writePromSDFile(path, groups); err != nil {
		return nil, "", err
	}
	return groups, path, nil
}

func (h *Handler) promSDFilePath(orgID uuid.UUID) string {
	base := strings.TrimSpace(h.DataDir)
	if base == "" {
		base = "./data"
	}
	return filepath.Join(base, "prom_sd", fmt.Sprintf("org_%s.json", strings.ToLower(orgID.String())))
}

func writePromSDFile(path string, groups []promSDTargetGroup) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(groups, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func decodePromStringMap(raw datatypes.JSON) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	var src map[string]any
	if err := json.Unmarshal(raw, &src); err != nil {
		return out
	}
	for k, v := range src {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		val := strings.TrimSpace(fmt.Sprint(v))
		if val == "" || val == "<nil>" {
			continue
		}
		out[key] = val
	}
	return out
}

func encodePromStringMap(in map[string]string) datatypes.JSON {
	clean := sanitizePromStringMap(in)
	raw, _ := json.Marshal(clean)
	return datatypes.JSON(raw)
}

func sanitizePromStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out
}
func normalizePromObsMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "external":
		return "external"
	default:
		return defaultPromObsMode
	}
}

func normalizePromObsReloadMethod(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "docker_hup":
		return "docker_hup"
	case "manual":
		return "manual"
	default:
		return defaultPromObsReloadMethod
	}
}

func normalizePromObsScheme(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "https":
		return "https"
	default:
		return defaultPromObsScheme
	}
}

func normalizePromObsAuthType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "basic":
		return "basic"
	case "bearer":
		return "bearer"
	default:
		return "none"
	}
}

func sanitizePromObsMetricsPath(v, fallback string) string {
	path := strings.TrimSpace(v)
	if path == "" {
		path = strings.TrimSpace(fallback)
	}
	if path == "" {
		path = defaultPromObsMetricsPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func coalescePromObsDuration(v, fallback string) string {
	out, err := normalizePromObsDuration(v, fallback, fallback)
	if err != nil {
		return fallback
	}
	return out
}

func normalizePromObsDuration(v, fallback, hardDefault string) (string, error) {
	raw := strings.TrimSpace(v)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		raw = strings.TrimSpace(hardDefault)
	}
	dur, err := time.ParseDuration(raw)
	if err != nil {
		return "", fmt.Errorf("invalid duration %q", raw)
	}
	if dur <= 0 {
		return "", fmt.Errorf("duration must be greater than zero")
	}
	return raw, nil
}

func isPromUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(err.Error())
	return strings.Contains(raw, "duplicate key") || strings.Contains(raw, "unique constraint")
}

func doPromObservabilityHTTPReload(ctx context.Context, baseURL string) (int, string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return 0, "", errors.New("prom_url is empty")
	}
	if _, err := validatePrometheusBaseURL(trimmed); err != nil {
		return 0, "", err
	}
	reloadURL := trimmed + "/-/reload"
	reloadCtx, cancel := context.WithTimeout(ctx, defaultPromObsReloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reloadCtx, http.MethodPost, reloadURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, message, fmt.Errorf("reload failed with status %d", resp.StatusCode)
	}
	return resp.StatusCode, message, nil
}

func promMetricsPreview(raw []byte, limit int) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	lines := make([]string, 0, limit)
	for scanner.Scan() {
		if len(lines) >= limit {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(line) > 500 {
			line = line[:500]
		}
		lines = append(lines, line)
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v != "" {
			return v
		}
	}
	return ""
}
