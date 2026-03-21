package backup

import (
    "encoding/json"
    "fmt"
    "sort"
    "strings"
    "time"

    "github.com/google/uuid"
)

const (
    StatusIdle           = "idle"
    StatusScheduled      = "scheduled"
    StatusQueued         = "queued"
    StatusRunning        = "running"
    StatusSuccess        = "success"
    StatusFailed         = "failed"
    StatusPartialSuccess = "partial_success"
    StatusCancelled      = "cancelled"

    TriggerManual    = "manual"
    TriggerScheduled = "scheduled"
    TriggerRetry     = "retry"

    CleanupPending = "pending"
    CleanupDone    = "done"
    CleanupFailed  = "failed"

    ChecksumPending = "pending"
    ChecksumDone    = "done"
    ChecksumFailed  = "failed"
)

const (
    SourceFilePath             = "file_path"
    SourceDirectoryPath        = "directory_path"
    SourceDockerVolume         = "docker_volume"
    SourcePostgresContainer    = "postgres_container_dump"
    SourcePostgresManual       = "postgres_manual_dump"
    SourceMySQLContainer       = "mysql_container_dump"
    SourceMySQLManual          = "mysql_manual_dump"
    SourceNginxSnapshot        = "nginx_snapshot"
    SourceCronSnapshot         = "cron_snapshot"
    SourceDockerInventory      = "docker_inventory_snapshot"
    SourceSystemSnapshot       = "system_snapshot"
    SourceCustomCommand        = "custom_command"
)

const (
    StorageFTP   = "ftp"
    StorageFTPS  = "ftps"
    StorageSFTP  = "sftp"
    StorageWebDAV = "webdav"
    StorageS3    = "s3"
    StorageLocal = "local"
)

var SupportedSourceTypes = map[string]struct{}{
    SourceFilePath:          {},
    SourceDirectoryPath:     {},
    SourceDockerVolume:      {},
    SourcePostgresContainer: {},
    SourcePostgresManual:    {},
    SourceMySQLContainer:    {},
    SourceMySQLManual:       {},
    SourceNginxSnapshot:     {},
    SourceCronSnapshot:      {},
    SourceDockerInventory:   {},
    SourceSystemSnapshot:    {},
    SourceCustomCommand:     {},
}

var SupportedStorageTypes = map[string]struct{}{
    StorageFTP:    {},
    StorageFTPS:   {},
    StorageSFTP:   {},
    StorageWebDAV: {},
    StorageS3:     {},
    StorageLocal:  {},
}

type StorageTargetConfig struct {
    Host               string `json:"host,omitempty"`
    Port               int    `json:"port,omitempty"`
    Username           string `json:"username,omitempty"`
    Password           string `json:"password,omitempty"`
    BasePath           string `json:"base_path,omitempty"`
    TimeoutSec         int    `json:"timeout_sec,omitempty"`
    PassiveMode        bool   `json:"passive_mode,omitempty"`
    InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
    AuthMethod         string `json:"auth_method,omitempty"`
    PrivateKeyPEM      string `json:"private_key_pem,omitempty"`
    URL                string `json:"url,omitempty"`
    Bucket             string `json:"bucket,omitempty"`
    Region             string `json:"region,omitempty"`
    AccessKey          string `json:"access_key,omitempty"`
    SecretKey          string `json:"secret_key,omitempty"`
    UseSSL             bool   `json:"use_ssl,omitempty"`
    PathStyle          bool   `json:"path_style,omitempty"`
    LocalPath          string `json:"local_path,omitempty"`
}

type PathSourceConfig struct {
    Path    string `json:"path"`
    Archive bool   `json:"archive"`
}

type DockerVolumeSourceConfig struct {
    VolumeName string `json:"volume_name"`
    Archive    bool   `json:"archive"`
}

type PostgresContainerSourceConfig struct {
    ContainerName      string `json:"container_name"`
    DBName             string `json:"db_name,omitempty"`
    DBUser             string `json:"db_user,omitempty"`
    DBPassword         string `json:"db_password,omitempty"`
    AutoDetectFromEnv  bool   `json:"auto_detect_from_env"`
    DumpArgs           string `json:"dump_args,omitempty"`
}

type PostgresManualSourceConfig struct {
    Host        string `json:"host"`
    Port        int    `json:"port"`
    DBName      string `json:"db_name"`
    DBUser      string `json:"db_user"`
    DBPassword  string `json:"db_password"`
    DumpArgs    string `json:"dump_args,omitempty"`
}

type NginxSnapshotSourceConfig struct {
    Command    string `json:"command,omitempty"`
    OutputName string `json:"output_name,omitempty"`
}

