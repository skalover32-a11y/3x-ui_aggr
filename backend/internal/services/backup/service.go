package backup

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
    "gorm.io/datatypes"
    "gorm.io/gorm"

    "agr_3x_ui/internal/db"
    "agr_3x_ui/internal/security"
    "agr_3x_ui/internal/services/backup/storage"
)

type Service struct {
    DB                  *gorm.DB
    Encryptor           *security.Encryptor
    DataDir             string
    SudoPasswords       []string
    AllowUnsafeCommands bool
    MaxConcurrentRuns   int

    stop     chan struct{}
    stopOnce sync.Once
    running  sync.Map
}

type StorageTargetInput struct {
    Name    string
    Type    string
    Enabled bool
    Config  StorageTargetConfig
}

type SourceInput struct {
    Name               string
    Type               string
    Enabled            bool
    OrderIndex         int
    ConfigRaw          []byte
    AllowUnsafeCommand bool
}

type RunListOptions struct {
    JobID    *uuid.UUID
    Status   string
    Limit    int
    Offset   int
    Desc     bool
}

type CatalogOptions struct {
    NodeID uuid.UUID
}

func New(dbConn *gorm.DB, enc *security.Encryptor, dataDir string, sudoPasswords []string) *Service {
    if strings.TrimSpace(dataDir) == "" {
        dataDir = "./data"
    }
    svc := &Service{
        DB:                dbConn,
        Encryptor:         enc,
        DataDir:           dataDir,
        SudoPasswords:     sudoPasswords,
        MaxConcurrentRuns: 2,
        stop:              make(chan struct{}),
    }
    if svc.DB != nil {
        _ = EnsureBuiltInTemplates(context.Background(), svc.DB)
        svc.refreshJobEnabledMetric(context.Background())
    }
    return svc
}

func (s *Service) Start(ctx context.Context) {
    if s == nil || s.DB == nil {
        return
    }
    go s.schedulerLoop(ctx)
}

func (s *Service) Stop() {
    if s == nil {
        return
    }
    s.stopOnce.Do(func() {
        close(s.stop)
    })
}

func (s *Service) ListStorageTargets(ctx context.Context, orgID uuid.UUID) ([]db.BackupStorageTarget, error) {
    var rows []db.BackupStorageTarget
    err := s.DB.WithContext(ctx).
        Where("org_id = ?", orgID).
        Order("created_at DESC").
        Find(&rows).Error
    return rows, err
}

func (s *Service) GetStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (*db.BackupStorageTarget, StorageTargetConfig, error) {
    var row db.BackupStorageTarget
    if err := s.DB.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, targetID).First(&row).Error; err != nil {
        return nil, StorageTargetConfig{}, err
    }
    cfg, err := s.decryptStorageConfig(row.ConfigEnc)
    if err != nil {
        return nil, StorageTargetConfig{}, err
    }
    return &row, cfg, nil
}

