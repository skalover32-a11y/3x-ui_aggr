package httpapi

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "gorm.io/gorm"

    "agr_3x_ui/internal/db"
    "agr_3x_ui/internal/services/backup"
)

type backupStorageTargetRequest struct {
    Name               string `json:"name"`
    Type               string `json:"type"`
    Enabled            *bool  `json:"enabled"`
    Host               string `json:"host"`
    Port               int    `json:"port"`
    Username           string `json:"username"`
    Password           string `json:"password"`
    BasePath           string `json:"base_path"`
    TimeoutSec         int    `json:"timeout_sec"`
    PassiveMode        bool   `json:"passive_mode"`
    InsecureSkipVerify bool   `json:"insecure_skip_verify"`
    AuthMethod         string `json:"auth_method"`
    PrivateKeyPEM      string `json:"private_key_pem"`
    URL                string `json:"url"`
    Bucket             string `json:"bucket"`
    Region             string `json:"region"`
    AccessKey          string `json:"access_key"`
    SecretKey          string `json:"secret_key"`
    UseSSL             bool   `json:"use_ssl"`
    PathStyle          bool   `json:"path_style"`
    LocalPath          string `json:"local_path"`
}

type backupJobRequest struct {
    Name               string `json:"name"`
    Description        string `json:"description"`
    NodeID             string `json:"node_id"`
    Enabled            *bool  `json:"enabled"`
    Timezone           string `json:"timezone"`
    CronExpression     string `json:"cron_expression"`
    RetentionDays      int    `json:"retention_days"`
    StorageTargetID    string `json:"storage_target_id"`
    CompressionEnabled *bool  `json:"compression_enabled"`
    CompressionLevel   *int   `json:"compression_level"`
    UploadConcurrency  int    `json:"upload_concurrency"`
}

type backupSourceRequest struct {
    Name       string          `json:"name"`
    Type       string          `json:"type"`
    Enabled    *bool           `json:"enabled"`
    OrderIndex int             `json:"order_index"`
    Config     json.RawMessage `json:"config"`
}

type backupReorderRequest struct {
    SourceIDs []string `json:"source_ids"`
}

type backupTemplateApplyRequest struct {
    TemplateID string `json:"template_id"`
    backupJobRequest
}

func parseOrgID(c *gin.Context) (uuid.UUID, error) {
    return uuid.Parse(strings.TrimSpace(c.Param("orgId")))
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, error) {
    return uuid.Parse(strings.TrimSpace(c.Param(name)))
}

func (h *Handler) backupService() *backup.Service {
    return h.Backup
}

func (h *Handler) ListBackupStorageTargets(c *gin.Context) {
    svc := h.backupService()
    if svc == nil {
        respondError(c, http.StatusServiceUnavailable, "BACKUP_DISABLED", "backup service unavailable")
        return
    }
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    rows, err := svc.ListStorageTargets(c.Request.Context(), orgID)
    if err != nil {
        respondError(c, http.StatusInternalServerError, "BACKUP_TARGETS", err.Error())
        return
    }
    resp := make([]gin.H, 0, len(rows))
    for _, row := range rows {
        cfg, _ := svc.DecryptStorageTargetConfig(row)
        resp = append(resp, backupStorageTargetResponse(&row, cfg))
    }
    respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) GetBackupStorageTarget(c *gin.Context) {
    svc := h.backupService()
    orgID, targetID, ok := h.parseBackupIDs(c, "targetId")
    if !ok {
        return
    }
    row, cfg, err := svc.GetStorageTarget(c.Request.Context(), orgID, targetID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, backupStorageTargetResponse(row, cfg))
}