type CronSnapshotSourceConfig struct {
    IncludeSystem bool   `json:"include_system"`
    OutputName    string `json:"output_name,omitempty"`
}

type DockerInventorySourceConfig struct {
    OutputName string `json:"output_name,omitempty"`
}

type SystemSnapshotSourceConfig struct {
    OutputName string `json:"output_name,omitempty"`
}

type CustomCommandSourceConfig struct {
    Command               string `json:"command"`
    OutputName            string `json:"output_name,omitempty"`
    AllowUntrustedCommand bool   `json:"allow_untrusted_command"`
}

type ExecutionPlan struct {
    JobID               uuid.UUID           `json:"job_id"`
    NodeID              *uuid.UUID          `json:"node_id,omitempty"`
    StorageTargetID     uuid.UUID           `json:"storage_target_id"`
    Timezone            string              `json:"timezone"`
    CronExpression      string              `json:"cron_expression"`
    RetentionDays       int                 `json:"retention_days"`
    CompressionEnabled  bool                `json:"compression_enabled"`
    CompressionLevel    *int                `json:"compression_level,omitempty"`
    UploadConcurrency   int                 `json:"upload_concurrency"`
    StorageType         string              `json:"storage_type"`
    StorageSummary      map[string]any      `json:"storage_summary"`
    Sources             []ExecutionPlanItem `json:"sources"`
}

type ExecutionPlanItem struct {
    SourceID     uuid.UUID       `json:"source_id"`
    Name         string          `json:"name"`
    Type         string          `json:"type"`
    LogicalName  string          `json:"logical_name"`
    Config       json.RawMessage `json:"config"`
}

type RunSummary struct {
    Total   int `json:"total"`
    Success int `json:"success"`
    Failed  int `json:"failed"`
    Partial int `json:"partial"`
    Running int `json:"running"`
    Queued  int `json:"queued"`
}

func IsSupportedSourceType(value string) bool {
    _, ok := SupportedSourceTypes[strings.TrimSpace(value)]
    return ok
}

func IsSupportedStorageType(value string) bool {
    _, ok := SupportedStorageTypes[strings.TrimSpace(value)]
    return ok
}

func DefaultStorageConfig(targetType string) StorageTargetConfig {
    switch targetType {
    case StorageFTP:
        return StorageTargetConfig{Port: 21, TimeoutSec: 30, PassiveMode: true}
    case StorageFTPS:
        return StorageTargetConfig{Port: 21, TimeoutSec: 30, PassiveMode: true}
    case StorageSFTP:
        return StorageTargetConfig{Port: 22, TimeoutSec: 30, AuthMethod: "password"}
    case StorageWebDAV:
        return StorageTargetConfig{TimeoutSec: 30}
    case StorageS3:
        return StorageTargetConfig{UseSSL: true, TimeoutSec: 30}
    case StorageLocal:
        return StorageTargetConfig{LocalPath: "./data/backups/export"}
    default:
        return StorageTargetConfig{TimeoutSec: 30}
    }
}

func SourceTypesSorted() []string {
    out := make([]string, 0, len(SupportedSourceTypes))
    for key := range SupportedSourceTypes {
        out = append(out, key)
    }
    sort.Strings(out)
    return out
}

func StorageTypesSorted() []string {
    out := make([]string, 0, len(SupportedStorageTypes))
    for key := range SupportedStorageTypes {
        out = append(out, key)
    }
    sort.Strings(out)
    return out
}

func TriggerLabel(trigger string) string {
    switch strings.TrimSpace(trigger) {
    case TriggerManual:
        return "Manual"
    case TriggerRetry:
        return "Retry"
    case TriggerScheduled:
        return "Scheduled"
    default:
        return strings.TrimSpace(trigger)
    }
}

func NormalizeTimezone(value string) string {
    value = strings.TrimSpace(value)
    if value == "" {
        return "UTC"
    }
    if _, err := time.LoadLocation(value); err != nil {
        return "UTC"
    }
    return value
}

func EnsureLeadingSlash(value string) string {
    trimmed := strings.TrimSpace(value)
    if trimmed == "" {
        return "/"
    }
    if strings.HasPrefix(trimmed, "/") {
        return trimmed
    }
    return "/" + trimmed
}

func MakeArtifactName(base string, suffix string) string {
    base = strings.TrimSpace(strings.ToLower(base))
    if base == "" {
        base = "artifact"
    }
    replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "..", "-")
    base = replacer.Replace(base)
    if suffix == "" {
        return base
    }
    return fmt.Sprintf("%s-%s", base, suffix)
}