func (s *Service) CreateStorageTarget(ctx context.Context, orgID uuid.UUID, input StorageTargetInput) (*db.BackupStorageTarget, error) {
    if s == nil || s.DB == nil || s.Encryptor == nil {
        return nil, errors.New("backup service not configured")
    }
    input.Name = strings.TrimSpace(input.Name)
    input.Type = strings.TrimSpace(strings.ToLower(input.Type))
    if input.Name == "" {
        return nil, errors.New("name is required")
    }
    cfg, err := ValidateStorageConfig(input.Type, input.Config)
    if err != nil {
        return nil, err
    }
    enc, err := s.encryptStorageConfig(cfg)
    if err != nil {
        return nil, err
    }
    row := &db.BackupStorageTarget{
        ID:        uuid.New(),
        OrgID:     orgID,
        Name:      input.Name,
        Type:      input.Type,
        ConfigEnc: enc,
        Enabled:   input.Enabled,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    if err := s.DB.WithContext(ctx).Create(row).Error; err != nil {
        return nil, err
    }
    return row, nil
}

func (s *Service) UpdateStorageTarget(ctx context.Context, orgID, targetID uuid.UUID, input StorageTargetInput) (*db.BackupStorageTarget, error) {
    row, existingCfg, err := s.GetStorageTarget(ctx, orgID, targetID)
    if err != nil {
        return nil, err
    }
    if trimmed := strings.TrimSpace(input.Name); trimmed != "" {
        row.Name = trimmed
    }
    if trimmed := strings.TrimSpace(strings.ToLower(input.Type)); trimmed != "" {
        row.Type = trimmed
    }
    merged := mergeStorageConfig(existingCfg, input.Config)
    merged, err = ValidateStorageConfig(row.Type, merged)
    if err != nil {
        return nil, err
    }
    enc, err := s.encryptStorageConfig(merged)
    if err != nil {
        return nil, err
    }
    row.ConfigEnc = enc
    row.Enabled = input.Enabled
    row.UpdatedAt = time.Now()
    if err := s.DB.WithContext(ctx).Save(row).Error; err != nil {
        return nil, err
    }
    return row, nil
}

func (s *Service) DeleteStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) error {
    var jobCount int64
    if err := s.DB.WithContext(ctx).Model(&db.BackupJob{}).Where("org_id = ? AND storage_target_id = ?", orgID, targetID).Count(&jobCount).Error; err != nil {
        return err
    }
    if jobCount > 0 {
        return errors.New("storage target is used by backup jobs")
    }
    return s.DB.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, targetID).Delete(&db.BackupStorageTarget{}).Error
}

func (s *Service) ListJobs(ctx context.Context, orgID uuid.UUID) ([]db.BackupJob, error) {
    var rows []db.BackupJob
    err := s.DB.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at DESC").Find(&rows).Error
    return rows, err
}

func (s *Service) CountSourcesByJobIDs(ctx context.Context, jobIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
    out := make(map[uuid.UUID]int64, len(jobIDs))
    if len(jobIDs) == 0 {
        return out, nil
    }
    type row struct {
        JobID uuid.UUID
        Count int64
    }
    var rows []row
    if err := s.DB.WithContext(ctx).
        Model(&db.BackupSource{}).
        Select("job_id, count(*) as count").
        Where("job_id IN ?", jobIDs).
        Group("job_id").
        Scan(&rows).Error; err != nil {
        return nil, err
    }
    for _, row := range rows {
        out[row.JobID] = row.Count
    }
    return out, nil
}

func (s *Service) GetJob(ctx context.Context, orgID, jobID uuid.UUID) (*db.BackupJob, []db.BackupSource, error) {
    var job db.BackupJob
    if err := s.DB.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, jobID).First(&job).Error; err != nil {
        return nil, nil, err
    }
    var sources []db.BackupSource
    if err := s.DB.WithContext(ctx).Where("job_id = ?", jobID).Order("order_index, created_at").Find(&sources).Error; err != nil {
        return nil, nil, err
    }
    return &job, sources, nil
}

func (s *Service) CreateJob(ctx context.Context, orgID uuid.UUID, input JobInput) (*db.BackupJob, error) {
    validated, err := ValidateJobInput(input)
    if err != nil {
        return nil, err
    }
    target, _, err := s.GetStorageTarget(ctx, orgID, validated.StorageTargetID)
    if err != nil {
        return nil, fmt.Errorf("storage target: %w", err)
    }
    if !target.Enabled {
        return nil, errors.New("storage target is disabled")
    }
    row := &db.BackupJob{
        ID:                 uuid.New(),
        OrgID:              orgID,
        NodeID:             validated.NodeID,
        Name:               validated.Name,
        Description:        validated.Description,
        Enabled:            validated.Enabled,
        Timezone:           validated.Timezone,
        CronExpression:     validated.CronExpression,
        RetentionDays:      validated.RetentionDays,
        StorageTargetID:    validated.StorageTargetID,
        CompressionEnabled: validated.CompressionEnabled,
        CompressionLevel:   validated.CompressionLevel,
        UploadConcurrency:  validated.UploadConcurrency,
        LastStatus:         StatusIdle,
        CreatedAt:          time.Now(),
        UpdatedAt:          time.Now(),
    }
    if err := s.DB.WithContext(ctx).Create(row).Error; err != nil {
        return nil, err
    }
    s.refreshJobEnabledMetric(ctx)
    return row, nil
}