func (h *Handler) CreateBackupStorageTarget(c *gin.Context) {
    svc := h.backupService()
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    var req backupStorageTargetRequest
    if !parseJSONBody(c, &req) {
        return
    }
    enabled := true
    if req.Enabled != nil {
        enabled = *req.Enabled
    }
    row, err := svc.CreateStorageTarget(c.Request.Context(), orgID, backup.StorageTargetInput{
        Name:    req.Name,
        Type:    req.Type,
        Enabled: enabled,
        Config: backup.StorageTargetConfig{
            Host:               req.Host,
            Port:               req.Port,
            Username:           req.Username,
            Password:           req.Password,
            BasePath:           req.BasePath,
            TimeoutSec:         req.TimeoutSec,
            PassiveMode:        req.PassiveMode,
            InsecureSkipVerify: req.InsecureSkipVerify,
            AuthMethod:         req.AuthMethod,
            PrivateKeyPEM:      req.PrivateKeyPEM,
            URL:                req.URL,
            Bucket:             req.Bucket,
            Region:             req.Region,
            AccessKey:          req.AccessKey,
            SecretKey:          req.SecretKey,
            UseSSL:             req.UseSSL,
            PathStyle:          req.PathStyle,
            LocalPath:          req.LocalPath,
        },
    })
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    cfg, _ := svc.DecryptStorageTargetConfig(*row)
    respondStatus(c, http.StatusCreated, backupStorageTargetResponse(row, cfg))
}

func (h *Handler) UpdateBackupStorageTarget(c *gin.Context) {
    svc := h.backupService()
    orgID, targetID, ok := h.parseBackupIDs(c, "targetId")
    if !ok {
        return
    }
    existing, _, err := svc.GetStorageTarget(c.Request.Context(), orgID, targetID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    var req backupStorageTargetRequest
    if !parseJSONBody(c, &req) {
        return
    }
    enabled := existing.Enabled
    if req.Enabled != nil {
        enabled = *req.Enabled
    }
    row, err := svc.UpdateStorageTarget(c.Request.Context(), orgID, targetID, backup.StorageTargetInput{
        Name:    firstString(req.Name, existing.Name),
        Type:    firstString(req.Type, existing.Type),
        Enabled: enabled,
        Config: backup.StorageTargetConfig{
            Host:               req.Host,
            Port:               req.Port,
            Username:           req.Username,
            Password:           req.Password,
            BasePath:           req.BasePath,
            TimeoutSec:         req.TimeoutSec,
            PassiveMode:        req.PassiveMode,
            InsecureSkipVerify: req.InsecureSkipVerify,
            AuthMethod:         req.AuthMethod,
            PrivateKeyPEM:      req.PrivateKeyPEM,
            URL:                req.URL,
            Bucket:             req.Bucket,
            Region:             req.Region,
            AccessKey:          req.AccessKey,
            SecretKey:          req.SecretKey,
            UseSSL:             req.UseSSL,
            PathStyle:          req.PathStyle,
            LocalPath:          req.LocalPath,
        },
    })
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    cfg, _ := svc.DecryptStorageTargetConfig(*row)
    respondStatus(c, http.StatusOK, backupStorageTargetResponse(row, cfg))
}

func (h *Handler) DeleteBackupStorageTarget(c *gin.Context) {
    svc := h.backupService()
    orgID, targetID, ok := h.parseBackupIDs(c, "targetId")
    if !ok {
        return
    }
    if err := svc.DeleteStorageTarget(c.Request.Context(), orgID, targetID); err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) TestBackupStorageTarget(c *gin.Context) {
    svc := h.backupService()
    orgID, targetID, ok := h.parseBackupIDs(c, "targetId")
    if !ok {
        return
    }
    row, err := svc.TestStorageTarget(c.Request.Context(), orgID, targetID)
    if err != nil && row == nil {
        h.respondBackupErr(c, err)
        return
    }
    cfg := backup.StorageTargetConfig{}
    if row != nil {
        cfg, _ = svc.DecryptStorageTargetConfig(*row)
    }
    payload := backupStorageTargetResponse(row, cfg)
    if err != nil {
        payload["test_error"] = err.Error()
    }
    respondStatus(c, http.StatusOK, payload)
}

func (h *Handler) ListBackupJobs(c *gin.Context) {
    svc := h.backupService()
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    rows, err := svc.ListJobs(c.Request.Context(), orgID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    jobIDs := make([]uuid.UUID, 0, len(rows))
    for _, row := range rows {
        jobIDs = append(jobIDs, row.ID)
    }
    sourceCounts, err := svc.CountSourcesByJobIDs(c.Request.Context(), jobIDs)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    resp := make([]gin.H, 0, len(rows))
    for _, row := range rows {
        payload := backupJobResponse(&row)
        payload["sources_count"] = sourceCounts[row.ID]
        resp = append(resp, payload)
    }
    respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) GetBackupJob(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    job, sources, err := svc.GetJob(c.Request.Context(), orgID, jobID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    plan, _ := svc.BuildExecutionPlan(c.Request.Context(), orgID, jobID)
    respondStatus(c, http.StatusOK, gin.H{"job": backupJobResponse(job), "sources": backupSourcesResponse(sources), "plan": plan})
}

func (h *Handler) CreateBackupJob(c *gin.Context) {
    svc := h.backupService()
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    var req backupJobRequest
    if !parseJSONBody(c, &req) {
        return
    }
    input, err := h.parseBackupJobInput(req)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    row, err := svc.CreateJob(c.Request.Context(), orgID, input)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusCreated, backupJobResponse(row))
}

func (h *Handler) UpdateBackupJob(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    var req backupJobRequest
    if !parseJSONBody(c, &req) {
        return
    }
    input, err := h.parseBackupJobInput(req)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    row, err := svc.UpdateJob(c.Request.Context(), orgID, jobID, input)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, backupJobResponse(row))
}

func (h *Handler) DeleteBackupJob(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    if err := svc.DeleteJob(c.Request.Context(), orgID, jobID); err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) RunBackupJob(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    var initiatedBy *uuid.UUID
    if user, err := h.actorUser(c); err == nil {
        initiatedBy = &user.ID
    }
    run, err := svc.TriggerRun(c.Request.Context(), orgID, jobID, backup.TriggerManual, initiatedBy)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusAccepted, backupRunResponse(run))
}

