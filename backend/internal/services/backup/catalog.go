package backup

import (
    "context"
    "encoding/json"
    "strings"
    "time"

    "github.com/google/uuid"

    "agr_3x_ui/internal/db"
)

type CatalogEntry struct {
    Name  string         `json:"name"`
    Label string         `json:"label"`
    Extra map[string]any `json:"extra,omitempty"`
}

func (s *Service) CommonPaths() []CatalogEntry {
    return []CatalogEntry{
        {Name: "/etc/nginx", Label: "/etc/nginx"},
        {Name: "/etc/nginx/sites-available", Label: "/etc/nginx/sites-available"},
        {Name: "/etc/nginx/sites-enabled", Label: "/etc/nginx/sites-enabled"},
        {Name: "/etc/letsencrypt", Label: "/etc/letsencrypt"},
        {Name: "/root/.acme.sh", Label: "/root/.acme.sh"},
        {Name: "/opt/remnawave", Label: "/opt/remnawave"},
        {Name: "/opt/3x-ui_aggr", Label: "/opt/3x-ui_aggr"},
        {Name: "/opt/vaultwarden", Label: "/opt/vaultwarden"},
        {Name: "/var/www", Label: "/var/www"},
        {Name: "/srv/www", Label: "/srv/www"},
    }
}

func (s *Service) ListDockerContainers(ctx context.Context, orgID, nodeID uuid.UUID) ([]CatalogEntry, error) {
    lines, err := s.inspectRemoteCatalog(ctx, orgID, nodeID, `docker ps -a --format '{{json .}}'`)
    if err != nil {
        return nil, err
    }
    items := make([]CatalogEntry, 0, len(lines))
    for _, line := range lines {
        var row map[string]any
        if err := json.Unmarshal([]byte(line), &row); err != nil {
            continue
        }
        name, _ := row["Names"].(string)
        image, _ := row["Image"].(string)
        items = append(items, CatalogEntry{Name: name, Label: name, Extra: map[string]any{"image": image, "raw": row}})
    }
    return items, nil
}

func (s *Service) ListDockerVolumes(ctx context.Context, orgID, nodeID uuid.UUID) ([]CatalogEntry, error) {
    lines, err := s.inspectRemoteCatalog(ctx, orgID, nodeID, `docker volume ls --format '{{json .}}'`)
    if err != nil {
        return nil, err
    }
    items := make([]CatalogEntry, 0, len(lines))
    for _, line := range lines {
        var row map[string]any
        if err := json.Unmarshal([]byte(line), &row); err != nil {
            continue
        }
        name, _ := row["Name"].(string)
        driver, _ := row["Driver"].(string)
        items = append(items, CatalogEntry{Name: name, Label: name, Extra: map[string]any{"driver": driver, "raw": row}})
    }
    return items, nil
}

func (s *Service) ListPostgresContainers(ctx context.Context, orgID, nodeID uuid.UUID) ([]CatalogEntry, error) {
    containers, err := s.ListDockerContainers(ctx, orgID, nodeID)
    if err != nil {
        return nil, err
    }
    filtered := make([]CatalogEntry, 0)
    for _, item := range containers {
        image, _ := item.Extra["image"].(string)
        if strings.Contains(strings.ToLower(image), "postgres") {
            filtered = append(filtered, item)
        }
    }
    return filtered, nil
}

func (s *Service) DetectSystemSources(ctx context.Context, orgID, nodeID uuid.UUID) ([]CatalogEntry, error) {
    lines, err := s.inspectRemoteCatalog(ctx, orgID, nodeID, `for p in /etc/nginx /etc/letsencrypt /root/.acme.sh /opt/remnawave /opt/3x-ui_aggr /opt/vaultwarden /var/www /srv/www; do [ -e "$p" ] && echo "$p"; done`)
    if err != nil {
        return nil, err
    }
    items := make([]CatalogEntry, 0, len(lines))
    for _, line := range lines {
        items = append(items, CatalogEntry{Name: line, Label: line})
    }
    return items, nil
}

func (s *Service) inspectRemoteCatalog(ctx context.Context, orgID, nodeID uuid.UUID, command string) ([]string, error) {
    var node db.Node
    if err := s.DB.WithContext(ctx).Where("id = ? AND org_id = ?", nodeID, orgID).First(&node).Error; err != nil {
        return nil, err
    }
    remote, err := s.openRemoteClient(&node)
    if err != nil {
        return nil, err
    }
    defer remote.Close()
    output, _, err := runRemote(ctx, remote.ssh, command)
    if err != nil {
        return nil, err
    }
    lines := make([]string, 0)
    for _, line := range strings.Split(output, "\n") {
        line = strings.TrimSpace(line)
        if line != "" {
            lines = append(lines, line)
        }
    }
    return lines, nil
}

func (s *Service) TestStorageTarget(ctx context.Context, orgID, targetID uuid.UUID) (*db.BackupStorageTarget, error) {
    target, cfg, err := s.GetStorageTarget(ctx, orgID, targetID)
    if err != nil {
        return nil, err
    }
    uploader, err := s.newUploader(cfg, target.Type)
    if err != nil {
        return nil, err
    }
    testErr := uploader.Test(ctx)
    now := time.Now().UTC()
    target.LastTestedAt = &now
    if testErr != nil {
        status := StatusFailed
        target.LastTestStatus = &status
        msg := strings.TrimSpace(testErr.Error())
        target.LastTestError = &msg
    } else {
        status := StatusSuccess
        target.LastTestStatus = &status
        target.LastTestError = nil
    }
    target.UpdatedAt = now
    if saveErr := s.DB.WithContext(ctx).Save(target).Error; saveErr != nil {
        return nil, saveErr
    }
    return target, testErr
}