func (s *Service) UpdateJob(ctx context.Context, orgID, jobID uuid.UUID, input JobInput) (*db.BackupJob, error) {
    validated, err := ValidateJobInput(input)
    if err != nil {
        return nil, err
    }
    job, _, err := s.GetJob(ctx, orgID, jobID)
    if err != nil {
        return nil, err
    }
    if _, _, err := s.GetStorageTarget(ctx, orgID, validated.StorageTargetID); err != nil {
        return nil, fmt.Errorf("storage target: %w", err)
    }
    job.NodeID = validated.NodeID
    job.Name = validated.Name
    job.Description = validated.Description
    job.Enabled = validated.Enabled
    job.Timezone = validated.Timezone
    job.CronExpression = validated.CronExpression
    job.RetentionDays = validated.RetentionDays
    job.StorageTargetID = validated.StorageTargetID
    job.CompressionEnabled = validated.CompressionEnabled
    job.CompressionLevel = validated.CompressionLevel
    job.UploadConcurrency = validated.UploadConcurrency
    job.UpdatedAt = time.Now()
    if err := s.DB.WithContext(ctx).Save(job).Error; err != nil {
        return nil, err
    }
    s.refreshJobEnabledMetric(ctx)
    return job, nil
}

func (s *Service) DeleteJob(ctx context.Context, orgID, jobID uuid.UUID) error {
    if err := s.DB.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, jobID).Delete(&db.BackupJob{}).Error; err != nil {
        return err
    }
    s.refreshJobEnabledMetric(ctx)
    return nil
}

func (s *Service) SetJobEnabled(ctx context.Context, orgID, jobID uuid.UUID, enabled bool) (*db.BackupJob, error) {
    job, _, err := s.GetJob(ctx, orgID, jobID)
    if err != nil {
        return nil, err
    }
    job.Enabled = enabled
    job.UpdatedAt = time.Now()
    if !enabled && job.LastStatus == StatusScheduled {
        job.LastStatus = StatusIdle
    }
    if err := s.DB.WithContext(ctx).Save(job).Error; err != nil {
        return nil, err
    }
    s.refreshJobEnabledMetric(ctx)
    return job, nil
}

func (s *Service) AddSource(ctx context.Context, orgID, jobID uuid.UUID, input SourceInput) (*db.BackupSource, error) {
    job, _, err := s.GetJob(ctx, orgID, jobID)
    if err != nil || job == nil {
        return nil, err
    }
    normalized, err := ValidateSourceConfig(input.Type, input.ConfigRaw, input.AllowUnsafeCommand && s.AllowUnsafeCommands)
    if err != nil {
        return nil, err
    }
    name := strings.TrimSpace(input.Name)
    if name == "" {
        name = strings.ReplaceAll(input.Type, "_", " ")
    }
    row := &db.BackupSource{
        ID:         uuid.New(),
        JobID:      jobID,
        Type:       strings.TrimSpace(input.Type),
        Name:       name,
        Enabled:    input.Enabled,
        OrderIndex: input.OrderIndex,
        ConfigJSON: datatypes.JSON(normalized),
        CreatedAt:  time.Now(),
        UpdatedAt:  time.Now(),
    }
    if err := s.DB.WithContext(ctx).Create(row).Error; err != nil {
        return nil, err
    }
    return row, nil
}