func (h *Handler) EnableBackupJob(c *gin.Context) {
    h.setBackupJobEnabled(c, true)
}

func (h *Handler) DisableBackupJob(c *gin.Context) {
    h.setBackupJobEnabled(c, false)
}

func (h *Handler) CreateBackupSource(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    var req backupSourceRequest
    if !parseJSONBody(c, &req) {
        return
    }
    enabled := true
    if req.Enabled != nil {
        enabled = *req.Enabled
    }
    row, err := svc.AddSource(c.Request.Context(), orgID, jobID, backup.SourceInput{Name: req.Name, Type: req.Type, Enabled: enabled, OrderIndex: req.OrderIndex, ConfigRaw: req.Config})
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusCreated, backupSourceResponse(row))
}

func (h *Handler) UpdateBackupSource(c *gin.Context) {
    svc := h.backupService()
    orgID, sourceID, ok := h.parseBackupIDs(c, "sourceId")
    if !ok {
        return
    }
    var req backupSourceRequest
    if !parseJSONBody(c, &req) {
        return
    }
    enabled := true
    if req.Enabled != nil {
        enabled = *req.Enabled
    }
    row, err := svc.UpdateSource(c.Request.Context(), orgID, sourceID, backup.SourceInput{Name: req.Name, Type: req.Type, Enabled: enabled, OrderIndex: req.OrderIndex, ConfigRaw: req.Config})
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, backupSourceResponse(row))
}

func (h *Handler) DeleteBackupSource(c *gin.Context) {
    svc := h.backupService()
    orgID, sourceID, ok := h.parseBackupIDs(c, "sourceId")
    if !ok {
        return
    }
    if err := svc.DeleteSource(c.Request.Context(), orgID, sourceID); err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) ReorderBackupSources(c *gin.Context) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    var req backupReorderRequest
    if !parseJSONBody(c, &req) {
        return
    }
    ids := make([]uuid.UUID, 0, len(req.SourceIDs))
    for _, raw := range req.SourceIDs {
        id, err := uuid.Parse(strings.TrimSpace(raw))
        if err != nil {
            respondError(c, http.StatusBadRequest, "INVALID_SOURCE", "invalid source id")
            return
        }
        ids = append(ids, id)
    }
    if err := svc.ReorderSources(c.Request.Context(), orgID, jobID, ids); err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) ListBackupRuns(c *gin.Context) {
    svc := h.backupService()
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    var jobID *uuid.UUID
    if raw := strings.TrimSpace(c.Query("job_id")); raw != "" {
        parsed, parseErr := uuid.Parse(raw)
        if parseErr != nil {
            respondError(c, http.StatusBadRequest, "INVALID_JOB", "invalid job id")
            return
        }
        jobID = &parsed
    }
    limit, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "50")))
    offset, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("offset", "0")))
    rows, err := svc.ListRuns(c.Request.Context(), orgID, backup.RunListOptions{JobID: jobID, Status: c.Query("status"), Limit: limit, Offset: offset, Desc: true})
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    resp := make([]gin.H, 0, len(rows))
    for _, row := range rows {
        resp = append(resp, backupRunResponse(&row))
    }
    respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) ListBackupJobRuns(c *gin.Context) {
    query := c.Request.URL.Query()
    query.Set("job_id", c.Param("jobId"))
    c.Request.URL.RawQuery = query.Encode()
    h.ListBackupRuns(c)
}

