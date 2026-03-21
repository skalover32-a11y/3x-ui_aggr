package backup

import (
    "encoding/json"
    "testing"

    "github.com/google/uuid"
)

func TestValidateJobInputAppliesDefaults(t *testing.T) {
    targetID := uuid.New()
    got, err := ValidateJobInput(JobInput{
        Name:            " nightly ",
        Timezone:        "Europe/Moscow",
        CronExpression:  "",
        RetentionDays:   0,
        StorageTargetID: targetID,
    })
    if err != nil {
        t.Fatalf("ValidateJobInput returned error: %v", err)
    }
    if got.Name != "nightly" {
        t.Fatalf("unexpected normalized name: %q", got.Name)
    }
    if got.CronExpression != "0 3 * * *" {
        t.Fatalf("expected default cron, got %q", got.CronExpression)
    }
    if got.RetentionDays != 14 {
        t.Fatalf("expected default retention, got %d", got.RetentionDays)
    }
    if got.UploadConcurrency != 2 {
        t.Fatalf("expected default concurrency, got %d", got.UploadConcurrency)
    }
}

func TestValidateJobInputRejectsInvalidCron(t *testing.T) {
    _, err := ValidateJobInput(JobInput{
        Name:            "broken",
        Timezone:        "UTC",
        CronExpression:  "not-a-cron",
        RetentionDays:   7,
        StorageTargetID: uuid.New(),
    })
    if err == nil {
        t.Fatal("expected cron validation error")
    }
}

func TestValidateSourceConfigRejectsTraversal(t *testing.T) {
    raw := []byte(`{"path":"/etc/../shadow","archive":true}`)
    _, err := ValidateSourceConfig(SourceFilePath, raw, false)
    if err == nil {
        t.Fatal("expected traversal validation error")
    }
}

func TestValidateSourceConfigNormalizesPathDirectoryArchive(t *testing.T) {
    raw := []byte(`{"path":"/etc/nginx","archive":false}`)
    normalized, err := ValidateSourceConfig(SourceDirectoryPath, raw, false)
    if err != nil {
        t.Fatalf("ValidateSourceConfig returned error: %v", err)
    }
    var cfg PathSourceConfig
    if err := json.Unmarshal(normalized, &cfg); err != nil {
        t.Fatalf("unmarshal normalized config: %v", err)
    }
    if cfg.Path != "/etc/nginx" {
        t.Fatalf("unexpected normalized path: %q", cfg.Path)
    }
    if !cfg.Archive {
        t.Fatal("directory_path should always archive")
    }
}

func TestValidateSourceConfigBlocksCustomCommandWithoutUnsafeFlag(t *testing.T) {
    raw := []byte(`{"command":"echo hi","output_name":"x","allow_untrusted_command":true}`)
    _, err := ValidateSourceConfig(SourceCustomCommand, raw, false)
    if err == nil {
        t.Fatal("expected custom command validation error")
    }
}

func TestValidateStorageConfigDefaultsAndValidation(t *testing.T) {
    cfg, err := ValidateStorageConfig(StorageSFTP, StorageTargetConfig{
        Host:       "backup.example.com",
        Username:   "root",
        AuthMethod: "password",
        Password:   "secret",
    })
    if err != nil {
        t.Fatalf("ValidateStorageConfig returned error: %v", err)
    }
    if cfg.Port != 22 {
        t.Fatalf("expected default sftp port, got %d", cfg.Port)
    }
    if cfg.TimeoutSec != 30 {
        t.Fatalf("expected default timeout, got %d", cfg.TimeoutSec)
    }
    if cfg.BasePath != "/" {
        t.Fatalf("expected default base path, got %q", cfg.BasePath)
    }
}

func TestValidateStorageConfigRejectsMissingSecret(t *testing.T) {
    _, err := ValidateStorageConfig(StorageFTP, StorageTargetConfig{
        Host:     "ftp.example.com",
        Username: "backup",
    })
    if err == nil {
        t.Fatal("expected ftp password validation error")
    }
}