func (s *Service) UpdateSource(ctx context.Context, orgID, sourceID uuid.UUID, input SourceInput) (*db.BackupSource, error) {
    var row db.BackupSource
    err := s.DB.WithContext(ctx).
        Joins("JOIN backup_jobs ON backup_jobs.id = backup_sources.job_id").
        Where("backup_jobs.org_id = ? AND backup_sources.id = ?", orgID, sourceID).
        First(&row).Error
    if err != nil {
        return nil, err
    }
    if trimmed := strings.TrimSpace(input.Name); trimmed != "" {
        row.Name = trimmed
    }
    if trimmed := strings.TrimSpace(input.Type); trimmed != "" {
        row.Type = trimmed
    }
    if len(input.ConfigRaw) > 0 {
        normalized, err := ValidateSourceConfig(row.Type, input.ConfigRaw, input.AllowUnsafeCommand && s.AllowUnsafeCommands)
        if err != nil {
            return nil, err
        }
        row.ConfigJSON = datatypes.JSON(normalized)
    }
    row.Enabled = input.Enabled
    row.OrderIndex = input.OrderIndex
    row.UpdatedAt = time.Now()
    if err := s.DB.WithContext(ctx).Save(&row).Error; err != nil {
        return nil, err
    }
    return &row, nil
}

func (s *Service) DeleteSource(ctx context.Context, orgID, sourceID uuid.UUID) error {
    return s.DB.WithContext(ctx).
        Exec(`DELETE FROM backup_sources USING backup_jobs WHERE backup_sources.job_id = backup_jobs.id AND backup_jobs.org_id = ? AND backup_sources.id = ?`, orgID, sourceID).
        Error
}

func (s *Service) ReorderSources(ctx context.Context, orgID, jobID uuid.UUID, sourceIDs []uuid.UUID) error {
    job, _, err := s.GetJob(ctx, orgID, jobID)
    if err != nil || job == nil {
        return err
    }
    return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        for index, sourceID := range sourceIDs {
            if err := tx.Exec(`UPDATE backup_sources SET order_index = ?, updated_at = now() WHERE id = ? AND job_id = ?`, index, sourceID, jobID).Error; err != nil {
                return err
            }
        }
        return nil
    })
}

func (s *Service) ListRuns(ctx context.Context, orgID uuid.UUID, options RunListOptions) ([]db.BackupRun, error) {
    var rows []db.BackupRun
    query := s.DB.WithContext(ctx).Where("org_id = ?", orgID)
    if options.JobID != nil && *options.JobID != uuid.Nil {
        query = query.Where("job_id = ?", *options.JobID)
    }
    if strings.TrimSpace(options.Status) != "" {
        query = query.Where("status = ?", strings.TrimSpace(options.Status))
    }
    if options.Limit <= 0 || options.Limit > 200 {
        options.Limit = 50
    }
    if options.Desc {
        query = query.Order("started_at DESC")
    } else {
        query = query.Order("started_at ASC")
    }
    err := query.Offset(options.Offset).Limit(options.Limit).Find(&rows).Error
    return rows, err
}

func (s *Service) GetRun(ctx context.Context, orgID, runID uuid.UUID) (*db.BackupRun, []db.BackupRunItem, error) {
    var run db.BackupRun
    if err := s.DB.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, runID).First(&run).Error; err != nil {
        return nil, nil, err
    }
    var items []db.BackupRunItem
    if err := s.DB.WithContext(ctx).Where("run_id = ?", runID).Order("started_at, logical_name").Find(&items).Error; err != nil {
        return nil, nil, err
    }
    return &run, items, nil
}

func (s *Service) GetRunLog(ctx context.Context, orgID, runID uuid.UUID) (string, error) {
    run, _, err := s.GetRun(ctx, orgID, runID)
    if err != nil {
        return "", err
    }
    if run.LogPath == nil || strings.TrimSpace(*run.LogPath) == "" {
        if run.LogExcerpt != nil {
            return *run.LogExcerpt, nil
        }
        return "", nil
    }
    raw, err := os.ReadFile(*run.LogPath)
    if err != nil {
        if run.LogExcerpt != nil {
            return *run.LogExcerpt, nil
        }
        return "", err
    }
    return string(raw), nil
}

func (s *Service) ListTemplates(ctx context.Context) ([]db.BackupTemplate, error) {
    var rows []db.BackupTemplate
    err := s.DB.WithContext(ctx).Order("name").Find(&rows).Error
    return rows, err
}