func (h *Handler) GetBackupRun(c *gin.Context) {
    svc := h.backupService()
    orgID, runID, ok := h.parseBackupIDs(c, "runId")
    if !ok {
        return
    }
    run, items, err := svc.GetRun(c.Request.Context(), orgID, runID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"run": backupRunResponse(run), "items": backupRunItemsResponse(items)})
}

func (h *Handler) GetBackupRunLog(c *gin.Context) {
    svc := h.backupService()
    orgID, runID, ok := h.parseBackupIDs(c, "runId")
    if !ok {
        return
    }
    content, err := svc.GetRunLog(c.Request.Context(), orgID, runID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"content": content})
}

func (h *Handler) RetryBackupRun(c *gin.Context) {
    svc := h.backupService()
    orgID, runID, ok := h.parseBackupIDs(c, "runId")
    if !ok {
        return
    }
    var initiatedBy *uuid.UUID
    if user, err := h.actorUser(c); err == nil {
        initiatedBy = &user.ID
    }
    run, err := svc.RetryRun(c.Request.Context(), orgID, runID, initiatedBy)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusAccepted, backupRunResponse(run))
}

func (h *Handler) CancelBackupRun(c *gin.Context) {
    svc := h.backupService()
    orgID, runID, ok := h.parseBackupIDs(c, "runId")
    if !ok {
        return
    }
    if err := svc.CancelRun(c.Request.Context(), orgID, runID); err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) ListBackupTemplates(c *gin.Context) {
    svc := h.backupService()
    rows, err := svc.ListTemplates(c.Request.Context())
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    resp := make([]gin.H, 0, len(rows))
    for _, row := range rows {
        resp = append(resp, gin.H{"id": row.ID.String(), "slug": row.Slug, "name": row.Name, "description": row.Description, "definition": json.RawMessage(row.DefinitionJSON)})
    }
    respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateBackupJobFromTemplate(c *gin.Context) {
    svc := h.backupService()
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    var req backupTemplateApplyRequest
    if !parseJSONBody(c, &req) {
        return
    }
    templateID, err := uuid.Parse(strings.TrimSpace(req.TemplateID))
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_TEMPLATE", "invalid template id")
        return
    }
    input, err := h.parseBackupJobInput(req.backupJobRequest)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    job, sources, err := svc.CreateJobFromTemplate(c.Request.Context(), orgID, templateID, input)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusCreated, gin.H{"job": backupJobResponse(job), "sources": backupSourcesResponse(sources)})
}

func (h *Handler) BackupCatalogDockerContainers(c *gin.Context) {
    h.respondBackupCatalog(c, func(ctx context.Context, orgID, nodeID uuid.UUID) ([]backup.CatalogEntry, error) {
        return h.Backup.ListDockerContainers(ctx, orgID, nodeID)
    })
}

func (h *Handler) BackupCatalogDockerVolumes(c *gin.Context) {
    h.respondBackupCatalog(c, func(ctx context.Context, orgID, nodeID uuid.UUID) ([]backup.CatalogEntry, error) {
        return h.Backup.ListDockerVolumes(ctx, orgID, nodeID)
    })
}

func (h *Handler) BackupCatalogCommonPaths(c *gin.Context) {
    respondStatus(c, http.StatusOK, h.Backup.CommonPaths())
}

func (h *Handler) BackupCatalogSystemDetected(c *gin.Context) {
    h.respondBackupCatalog(c, func(ctx context.Context, orgID, nodeID uuid.UUID) ([]backup.CatalogEntry, error) {
        return h.Backup.DetectSystemSources(ctx, orgID, nodeID)
    })
}

