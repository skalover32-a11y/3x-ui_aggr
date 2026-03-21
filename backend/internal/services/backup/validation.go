package backup

import (
    "encoding/json"
    "errors"
    "fmt"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"

    "github.com/google/uuid"
    "github.com/robfig/cron/v3"
)

var (
    allowedSourcePath = regexp.MustCompile(`^/[-_A-Za-z0-9./@]+$`)
    s3BucketPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

type JobInput struct {
    Name               string
    Description        string
    NodeID             *uuid.UUID
    Enabled            bool
    Timezone           string
    CronExpression     string
    RetentionDays      int
    StorageTargetID    uuid.UUID
    CompressionEnabled bool
    CompressionLevel   *int
    UploadConcurrency  int
}

func ValidateJobInput(input JobInput) (JobInput, error) {
    input.Name = strings.TrimSpace(input.Name)
    if input.Name == "" {
        return input, errors.New("name is required")
    }
    input.Description = strings.TrimSpace(input.Description)
    input.Timezone = NormalizeTimezone(input.Timezone)
    input.CronExpression = strings.TrimSpace(input.CronExpression)
    if input.CronExpression == "" {
        input.CronExpression = "0 3 * * *"
    }
    if _, err := cron.ParseStandard(input.CronExpression); err != nil {
        return input, fmt.Errorf("invalid cron expression: %w", err)
    }
    if input.RetentionDays <= 0 {
        input.RetentionDays = 14
    }
    if input.RetentionDays > 3650 {
        return input, errors.New("retention_days is too large")
    }
    if input.StorageTargetID == uuid.Nil {
        return input, errors.New("storage_target_id is required")
    }
    if input.UploadConcurrency <= 0 {
        input.UploadConcurrency = 2
    }
    if input.UploadConcurrency > 8 {
        return input, errors.New("upload_concurrency must be <= 8")
    }
    if input.CompressionLevel != nil {
        value := *input.CompressionLevel
        if value < 1 || value > 9 {
            return input, errors.New("compression_level must be between 1 and 9")
        }
    }
    return input, nil
}

func ValidateSourceConfig(sourceType string, raw []byte, allowUnsafeCommands bool) ([]byte, error) {
    sourceType = strings.TrimSpace(sourceType)
    if !IsSupportedSourceType(sourceType) {
        return nil, errors.New("unsupported source type")
    }
    if len(raw) == 0 {
        raw = []byte("{}")
    }
    switch sourceType {
    case SourceFilePath, SourceDirectoryPath:
        var cfg PathSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid path config: %w", err)
        }
        cfg.Path = strings.TrimSpace(cfg.Path)
        if err := validateUnixPath(cfg.Path); err != nil {
            return nil, err
        }
        if sourceType == SourceDirectoryPath {
            cfg.Archive = true
        }
        return json.Marshal(cfg)
    case SourceDockerVolume:
        var cfg DockerVolumeSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid docker volume config: %w", err)
        }
        cfg.VolumeName = strings.TrimSpace(cfg.VolumeName)
        if cfg.VolumeName == "" {
            return nil, errors.New("volume_name is required")
        }
        if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]+$`).MatchString(cfg.VolumeName) {
            return nil, errors.New("volume_name contains unsupported characters")
        }
        cfg.Archive = true
        return json.Marshal(cfg)
    case SourcePostgresContainer:
        var cfg PostgresContainerSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid postgres container config: %w", err)
        }
        cfg.ContainerName = strings.TrimSpace(cfg.ContainerName)
        cfg.DBName = strings.TrimSpace(cfg.DBName)
        cfg.DBUser = strings.TrimSpace(cfg.DBUser)
        cfg.DBPassword = strings.TrimSpace(cfg.DBPassword)
        cfg.DumpArgs = strings.TrimSpace(cfg.DumpArgs)
        if cfg.ContainerName == "" {
            return nil, errors.New("container_name is required")
        }
        if !cfg.AutoDetectFromEnv && (cfg.DBName == "" || cfg.DBUser == "") {
            return nil, errors.New("db_name and db_user are required when auto detection is disabled")
        }
        return json.Marshal(cfg)
    case SourcePostgresManual:
        var cfg PostgresManualSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid postgres manual config: %w", err)
        }
        cfg.Host = strings.TrimSpace(cfg.Host)
        cfg.DBName = strings.TrimSpace(cfg.DBName)
        cfg.DBUser = strings.TrimSpace(cfg.DBUser)
        cfg.DBPassword = strings.TrimSpace(cfg.DBPassword)
        cfg.DumpArgs = strings.TrimSpace(cfg.DumpArgs)
        if cfg.Host == "" || cfg.DBName == "" || cfg.DBUser == "" {
            return nil, errors.New("host, db_name and db_user are required")
        }
        if cfg.Port <= 0 {
            cfg.Port = 5432
        }
        return json.Marshal(cfg)
    case SourceNginxSnapshot:
        var cfg NginxSnapshotSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid nginx snapshot config: %w", err)
        }
        if strings.TrimSpace(cfg.Command) == "" {
            cfg.Command = "nginx -T"
        }
        if strings.TrimSpace(cfg.OutputName) == "" {
            cfg.OutputName = "nginx-full-config"
        }
        return json.Marshal(cfg)
    case SourceCronSnapshot:
        var cfg CronSnapshotSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid cron snapshot config: %w", err)
        }
        if strings.TrimSpace(cfg.OutputName) == "" {
            cfg.OutputName = "cron-snapshot"
        }
        return json.Marshal(cfg)
    case SourceDockerInventory:
        var cfg DockerInventorySourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid docker inventory config: %w", err)
        }
        if strings.TrimSpace(cfg.OutputName) == "" {
            cfg.OutputName = "docker-inventory"
        }
        return json.Marshal(cfg)
    case SourceSystemSnapshot:
        var cfg SystemSnapshotSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid system snapshot config: %w", err)
        }
        if strings.TrimSpace(cfg.OutputName) == "" {
            cfg.OutputName = "system-snapshot"
        }
        return json.Marshal(cfg)
    case SourceCustomCommand:
        var cfg CustomCommandSourceConfig
        if err := json.Unmarshal(raw, &cfg); err != nil {
            return nil, fmt.Errorf("invalid custom command config: %w", err)
        }
        cfg.Command = strings.TrimSpace(cfg.Command)
        cfg.OutputName = strings.TrimSpace(cfg.OutputName)
        if cfg.Command == "" {
            return nil, errors.New("command is required")
        }
        if !allowUnsafeCommands || !cfg.AllowUntrustedCommand {
            return nil, errors.New("custom commands require explicit unsafe enablement")
        }
        if cfg.OutputName == "" {
            cfg.OutputName = "custom-command"
        }
        return json.Marshal(cfg)
    case SourceMySQLContainer, SourceMySQLManual:
        return nil, errors.New("mysql sources are not implemented yet")
    default:
        return nil, errors.New("unsupported source type")
    }
}

func ValidateStorageConfig(targetType string, cfg StorageTargetConfig) (StorageTargetConfig, error) {
    targetType = strings.TrimSpace(targetType)
    if !IsSupportedStorageType(targetType) {
        return cfg, errors.New("unsupported storage target type")
    }
    defaults := DefaultStorageConfig(targetType)
    if cfg.Port == 0 {
        cfg.Port = defaults.Port
    }
    if cfg.TimeoutSec <= 0 {
        cfg.TimeoutSec = defaults.TimeoutSec
    }
    cfg.BasePath = normalizeBasePath(cfg.BasePath)
    cfg.AuthMethod = strings.TrimSpace(strings.ToLower(cfg.AuthMethod))
    cfg.Host = strings.TrimSpace(cfg.Host)
    cfg.Username = strings.TrimSpace(cfg.Username)
    cfg.URL = strings.TrimSpace(cfg.URL)
    cfg.Bucket = strings.TrimSpace(cfg.Bucket)
    cfg.Region = strings.TrimSpace(cfg.Region)
    cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
    cfg.LocalPath = strings.TrimSpace(cfg.LocalPath)
    switch targetType {
    case StorageFTP, StorageFTPS:
        if cfg.Host == "" {
            return cfg, errors.New("host is required")
        }
        if cfg.Username == "" {
            return cfg, errors.New("username is required")
        }
        if cfg.Password == "" {
            return cfg, errors.New("password is required")
        }
    case StorageSFTP:
        if cfg.Host == "" {
            return cfg, errors.New("host is required")
        }
        if cfg.Username == "" {
            return cfg, errors.New("username is required")
        }
        if cfg.AuthMethod == "" {
            cfg.AuthMethod = defaults.AuthMethod
        }
        if cfg.AuthMethod != "password" && cfg.AuthMethod != "key" {
            return cfg, errors.New("auth_method must be password or key")
        }
        if cfg.AuthMethod == "password" && cfg.Password == "" {
            return cfg, errors.New("password is required")
        }
        if cfg.AuthMethod == "key" && strings.TrimSpace(cfg.PrivateKeyPEM) == "" {
            return cfg, errors.New("private_key_pem is required")
        }
    case StorageWebDAV:
        if cfg.URL == "" {
            return cfg, errors.New("url is required")
        }
        if cfg.Username == "" {
            return cfg, errors.New("username is required")
        }
        if cfg.Password == "" {
            return cfg, errors.New("password is required")
        }
    case StorageS3:
        if cfg.Host == "" {
            return cfg, errors.New("endpoint host is required")
        }
        if !s3BucketPattern.MatchString(cfg.Bucket) {
            return cfg, errors.New("bucket is invalid")
        }
        if cfg.AccessKey == "" || cfg.SecretKey == "" {
            return cfg, errors.New("access_key and secret_key are required")
        }
    case StorageLocal:
        if cfg.LocalPath == "" {
            return cfg, errors.New("local_path is required")
        }
    }
    return cfg, nil
}

func validateUnixPath(value string) error {
    value = strings.TrimSpace(value)
    if value == "" {
        return errors.New("path is required")
    }
    if !allowedSourcePath.MatchString(value) {
        return errors.New("path contains unsupported characters")
    }
    for _, part := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
        if part == ".." {
            return errors.New("path traversal is not allowed")
        }
    }
    cleaned := filepath.ToSlash(filepath.Clean(value))
    if cleaned == "." || !strings.HasPrefix(cleaned, "/") {
        return errors.New("path must be absolute")
    }
    if strings.Contains(cleaned, "../") || strings.HasSuffix(cleaned, "/..") {
        return errors.New("path traversal is not allowed")
    }
    return nil
}

func normalizeBasePath(value string) string {
    value = strings.TrimSpace(value)
    if value == "" {
        return "/"
    }
    if strings.HasPrefix(value, "/") {
        return strings.TrimRight(value, "/")
    }
    return "/" + strings.TrimRight(value, "/")
}

func ParseMaskedInt(value string, fallback int) int {
    parsed, err := strconv.Atoi(strings.TrimSpace(value))
    if err != nil || parsed <= 0 {
        return fallback
    }
    return parsed
}