func (s *Service) CreateJobFromTemplate(ctx context.Context, orgID uuid.UUID, templateID uuid.UUID, input JobInput) (*db.BackupJob, []db.BackupSource, error) {
    var tpl db.BackupTemplate
    if err := s.DB.WithContext(ctx).Where("id = ?", templateID).First(&tpl).Error; err != nil {
        return nil, nil, err
    }
    job, err := s.CreateJob(ctx, orgID, input)
    if err != nil {
        return nil, nil, err
    }
    var payload struct {
        Sources []struct {
            Name      string          `json:"name"`
            Type      string          `json:"type"`
            Order     int             `json:"order_index"`
            Enabled   bool            `json:"enabled"`
            ConfigRaw json.RawMessage `json:"config"`
        } `json:"sources"`
    }
    if err := json.Unmarshal(tpl.DefinitionJSON, &payload); err != nil {
        return nil, nil, err
    }
    created := make([]db.BackupSource, 0, len(payload.Sources))
    for _, item := range payload.Sources {
        source, err := s.AddSource(ctx, orgID, job.ID, SourceInput{
            Name:       item.Name,
            Type:       item.Type,
            Enabled:    item.Enabled,
            OrderIndex: item.Order,
            ConfigRaw:  item.ConfigRaw,
        })
        if err != nil {
            return nil, nil, err
        }
        created = append(created, *source)
    }
    return job, created, nil
}

func (s *Service) BuildExecutionPlan(ctx context.Context, orgID, jobID uuid.UUID) (*ExecutionPlan, error) {
    job, sources, err := s.GetJob(ctx, orgID, jobID)
    if err != nil {
        return nil, err
    }
    target, cfg, err := s.GetStorageTarget(ctx, orgID, job.StorageTargetID)
    if err != nil {
        return nil, err
    }
    plan := &ExecutionPlan{
        JobID:              job.ID,
        NodeID:             job.NodeID,
        StorageTargetID:    job.StorageTargetID,
        Timezone:           job.Timezone,
        CronExpression:     job.CronExpression,
        RetentionDays:      job.RetentionDays,
        CompressionEnabled: job.CompressionEnabled,
        CompressionLevel:   job.CompressionLevel,
        UploadConcurrency:  job.UploadConcurrency,
        StorageType:        target.Type,
        StorageSummary:     MaskStorageConfig(cfg),
        Sources:            make([]ExecutionPlanItem, 0, len(sources)),
    }
    for _, source := range sources {
        plan.Sources = append(plan.Sources, ExecutionPlanItem{
            SourceID:    source.ID,
            Name:        source.Name,
            Type:        source.Type,
            LogicalName: source.Name,
            Config:      append(json.RawMessage{}, source.ConfigJSON...),
        })
    }
    return plan, nil
}

func (s *Service) encryptStorageConfig(cfg StorageTargetConfig) (string, error) {
    raw, err := json.Marshal(cfg)
    if err != nil {
        return "", err
    }
    return s.Encryptor.EncryptString(string(raw))
}

func (s *Service) decryptStorageConfig(value string) (StorageTargetConfig, error) {
    out := StorageTargetConfig{}
    if strings.TrimSpace(value) == "" {
        return out, nil
    }
    raw, err := s.Encryptor.DecryptString(value)
    if err != nil {
        return out, err
    }
    err = json.Unmarshal([]byte(raw), &out)
    return out, err
}

func (s *Service) DecryptStorageTargetConfig(row db.BackupStorageTarget) (StorageTargetConfig, error) {
	return s.decryptStorageConfig(row.ConfigEnc)
}

func MaskStorageConfig(cfg StorageTargetConfig) map[string]any {
    return map[string]any{
        "host":                 cfg.Host,
        "port":                 cfg.Port,
        "username":             cfg.Username,
        "base_path":            cfg.BasePath,
        "timeout_sec":          int(cfg.TimeoutSec),
        "passive_mode":         cfg.PassiveMode,
        "insecure_skip_verify": cfg.InsecureSkipVerify,
        "auth_method":          cfg.AuthMethod,
        "url":                  cfg.URL,
        "bucket":               cfg.Bucket,
        "region":               cfg.Region,
        "use_ssl":              cfg.UseSSL,
        "path_style":           cfg.PathStyle,
        "local_path":           cfg.LocalPath,
        "password_set":         strings.TrimSpace(cfg.Password) != "",
        "private_key_set":      strings.TrimSpace(cfg.PrivateKeyPEM) != "",
        "secret_key_set":       strings.TrimSpace(cfg.SecretKey) != "",
        "access_key":           cfg.AccessKey,
    }
}