func (h *Handler) BackupCatalogPostgresContainers(c *gin.Context) {
    h.respondBackupCatalog(c, func(ctx context.Context, orgID, nodeID uuid.UUID) ([]backup.CatalogEntry, error) {
        return h.Backup.ListPostgresContainers(ctx, orgID, nodeID)
    })
}

func (h *Handler) respondBackupCatalog(c *gin.Context, fn func(context.Context, uuid.UUID, uuid.UUID) ([]backup.CatalogEntry, error)) {
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return
    }
    nodeID, err := uuid.Parse(strings.TrimSpace(c.Query("node_id")))
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_NODE", "node_id is required")
        return
    }
    rows, err := fn(c.Request.Context(), orgID, nodeID)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, rows)
}

func (h *Handler) setBackupJobEnabled(c *gin.Context, enabled bool) {
    svc := h.backupService()
    orgID, jobID, ok := h.parseBackupIDs(c, "jobId")
    if !ok {
        return
    }
    row, err := svc.SetJobEnabled(c.Request.Context(), orgID, jobID, enabled)
    if err != nil {
        h.respondBackupErr(c, err)
        return
    }
    respondStatus(c, http.StatusOK, backupJobResponse(row))
}

func (h *Handler) parseBackupIDs(c *gin.Context, key string) (uuid.UUID, uuid.UUID, bool) {
    orgID, err := parseOrgID(c)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org id")
        return uuid.Nil, uuid.Nil, false
    }
    entityID, err := parseUUIDParam(c, key)
    if err != nil {
        respondError(c, http.StatusBadRequest, "INVALID_ID", "invalid id")
        return uuid.Nil, uuid.Nil, false
    }
    return orgID, entityID, true
}

func (h *Handler) parseBackupJobInput(req backupJobRequest) (backup.JobInput, error) {
    var nodeID *uuid.UUID
    if strings.TrimSpace(req.NodeID) != "" {
        parsed, err := uuid.Parse(strings.TrimSpace(req.NodeID))
        if err != nil {
            return backup.JobInput{}, errors.New("invalid node_id")
        }
        nodeID = &parsed
    }
    targetID, err := uuid.Parse(strings.TrimSpace(req.StorageTargetID))
    if err != nil {
        return backup.JobInput{}, errors.New("invalid storage_target_id")
    }
    enabled := true
    if req.Enabled != nil {
        enabled = *req.Enabled
    }
    compressionEnabled := true
    if req.CompressionEnabled != nil {
        compressionEnabled = *req.CompressionEnabled
    }
    return backup.JobInput{
        Name:               req.Name,
        Description:        req.Description,
        NodeID:             nodeID,
        Enabled:            enabled,
        Timezone:           req.Timezone,
        CronExpression:     req.CronExpression,
        RetentionDays:      req.RetentionDays,
        StorageTargetID:    targetID,
        CompressionEnabled: compressionEnabled,
        CompressionLevel:   req.CompressionLevel,
        UploadConcurrency:  req.UploadConcurrency,
    }, nil
}

func (h *Handler) respondBackupErr(c *gin.Context, err error) {
    if err == nil {
        return
    }
    switch {
    case errors.Is(err, gorm.ErrRecordNotFound):
        respondError(c, http.StatusNotFound, "NOT_FOUND", "resource not found")
    default:
        code := http.StatusBadRequest
        if strings.Contains(strings.ToLower(err.Error()), "db") {
            code = http.StatusInternalServerError
        }
        respondError(c, code, "BACKUP_ERROR", err.Error())
    }
}

func backupStorageTargetResponse(row *db.BackupStorageTarget, cfg backup.StorageTargetConfig) gin.H {
    if row == nil {
        return gin.H{}
    }
    return gin.H{
        "id":               row.ID.String(),
        "org_id":           row.OrgID.String(),
        "name":             row.Name,
        "type":             row.Type,
        "enabled":          row.Enabled,
        "last_tested_at":   row.LastTestedAt,
        "last_test_status": row.LastTestStatus,
        "last_test_error":  row.LastTestError,
        "config":           backup.MaskStorageConfig(cfg),
        "created_at":       row.CreatedAt,
        "updated_at":       row.UpdatedAt,
    }
}

