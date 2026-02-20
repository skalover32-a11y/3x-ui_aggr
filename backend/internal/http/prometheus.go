package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

const (
	defaultPrometheusTimeoutMS    = 5000
	minPrometheusTimeoutMS        = 1000
	maxPrometheusTimeoutMS        = 60000
	defaultPrometheusStepSec      = 60
	minPrometheusStepSec          = 5
	maxPrometheusStepSec          = 3600
	defaultPrometheusRangeMinutes = 60
)

type prometheusSettingsResponse struct {
	Enabled               bool   `json:"enabled"`
	BaseURL               string `json:"base_url"`
	AuthType              string `json:"auth_type"`
	Username              string `json:"username"`
	PasswordSet           bool   `json:"password_set"`
	BearerTokenSet        bool   `json:"bearer_token_set"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
	TimeoutMS             int    `json:"timeout_ms"`
	DefaultStepSec        int    `json:"default_step_sec"`
}

type prometheusSettingsRequest struct {
	Enabled               bool   `json:"enabled"`
	BaseURL               string `json:"base_url"`
	AuthType              string `json:"auth_type"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	BearerToken           string `json:"bearer_token"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
	TimeoutMS             int    `json:"timeout_ms"`
	DefaultStepSec        int    `json:"default_step_sec"`
}

type prometheusTestRequest struct {
	Query string `json:"query"`
}

type prometheusQueryRequest struct {
	Query    string `json:"query"`
	Instant  *bool  `json:"instant"`
	Time     string `json:"time"`
	Start    string `json:"start"`
	End      string `json:"end"`
	StepSec  int    `json:"step_sec"`
	TimeoutS int    `json:"timeout_sec"`
}

type prometheusAPIResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
	ErrorType string   `json:"errorType"`
	Error     string   `json:"error"`
	Warnings  []string `json:"warnings"`
}

type prometheusRuntimeConfig struct {
	Enabled               bool
	BaseURL               string
	AuthType              string
	Username              string
	Password              string
	BearerToken           string
	TLSInsecureSkipVerify bool
	Timeout               time.Duration
	DefaultStepSec        int
}

func (h *Handler) GetPrometheusSettings(c *gin.Context) {
	orgID, err := h.resolveOrgFromRequest(c, true)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	row, err := h.getPrometheusSettings(c, orgID)
	if err != nil {
		respondStatus(c, http.StatusOK, prometheusSettingsResponse{
			Enabled:        false,
			AuthType:       "none",
			TimeoutMS:      defaultPrometheusTimeoutMS,
			DefaultStepSec: defaultPrometheusStepSec,
		})
		return
	}
	respondStatus(c, http.StatusOK, prometheusSettingsResponse{
		Enabled:               row.Enabled,
		BaseURL:               strings.TrimSpace(row.BaseURL),
		AuthType:              normalizePrometheusAuthType(row.AuthType),
		Username:              strings.TrimSpace(row.Username),
		PasswordSet:           strings.TrimSpace(row.PasswordEnc) != "",
		BearerTokenSet:        strings.TrimSpace(row.BearerTokenEnc) != "",
		TLSInsecureSkipVerify: row.TLSInsecureSkipVerify,
		TimeoutMS:             sanitizePrometheusTimeoutMS(row.TimeoutMS),
		DefaultStepSec:        sanitizePrometheusStepSec(row.DefaultStepSec, defaultPrometheusStepSec),
	})
}

func (h *Handler) UpdatePrometheusSettings(c *gin.Context) {
	var req prometheusSettingsRequest
	if !parseJSONBody(c, &req) {
		return
	}
	orgID, err := h.resolveOrgFromRequest(c, true)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	current, err := h.getPrometheusSettings(c, orgID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusInternalServerError, "PROMETHEUS_LOAD", "failed to load settings")
		return
	}
	authType := normalizePrometheusAuthType(req.AuthType)
	if authType != "none" && authType != "basic" && authType != "bearer" {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_AUTH", "auth_type must be one of: none, basic, bearer")
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(current.BaseURL)
	}
	if req.Enabled && baseURL == "" {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_URL", "base_url is required when integration is enabled")
		return
	}
	if baseURL != "" {
		if _, err := validatePrometheusBaseURL(baseURL); err != nil {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_URL", err.Error())
			return
		}
	}

	timeoutMS := req.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = current.TimeoutMS
	}
	timeoutMS = sanitizePrometheusTimeoutMS(timeoutMS)

	defaultStepSec := req.DefaultStepSec
	if defaultStepSec <= 0 {
		defaultStepSec = current.DefaultStepSec
	}
	defaultStepSec = sanitizePrometheusStepSec(defaultStepSec, defaultPrometheusStepSec)

	username := ""
	passwordEnc := ""
	bearerTokenEnc := ""

	switch authType {
	case "none":
		// no-op: secrets are cleared
	case "basic":
		username = strings.TrimSpace(req.Username)
		if username == "" {
			username = strings.TrimSpace(current.Username)
		}
		if username == "" {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_AUTH", "username is required for basic auth")
			return
		}
		passwordRaw := strings.TrimSpace(req.Password)
		if passwordRaw != "" {
			enc, encErr := h.encryptString(passwordRaw)
			if encErr != nil {
				respondError(c, http.StatusInternalServerError, "PROMETHEUS_AUTH", "failed to encrypt password")
				return
			}
			passwordEnc = enc
		} else if normalizePrometheusAuthType(current.AuthType) == "basic" && strings.TrimSpace(current.PasswordEnc) != "" {
			passwordEnc = current.PasswordEnc
		} else {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_AUTH", "password is required for basic auth")
			return
		}
	case "bearer":
		tokenRaw := strings.TrimSpace(req.BearerToken)
		if tokenRaw != "" {
			enc, encErr := h.encryptString(tokenRaw)
			if encErr != nil {
				respondError(c, http.StatusInternalServerError, "PROMETHEUS_AUTH", "failed to encrypt bearer token")
				return
			}
			bearerTokenEnc = enc
		} else if normalizePrometheusAuthType(current.AuthType) == "bearer" && strings.TrimSpace(current.BearerTokenEnc) != "" {
			bearerTokenEnc = current.BearerTokenEnc
		} else {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_AUTH", "bearer token is required for bearer auth")
			return
		}
	}

	row := db.PrometheusSettings{
		OrgID:                 &orgID,
		Enabled:               req.Enabled,
		BaseURL:               strings.TrimRight(baseURL, "/"),
		AuthType:              authType,
		Username:              username,
		PasswordEnc:           passwordEnc,
		BearerTokenEnc:        bearerTokenEnc,
		TLSInsecureSkipVerify: req.TLSInsecureSkipVerify,
		TimeoutMS:             timeoutMS,
		DefaultStepSec:        defaultStepSec,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	tx := h.DB.WithContext(c.Request.Context()).Begin()
	var existing db.PrometheusSettings
	err = tx.Where("org_id = ?", orgID).Order("created_at desc").First(&existing).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"org_id":                   row.OrgID,
			"enabled":                  row.Enabled,
			"base_url":                 row.BaseURL,
			"auth_type":                row.AuthType,
			"username":                 row.Username,
			"password_enc":             row.PasswordEnc,
			"bearer_token_enc":         row.BearerTokenEnc,
			"tls_insecure_skip_verify": row.TLSInsecureSkipVerify,
			"timeout_ms":               row.TimeoutMS,
			"default_step_sec":         row.DefaultStepSec,
			"updated_at":               time.Now(),
		}
		if err := tx.Model(&db.PrometheusSettings{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			_ = tx.Rollback()
			respondError(c, http.StatusInternalServerError, "PROMETHEUS_SAVE", "failed to save settings")
			return
		}
		row = existing
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := tx.Create(&row).Error; err != nil {
			_ = tx.Rollback()
			respondError(c, http.StatusInternalServerError, "PROMETHEUS_SAVE", "failed to save settings")
			return
		}
	default:
		_ = tx.Rollback()
		respondError(c, http.StatusInternalServerError, "PROMETHEUS_SAVE", "failed to save settings")
		return
	}
	if row.ID != uuid.Nil {
		_ = tx.Where("id <> ? AND org_id = ?", row.ID, orgID).Delete(&db.PrometheusSettings{}).Error
	}
	if err := tx.Commit().Error; err != nil {
		respondError(c, http.StatusInternalServerError, "PROMETHEUS_SAVE", "failed to save settings")
		return
	}
	h.auditEvent(c, nil, "PROMETHEUS_SETTINGS_UPDATE", "ok", nil, gin.H{
		"enabled":                  req.Enabled,
		"base_url":                 strings.TrimRight(baseURL, "/"),
		"auth_type":                authType,
		"tls_insecure_skip_verify": req.TLSInsecureSkipVerify,
		"timeout_ms":               timeoutMS,
		"default_step_sec":         defaultStepSec,
	}, nil)
	respondStatus(c, http.StatusOK, prometheusSettingsResponse{
		Enabled:               req.Enabled,
		BaseURL:               strings.TrimRight(baseURL, "/"),
		AuthType:              authType,
		Username:              username,
		PasswordSet:           strings.TrimSpace(passwordEnc) != "",
		BearerTokenSet:        strings.TrimSpace(bearerTokenEnc) != "",
		TLSInsecureSkipVerify: req.TLSInsecureSkipVerify,
		TimeoutMS:             timeoutMS,
		DefaultStepSec:        defaultStepSec,
	})
}

func (h *Handler) TestPrometheusConnection(c *gin.Context) {
	var req prometheusTestRequest
	if !parseJSONBody(c, &req) {
		return
	}
	orgID, err := h.resolveOrgFromRequest(c, true)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	row, err := h.getPrometheusSettings(c, orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_SETTINGS", "prometheus settings are not configured")
		return
	}
	cfg, err := h.prometheusRuntimeConfigFromRow(row)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROMETHEUS_SETTINGS", "failed to decode prometheus credentials")
		return
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_URL", "base_url is empty")
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = "up"
	}
	started := time.Now()
	resp, err := doPrometheusInstantQuery(c.Request.Context(), cfg, query, time.Now())
	if err != nil {
		respondError(c, http.StatusBadGateway, "PROMETHEUS_TEST", err.Error())
		return
	}
	total, up := summarizePrometheusUp(resp.Data.ResultType, resp.Data.Result)
	down := total - up
	if down < 0 {
		down = 0
	}
	respondStatus(c, http.StatusOK, gin.H{
		"ok":          true,
		"query":       query,
		"status":      resp.Status,
		"result_type": resp.Data.ResultType,
		"samples":     total,
		"up":          up,
		"down":        down,
		"warnings":    resp.Warnings,
		"took_ms":     time.Since(started).Milliseconds(),
	})
}

func (h *Handler) QueryPrometheus(c *gin.Context) {
	var req prometheusQueryRequest
	if !parseJSONBody(c, &req) {
		return
	}
	orgID, err := h.resolveOrgFromRequest(c, false)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	row, err := h.getPrometheusSettings(c, orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_SETTINGS", "prometheus settings are not configured")
		return
	}
	cfg, err := h.prometheusRuntimeConfigFromRow(row)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "PROMETHEUS_SETTINGS", "failed to decode prometheus credentials")
		return
	}
	if !cfg.Enabled {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_DISABLED", "prometheus integration is disabled")
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_QUERY", "query is required")
		return
	}
	useInstant := true
	if req.Instant != nil {
		useInstant = *req.Instant
	}
	timeout := cfg.Timeout
	if req.TimeoutS > 0 {
		timeout = time.Duration(req.TimeoutS) * time.Second
		if timeout < time.Second {
			timeout = time.Second
		}
		if timeout > 2*time.Minute {
			timeout = 2 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	started := time.Now()
	if useInstant {
		at := time.Now()
		if strings.TrimSpace(req.Time) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Time))
			if err != nil {
				respondError(c, http.StatusBadRequest, "PROMETHEUS_QUERY", "time must be RFC3339")
				return
			}
			at = parsed
		}
		resp, err := doPrometheusInstantQuery(ctx, cfg, query, at)
		if err != nil {
			respondError(c, http.StatusBadGateway, "PROMETHEUS_QUERY", err.Error())
			return
		}
		decoded := decodePrometheusResult(resp.Data.Result)
		respondStatus(c, http.StatusOK, gin.H{
			"ok":          true,
			"query":       query,
			"instant":     true,
			"time":        at.UTC().Format(time.RFC3339),
			"status":      resp.Status,
			"result_type": resp.Data.ResultType,
			"result":      decoded,
			"warnings":    resp.Warnings,
			"took_ms":     time.Since(started).Milliseconds(),
		})
		return
	}

	rangeEnd := time.Now()
	if strings.TrimSpace(req.End) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.End))
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_QUERY", "end must be RFC3339")
			return
		}
		rangeEnd = parsed
	}
	rangeStart := rangeEnd.Add(-defaultPrometheusRangeMinutes * time.Minute)
	if strings.TrimSpace(req.Start) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Start))
		if err != nil {
			respondError(c, http.StatusBadRequest, "PROMETHEUS_QUERY", "start must be RFC3339")
			return
		}
		rangeStart = parsed
	}
	if !rangeEnd.After(rangeStart) {
		respondError(c, http.StatusBadRequest, "PROMETHEUS_QUERY", "end must be greater than start")
		return
	}
	stepSec := sanitizePrometheusStepSec(req.StepSec, cfg.DefaultStepSec)
	resp, err := doPrometheusRangeQuery(ctx, cfg, query, rangeStart, rangeEnd, stepSec)
	if err != nil {
		respondError(c, http.StatusBadGateway, "PROMETHEUS_QUERY", err.Error())
		return
	}
	decoded := decodePrometheusResult(resp.Data.Result)
	respondStatus(c, http.StatusOK, gin.H{
		"ok":          true,
		"query":       query,
		"instant":     false,
		"start":       rangeStart.UTC().Format(time.RFC3339),
		"end":         rangeEnd.UTC().Format(time.RFC3339),
		"step_sec":    stepSec,
		"status":      resp.Status,
		"result_type": resp.Data.ResultType,
		"result":      decoded,
		"warnings":    resp.Warnings,
		"took_ms":     time.Since(started).Milliseconds(),
	})
}

func normalizePrometheusAuthType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "basic", "bearer":
		return normalized
	default:
		return "none"
	}
}

func sanitizePrometheusTimeoutMS(value int) int {
	if value < minPrometheusTimeoutMS {
		return defaultPrometheusTimeoutMS
	}
	if value > maxPrometheusTimeoutMS {
		return maxPrometheusTimeoutMS
	}
	return value
}

func sanitizePrometheusStepSec(value int, fallback int) int {
	if fallback < minPrometheusStepSec || fallback > maxPrometheusStepSec {
		fallback = defaultPrometheusStepSec
	}
	if value <= 0 {
		return fallback
	}
	if value < minPrometheusStepSec {
		return minPrometheusStepSec
	}
	if value > maxPrometheusStepSec {
		return maxPrometheusStepSec
	}
	return value
}

func validatePrometheusBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid base_url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("base_url must start with http:// or https://")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("base_url host is empty")
	}
	return parsed, nil
}

func (h *Handler) encryptString(value string) (string, error) {
	if h == nil || h.Encryptor == nil {
		return "", errors.New("encryptor not configured")
	}
	return h.Encryptor.EncryptString(value)
}

func (h *Handler) decryptString(value string) (string, error) {
	if h == nil || h.Encryptor == nil {
		return "", errors.New("encryptor not configured")
	}
	return h.Encryptor.DecryptString(value)
}

func (h *Handler) getPrometheusSettings(c *gin.Context, orgID uuid.UUID) (db.PrometheusSettings, error) {
	var row db.PrometheusSettings
	err := h.DB.WithContext(c.Request.Context()).
		Where("org_id = ?", orgID).
		Order("created_at desc").
		First(&row).Error
	if err != nil {
		return db.PrometheusSettings{}, err
	}
	return row, nil
}

func (h *Handler) prometheusRuntimeConfigFromRow(row db.PrometheusSettings) (prometheusRuntimeConfig, error) {
	cfg := prometheusRuntimeConfig{
		Enabled:               row.Enabled,
		BaseURL:               strings.TrimRight(strings.TrimSpace(row.BaseURL), "/"),
		AuthType:              normalizePrometheusAuthType(row.AuthType),
		Username:              strings.TrimSpace(row.Username),
		TLSInsecureSkipVerify: row.TLSInsecureSkipVerify,
		Timeout:               time.Duration(sanitizePrometheusTimeoutMS(row.TimeoutMS)) * time.Millisecond,
		DefaultStepSec:        sanitizePrometheusStepSec(row.DefaultStepSec, defaultPrometheusStepSec),
	}
	if cfg.BaseURL != "" {
		if _, err := validatePrometheusBaseURL(cfg.BaseURL); err != nil {
			return prometheusRuntimeConfig{}, err
		}
	}
	if cfg.AuthType == "basic" && strings.TrimSpace(row.PasswordEnc) != "" {
		password, err := h.decryptString(row.PasswordEnc)
		if err != nil {
			return prometheusRuntimeConfig{}, err
		}
		cfg.Password = strings.TrimSpace(password)
	}
	if cfg.AuthType == "bearer" && strings.TrimSpace(row.BearerTokenEnc) != "" {
		token, err := h.decryptString(row.BearerTokenEnc)
		if err != nil {
			return prometheusRuntimeConfig{}, err
		}
		cfg.BearerToken = strings.TrimSpace(token)
	}
	return cfg, nil
}

func doPrometheusInstantQuery(ctx context.Context, cfg prometheusRuntimeConfig, query string, at time.Time) (*prometheusAPIResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("time", strconv.FormatFloat(float64(at.UnixNano())/1e9, 'f', -1, 64))
	return doPrometheusRequest(ctx, cfg, "/api/v1/query", params)
}

func doPrometheusRangeQuery(ctx context.Context, cfg prometheusRuntimeConfig, query string, start, end time.Time, stepSec int) (*prometheusAPIResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatFloat(float64(start.UnixNano())/1e9, 'f', -1, 64))
	params.Set("end", strconv.FormatFloat(float64(end.UnixNano())/1e9, 'f', -1, 64))
	params.Set("step", strconv.Itoa(stepSec))
	return doPrometheusRequest(ctx, cfg, "/api/v1/query_range", params)
}

func doPrometheusRequest(ctx context.Context, cfg prometheusRuntimeConfig, path string, params url.Values) (*prometheusAPIResponse, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base_url is empty")
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + path
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: insecureTLS(!cfg.TLSInsecureSkipVerify),
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	switch cfg.AuthType {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if strings.TrimSpace(cfg.BearerToken) != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(raw))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("prometheus http %d: %s", resp.StatusCode, message)
	}
	var payload prometheusAPIResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(payload.Status)) != "success" {
		reason := strings.TrimSpace(payload.Error)
		if reason == "" {
			reason = "unknown error"
		}
		return nil, fmt.Errorf("prometheus %s: %s", strings.TrimSpace(payload.ErrorType), reason)
	}
	return &payload, nil
}

func decodePrometheusResult(raw json.RawMessage) any {
	if len(raw) == 0 {
		return []any{}
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return []any{}
	}
	return out
}

func summarizePrometheusUp(resultType string, raw json.RawMessage) (int, int) {
	if strings.TrimSpace(resultType) != "vector" {
		return 0, 0
	}
	type vectorSample struct {
		Value []any `json:"value"`
	}
	var samples []vectorSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		return 0, 0
	}
	total := len(samples)
	up := 0
	for _, sample := range samples {
		if len(sample.Value) < 2 {
			continue
		}
		v := parsePrometheusNumeric(sample.Value[1])
		if v > 0 {
			up++
		}
	}
	return total, up
}

func parsePrometheusNumeric(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		out, _ := v.Float64()
		return out
	case string:
		out, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return out
	default:
		return 0
	}
}