func mergeStorageConfig(existing StorageTargetConfig, update StorageTargetConfig) StorageTargetConfig {
    merged := existing
    if strings.TrimSpace(update.Host) != "" {
        merged.Host = strings.TrimSpace(update.Host)
    }
    if update.Port > 0 {
        merged.Port = update.Port
    }
    if strings.TrimSpace(update.Username) != "" {
        merged.Username = strings.TrimSpace(update.Username)
    }
    if update.Password != "" {
        merged.Password = update.Password
    }
    if strings.TrimSpace(update.BasePath) != "" {
        merged.BasePath = strings.TrimSpace(update.BasePath)
    }
    if update.TimeoutSec > 0 {
        merged.TimeoutSec = update.TimeoutSec
    }
    merged.PassiveMode = update.PassiveMode
    merged.InsecureSkipVerify = update.InsecureSkipVerify
    if strings.TrimSpace(update.AuthMethod) != "" {
        merged.AuthMethod = strings.TrimSpace(update.AuthMethod)
    }
    if strings.TrimSpace(update.PrivateKeyPEM) != "" {
        merged.PrivateKeyPEM = strings.TrimSpace(update.PrivateKeyPEM)
    }
    if strings.TrimSpace(update.URL) != "" {
        merged.URL = strings.TrimSpace(update.URL)
    }
    if strings.TrimSpace(update.Bucket) != "" {
        merged.Bucket = strings.TrimSpace(update.Bucket)
    }
    if strings.TrimSpace(update.Region) != "" {
        merged.Region = strings.TrimSpace(update.Region)
    }
    if strings.TrimSpace(update.AccessKey) != "" {
        merged.AccessKey = strings.TrimSpace(update.AccessKey)
    }
    if strings.TrimSpace(update.SecretKey) != "" {
        merged.SecretKey = strings.TrimSpace(update.SecretKey)
    }
    merged.UseSSL = update.UseSSL
    merged.PathStyle = update.PathStyle
    if strings.TrimSpace(update.LocalPath) != "" {
        merged.LocalPath = strings.TrimSpace(update.LocalPath)
    }
    return merged
}

func (s *Service) makeRunWorkdir(runID uuid.UUID) (string, error) {
    workdir := filepath.Join(s.DataDir, "backup", "runs", runID.String())
    return workdir, os.MkdirAll(workdir, 0o755)
}

func (s *Service) refreshJobEnabledMetric(ctx context.Context) {
    if s == nil || s.DB == nil {
        return
    }
    var count int64
    if err := s.DB.WithContext(ctx).Model(&db.BackupJob{}).Where("enabled = true").Count(&count).Error; err == nil {
        metricJobEnabled.Set(float64(count))
    }
}

func (s *Service) newUploader(cfg StorageTargetConfig, targetType string) (storage.Uploader, error) {
    sc := storage.Config{
        Type:               targetType,
        Name:               targetType,
        Host:               cfg.Host,
        Port:               cfg.Port,
        Username:           cfg.Username,
        Password:           cfg.Password,
        BasePath:           cfg.BasePath,
        Timeout:            time.Duration(cfg.TimeoutSec) * time.Second,
        PassiveMode:        cfg.PassiveMode,
        InsecureSkipVerify: cfg.InsecureSkipVerify,
        AuthMethod:         cfg.AuthMethod,
        PrivateKeyPEM:      cfg.PrivateKeyPEM,
        URL:                cfg.URL,
        Bucket:             cfg.Bucket,
        Region:             cfg.Region,
        AccessKey:          cfg.AccessKey,
        SecretKey:          cfg.SecretKey,
        UseSSL:             cfg.UseSSL,
        PathStyle:          cfg.PathStyle,
        LocalPath:          cfg.LocalPath,
    }
    return NewUploader(sc)
}