func backupJobResponse(row *db.BackupJob) gin.H {
    if row == nil {
        return gin.H{}
    }
    payload := gin.H{
        "id":                  row.ID.String(),
        "org_id":              row.OrgID.String(),
        "name":                row.Name,
        "description":         row.Description,
        "enabled":             row.Enabled,
        "timezone":            row.Timezone,
        "cron_expression":     row.CronExpression,
        "retention_days":      row.RetentionDays,
        "storage_target_id":   row.StorageTargetID.String(),
        "compression_enabled": row.CompressionEnabled,
        "compression_level":   row.CompressionLevel,
        "upload_concurrency":  row.UploadConcurrency,
        "last_run_at":         row.LastRunAt,
        "last_success_at":     row.LastSuccessAt,
        "last_status":         row.LastStatus,
        "last_error":          row.LastError,
        "last_size_bytes":     row.LastSizeBytes,
        "created_at":          row.CreatedAt,
        "updated_at":          row.UpdatedAt,
    }
    if row.NodeID != nil {
        payload["node_id"] = row.NodeID.String()
    }
    return payload
}

func backupSourceResponse(row *db.BackupSource) gin.H {
    if row == nil {
        return gin.H{}
    }
    return gin.H{
        "id":          row.ID.String(),
        "job_id":      row.JobID.String(),
        "name":        row.Name,
        "type":        row.Type,
        "enabled":     row.Enabled,
        "order_index": row.OrderIndex,
        "config":      json.RawMessage(row.ConfigJSON),
        "created_at":  row.CreatedAt,
        "updated_at":  row.UpdatedAt,
    }
}

func backupSourcesResponse(rows []db.BackupSource) []gin.H {
    resp := make([]gin.H, 0, len(rows))
    for i := range rows {
        resp = append(resp, backupSourceResponse(&rows[i]))
    }
    return resp
}

func backupRunResponse(row *db.BackupRun) gin.H {
    if row == nil {
        return gin.H{}
    }
    payload := gin.H{
        "id":                  row.ID.String(),
        "org_id":              row.OrgID.String(),
        "job_id":              row.JobID.String(),
        "status":              row.Status,
        "trigger_type":        row.TriggerType,
        "local_workdir":       row.LocalWorkdir,
        "remote_workdir":      row.RemoteWorkdir,
        "remote_path":         row.RemotePath,
        "total_size_bytes":    row.TotalSizeBytes,
        "uploaded_size_bytes": row.UploadedSizeBytes,
        "file_count":          row.FileCount,
        "checksum_status":     row.ChecksumStatus,
        "cleanup_status":      row.CleanupStatus,
        "error_summary":       row.ErrorSummary,
        "exit_code":           row.ExitCode,
        "log_excerpt":         row.LogExcerpt,
        "started_at":          row.StartedAt,
        "finished_at":         row.FinishedAt,
        "duration_ms":         row.DurationMS,
        "cancel_requested_at": row.CancelRequestedAt,
        "created_at":          row.CreatedAt,
    }
    if row.InitiatedByUserID != nil {
        payload["initiated_by_user_id"] = row.InitiatedByUserID.String()
    }
    return payload
}

func backupRunItemsResponse(rows []db.BackupRunItem) []gin.H {
    resp := make([]gin.H, 0, len(rows))
    for i := range rows {
        row := rows[i]
        payload := gin.H{
            "id":               row.ID.String(),
            "run_id":           row.RunID.String(),
            "item_type":        row.ItemType,
            "logical_name":     row.LogicalName,
            "output_file_name": row.OutputFileName,
            "remote_source_path": row.RemoteSourcePath,
            "size_bytes":       row.SizeBytes,
            "checksum":         row.Checksum,
            "status":           row.Status,
            "started_at":       row.StartedAt,
            "finished_at":      row.FinishedAt,
            "error_text":       row.ErrorText,
            "extra":            json.RawMessage(row.ExtraJSON),
        }
        if row.SourceID != nil {
            payload["source_id"] = row.SourceID.String()
        }
        resp = append(resp, payload)
    }
    return resp
}

func firstString(value string, fallback string) string {
    if strings.TrimSpace(value) != "" {
        return strings.TrimSpace(value)
    }
    return fallback
}
