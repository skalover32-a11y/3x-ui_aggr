package backup

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "github.com/google/uuid"
    "gorm.io/datatypes"
    "gorm.io/gorm"

    "agr_3x_ui/internal/db"
)

type TemplateSeed struct {
    Slug        string
    Name        string
    Description string
    Sources     []TemplateSourceSeed
}

type TemplateSourceSeed struct {
    Name   string
    Type   string
    Config any
}

func BuiltInTemplates() []TemplateSeed {
    return []TemplateSeed{
        {
            Slug:        "website-server",
            Name:        "Website Server",
            Description: "Nginx, certificates, web roots and system snapshots",
            Sources: []TemplateSourceSeed{
                {Name: "/etc/nginx", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/etc/nginx", Archive: true}},
                {Name: "/etc/nginx/sites-available", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/etc/nginx/sites-available", Archive: true}},
                {Name: "/etc/nginx/sites-enabled", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/etc/nginx/sites-enabled", Archive: true}},
                {Name: "/etc/letsencrypt", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/etc/letsencrypt", Archive: true}},
                {Name: "/root/.acme.sh", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/root/.acme.sh", Archive: true}},
                {Name: "/var/www", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/var/www", Archive: true}},
                {Name: "nginx-full-config", Type: SourceNginxSnapshot, Config: NginxSnapshotSourceConfig{OutputName: "nginx-full-config"}},
                {Name: "cron", Type: SourceCronSnapshot, Config: CronSnapshotSourceConfig{IncludeSystem: true, OutputName: "cron-snapshot"}},
                {Name: "system", Type: SourceSystemSnapshot, Config: SystemSnapshotSourceConfig{OutputName: "system-snapshot"}},
            },
        },
        {
            Slug:        "remnawave",
            Name:        "Remnawave",
            Description: "Remnawave app, database, volumes and infra snapshots",
            Sources: []TemplateSourceSeed{
                {Name: "/opt/remnawave", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/remnawave", Archive: true}},
                {Name: "remnawave-db", Type: SourcePostgresContainer, Config: PostgresContainerSourceConfig{ContainerName: "remnawave-db", AutoDetectFromEnv: true}},
                {Name: "remnawave-db-data", Type: SourceDockerVolume, Config: DockerVolumeSourceConfig{VolumeName: "remnawave-db-data", Archive: true}},
                {Name: "nginx-full-config", Type: SourceNginxSnapshot, Config: NginxSnapshotSourceConfig{OutputName: "nginx-full-config"}},
                {Name: "docker-inventory", Type: SourceDockerInventory, Config: DockerInventorySourceConfig{OutputName: "docker-inventory"}},
                {Name: "cron", Type: SourceCronSnapshot, Config: CronSnapshotSourceConfig{IncludeSystem: true, OutputName: "cron-snapshot"}},
            },
        },
        {
            Slug:        "aggregator-node",
            Name:        "Aggregator Node",
            Description: "Aggregator app, postgres, prometheus and infra configs",
            Sources: []TemplateSourceSeed{
                {Name: "/opt/3x-ui_aggr", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/3x-ui_aggr", Archive: true}},
                {Name: "aggregator-postgres", Type: SourcePostgresContainer, Config: PostgresContainerSourceConfig{ContainerName: "3x-ui_aggr-postgres-1", AutoDetectFromEnv: true}},
                {Name: "prometheus-config", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/3x-ui_aggr/deploy/prometheus", Archive: true}},
                {Name: "prometheus-data", Type: SourceDockerVolume, Config: DockerVolumeSourceConfig{VolumeName: "3x-ui_aggr_prometheus_data", Archive: true}},
                {Name: "nginx-full-config", Type: SourceNginxSnapshot, Config: NginxSnapshotSourceConfig{OutputName: "nginx-full-config"}},
                {Name: "docker-inventory", Type: SourceDockerInventory, Config: DockerInventorySourceConfig{OutputName: "docker-inventory"}},
                {Name: "cron", Type: SourceCronSnapshot, Config: CronSnapshotSourceConfig{IncludeSystem: true, OutputName: "cron-snapshot"}},
            },
        },
        {
            Slug:        "vaultwarden",
            Name:        "Vaultwarden",
            Description: "Vaultwarden app and data with nginx and cron snapshots",
            Sources: []TemplateSourceSeed{
                {Name: "/opt/vaultwarden", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/vaultwarden", Archive: true}},
                {Name: "/opt/vaultwarden/data", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/vaultwarden/data", Archive: true}},
                {Name: "nginx-full-config", Type: SourceNginxSnapshot, Config: NginxSnapshotSourceConfig{OutputName: "nginx-full-config"}},
                {Name: "cron", Type: SourceCronSnapshot, Config: CronSnapshotSourceConfig{IncludeSystem: true, OutputName: "cron-snapshot"}},
            },
        },
        {
            Slug:        "docker-postgres-app",
            Name:        "Docker app + PostgreSQL",
            Description: "Generic dockerized application with postgres and host snapshots",
            Sources: []TemplateSourceSeed{
                {Name: "application-root", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/opt/app", Archive: true}},
                {Name: "postgres-container", Type: SourcePostgresContainer, Config: PostgresContainerSourceConfig{ContainerName: "postgres", AutoDetectFromEnv: true}},
                {Name: "docker-inventory", Type: SourceDockerInventory, Config: DockerInventorySourceConfig{OutputName: "docker-inventory"}},
                {Name: "system", Type: SourceSystemSnapshot, Config: SystemSnapshotSourceConfig{OutputName: "system-snapshot"}},
            },
        },
        {
            Slug:        "generic-linux-server",
            Name:        "Generic Linux server",
            Description: "Host configuration snapshots with selected paths",
            Sources: []TemplateSourceSeed{
                {Name: "/etc", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/etc", Archive: true}},
                {Name: "/var/www", Type: SourceDirectoryPath, Config: PathSourceConfig{Path: "/var/www", Archive: true}},
                {Name: "system", Type: SourceSystemSnapshot, Config: SystemSnapshotSourceConfig{OutputName: "system-snapshot"}},
                {Name: "cron", Type: SourceCronSnapshot, Config: CronSnapshotSourceConfig{IncludeSystem: true, OutputName: "cron-snapshot"}},
            },
        },
    }
}

func EnsureBuiltInTemplates(ctx context.Context, dbConn *gorm.DB) error {
    if dbConn == nil {
        return nil
    }
    for _, seed := range BuiltInTemplates() {
        definition, err := buildTemplateDefinition(seed)
        if err != nil {
            return err
        }
        var existing db.BackupTemplate
        err = dbConn.WithContext(ctx).Where("slug = ?", seed.Slug).First(&existing).Error
        if err == nil {
            existing.Name = seed.Name
            existing.Description = seed.Description
            existing.DefinitionJSON = definition
            existing.UpdatedAt = time.Now()
            if saveErr := dbConn.WithContext(ctx).Save(&existing).Error; saveErr != nil {
                return saveErr
            }
            continue
        }
        if err != gorm.ErrRecordNotFound {
            return err
        }
        row := db.BackupTemplate{
            ID:             uuid.New(),
            Slug:           seed.Slug,
            Name:           seed.Name,
            Description:    seed.Description,
            DefinitionJSON: definition,
            CreatedAt:      time.Now(),
            UpdatedAt:      time.Now(),
        }
        if createErr := dbConn.WithContext(ctx).Create(&row).Error; createErr != nil {
            return createErr
        }
    }
    return nil
}

func buildTemplateDefinition(seed TemplateSeed) (datatypes.JSON, error) {
    payload := map[string]any{
        "slug":        seed.Slug,
        "name":        seed.Name,
        "description": seed.Description,
        "sources":     make([]map[string]any, 0, len(seed.Sources)),
    }
    sources := payload["sources"].([]map[string]any)
    for index, source := range seed.Sources {
        raw, err := json.Marshal(source.Config)
        if err != nil {
            return nil, fmt.Errorf("template %s: %w", seed.Slug, err)
        }
        normalized, err := ValidateSourceConfig(source.Type, raw, false)
        if err != nil {
            return nil, fmt.Errorf("template %s source %s: %w", seed.Slug, source.Name, err)
        }
        item := map[string]any{
            "name":        strings.TrimSpace(source.Name),
            "type":        source.Type,
            "order_index": index,
            "enabled":     true,
            "config":      json.RawMessage(normalized),
        }
        sources = append(sources, item)
    }
    payload["sources"] = sources
    raw, err := json.Marshal(payload)
    if err != nil {
        return nil, err
    }
    return datatypes.JSON(raw), nil
}
