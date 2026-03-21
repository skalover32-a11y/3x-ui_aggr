package backup

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
    "gorm.io/datatypes"
    "gorm.io/gorm"

    "agr_3x_ui/internal/db"
    "agr_3x_ui/internal/services/backup/storage"
)

type artifactFile struct {
    ItemID      uuid.UUID
    ItemType    string
    LogicalName string
    LocalPath   string
    ObjectName  string
    Size        int64
    Checksum    string
}

type runLogger struct {
    mu      sync.Mutex
    file    *os.File
    tail    []string
    maxTail int
}

func newRunLogger(logPath string) (*runLogger, error) {
    file, err := os.Create(logPath)
    if err != nil {
        return nil, err
    }
    return &runLogger{file: file, maxTail: 80}, nil
}

func (l *runLogger) Close() error {
    if l == nil || l.file == nil {
        return nil
    }
    return l.file.Close()
}

func (l *runLogger) Writef(format string, args ...any) {
    if l == nil {
        return
    }
    line := fmt.Sprintf("%s %s", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
    l.mu.Lock()
    defer l.mu.Unlock()
    _, _ = l.file.WriteString(line + "\n")
    l.tail = append(l.tail, line)
    if len(l.tail) > l.maxTail {
        l.tail = l.tail[len(l.tail)-l.maxTail:]
    }
}

func (l *runLogger) Excerpt() string {
    if l == nil {
        return ""
    }
    l.mu.Lock()
    defer l.mu.Unlock()
    return strings.Join(l.tail, "\n")
}

func (s *Service) TriggerRun(ctx context.Context, orgID, jobID uuid.UUID, triggerType string, initiatedBy *uuid.UUID) (*db.BackupRun, error) {
    job, _, err := s.GetJob(ctx, orgID, jobID)
    if err != nil {
        return nil, err
    }
    if job == nil {
        return nil, gorm.ErrRecordNotFound
    }
    if s.isJobRunning(ctx, jobID) {
        return nil, errors.New("job already has a queued or running backup")
    }
    run := &db.BackupRun{
        ID:                uuid.New(),
        OrgID:             orgID,
        JobID:             jobID,
        Status:            StatusQueued,
        TriggerType:       triggerType,
        InitiatedByUserID: initiatedBy,
        ChecksumStatus:    ChecksumPending,
        CleanupStatus:     CleanupPending,
        StartedAt:         time.Now().UTC(),
        CreatedAt:         time.Now().UTC(),
    }
    if err := s.DB.WithContext(ctx).Create(run).Error; err != nil {
        return nil, err
    }
    go s.runAsync(run.ID)
    return run, nil
}

func (s *Service) RetryRun(ctx context.Context, orgID, runID uuid.UUID, initiatedBy *uuid.UUID) (*db.BackupRun, error) {
    run, _, err := s.GetRun(ctx, orgID, runID)
    if err != nil {
        return nil, err
    }
    return s.TriggerRun(ctx, orgID, run.JobID, TriggerRetry, initiatedBy)
}

func (s *Service) CancelRun(ctx context.Context, orgID, runID uuid.UUID) error {
    run, _, err := s.GetRun(ctx, orgID, runID)
    if err != nil {
        return err
    }
    now := time.Now().UTC()
    run.CancelRequestedAt = &now
    if err := s.DB.WithContext(ctx).Save(run).Error; err != nil {
        return err
    }
    if cancelAny, ok := s.running.Load(run.ID); ok {
        if cancel, ok := cancelAny.(context.CancelFunc); ok {
            cancel()
        }
    }
    return nil
}

func (s *Service) runAsync(runID uuid.UUID) {
    ctx, cancel := context.WithCancel(context.Background())
    s.running.Store(runID, cancel)
    defer func() {
        cancel()
        s.running.Delete(runID)
    }()
    _ = s.executeRun(ctx, runID)
}

func (s *Service) executeRun(ctx context.Context, runID uuid.UUID) error {
    run, job, sources, target, targetCfg, err := s.loadRunContext(ctx, runID)
    if err != nil {
        return err
    }
    workdir, err := s.makeRunWorkdir(run.ID)
    if err != nil {
        return s.finishRunWithError(ctx, run, job, nil, "failed to create local workdir", err)
    }
    logPath := filepath.Join(workdir, "run.log")
    logger, err := newRunLogger(logPath)
    if err != nil {
        return s.finishRunWithError(ctx, run, job, nil, "failed to create run logger", err)
    }
    defer logger.Close()
    localWorkdir := workdir
    run.LocalWorkdir = &localWorkdir
    run.LogPath = &logPath
    run.Status = StatusRunning
    run.StartedAt = time.Now().UTC()
    if err := s.DB.WithContext(ctx).Save(run).Error; err != nil {
        return err
    }
    logger.Writef("run started job=%s trigger=%s", job.Name, run.TriggerType)

    uploader, err := s.newUploader(targetCfg, target.Type)
    if err != nil {
        return s.finishRunWithError(ctx, run, job, logger, "storage adapter initialization failed", err)
    }
    if err := uploader.Test(ctx); err != nil {
        return s.finishRunWithError(ctx, run, job, logger, "storage test failed", err)
    }
    logger.Writef("storage target ready type=%s name=%s", target.Type, target.Name)

    artifacts, execErr := s.collectArtifacts(ctx, run, job, sources, logger)
    if execErr != nil && len(artifacts) == 0 {
        return s.finishRunWithError(ctx, run, job, logger, "backup collection failed", execErr)
    }

    manifestPath, manifestChecksum, manifestSize, manifestErr := s.writeChecksumsManifest(artifacts, workdir)
    if manifestErr == nil {
        manifestItem := db.BackupRunItem{
            ID:             uuid.New(),
            RunID:          run.ID,
            ItemType:       "manifest",
            LogicalName:    "SHA256SUMS",
            OutputFileName: "SHA256SUMS",
            SizeBytes:      manifestSize,
            Status:         StatusSuccess,
            Checksum:       &manifestChecksum,
            ExtraJSON:      datatypesJSON(`{"generated":true}`),
        }
        now := time.Now().UTC()
        manifestItem.StartedAt = &now
        manifestItem.FinishedAt = &now
        _ = s.DB.WithContext(ctx).Create(&manifestItem).Error
        artifacts = append(artifacts, artifactFile{ItemID: manifestItem.ID, ItemType: manifestItem.ItemType, LogicalName: manifestItem.LogicalName, LocalPath: manifestPath, ObjectName: "SHA256SUMS", Size: manifestSize, Checksum: manifestChecksum})
        run.ChecksumStatus = ChecksumDone
    } else {
        logger.Writef("checksum manifest failed: %v", manifestErr)
        run.ChecksumStatus = ChecksumFailed
    }

    remoteDir := path.Join(fmt.Sprintf("org_%s", job.OrgID.String()), fmt.Sprintf("job_%s", job.ID.String()), run.StartedAt.UTC().Format("20060102T150405Z")+"-"+run.ID.String()[:8])
    remotePath := storage.JoinRemote(targetCfg.BasePath, remoteDir)
    run.RemotePath = &remotePath

    uploadErr := s.uploadArtifacts(ctx, uploader, remoteDir, artifacts, logger, run)
    cleanupErr := s.applyRetention(ctx, uploader, job, logger)
    if cleanupErr != nil {
        run.CleanupStatus = CleanupFailed
        logger.Writef("retention cleanup failed: %v", cleanupErr)
    } else {
        run.CleanupStatus = CleanupDone
    }

    finalStatus := deriveFinalStatus(execErr, uploadErr, ctx.Err())
    run.Status = finalStatus
    finishedAt := time.Now().UTC()
    run.FinishedAt = &finishedAt
    run.DurationMS = finishedAt.Sub(run.StartedAt).Milliseconds()
    excerpt := logger.Excerpt()
    run.LogExcerpt = &excerpt
    run.FileCount = len(artifacts)
    if execErr != nil || uploadErr != nil {
        summary := firstNonEmpty(errSummary(execErr), errSummary(uploadErr), errSummary(cleanupErr), errSummary(ctx.Err()))
        if summary != "" {
            run.ErrorSummary = &summary
            job.LastError = &summary
        }
    }
    if run.Status == StatusSuccess {
        job.LastSuccessAt = &finishedAt
        job.LastError = nil
        metricLastSuccess.WithLabelValues(job.ID.String()).Set(float64(finishedAt.Unix()))
    }
    job.LastRunAt = &finishedAt
    job.LastStatus = run.Status
    job.LastSizeBytes = run.TotalSizeBytes
    _ = s.DB.WithContext(ctx).Save(run).Error
    _ = s.DB.WithContext(ctx).Save(job).Error
    metricRunsTotal.WithLabelValues(run.Status).Inc()
    metricRunDuration.Observe(float64(run.DurationMS) / 1000)
    metricUploadedBytes.Add(float64(run.UploadedSizeBytes))
    logger.Writef("run finished status=%s files=%d size=%d uploaded=%d", run.Status, run.FileCount, run.TotalSizeBytes, run.UploadedSizeBytes)
    return firstError(execErr, uploadErr, cleanupErr)
}

func (s *Service) loadRunContext(ctx context.Context, runID uuid.UUID) (*db.BackupRun, *db.BackupJob, []db.BackupSource, *db.BackupStorageTarget, StorageTargetConfig, error) {
    var run db.BackupRun
    if err := s.DB.WithContext(ctx).Where("id = ?", runID).First(&run).Error; err != nil {
        return nil, nil, nil, nil, StorageTargetConfig{}, err
    }
    job, sources, err := s.GetJob(ctx, run.OrgID, run.JobID)
    if err != nil {
        return nil, nil, nil, nil, StorageTargetConfig{}, err
    }
    target, cfg, err := s.GetStorageTarget(ctx, run.OrgID, job.StorageTargetID)
    if err != nil {
        return nil, nil, nil, nil, StorageTargetConfig{}, err
    }
    return &run, job, sources, target, cfg, nil
}

func (s *Service) collectArtifacts(ctx context.Context, run *db.BackupRun, job *db.BackupJob, sources []db.BackupSource, logger *runLogger) ([]artifactFile, error) {
    if job.NodeID == nil || *job.NodeID == uuid.Nil {
        return nil, errors.New("backup jobs currently require node_id for real execution")
    }
    var node db.Node
    if err := s.DB.WithContext(ctx).Where("id = ? AND org_id = ?", *job.NodeID, job.OrgID).First(&node).Error; err != nil {
        return nil, err
    }
    remote, err := s.openRemoteClient(&node)
    if err != nil {
        return nil, err
    }
    defer remote.Close()

    remoteWorkdir := path.Join("/tmp", "agg-backup-"+run.ID.String())
    run.RemoteWorkdir = &remoteWorkdir
    _ = s.DB.WithContext(ctx).Save(run).Error
    if _, _, err := runRemote(ctx, remote.ssh, fmt.Sprintf("mkdir -p %s", shellEscape(remoteWorkdir))); err != nil {
        return nil, err
    }
    defer runRemote(context.Background(), remote.ssh, fmt.Sprintf("rm -rf %s", shellEscape(remoteWorkdir)))

    artifacts := make([]artifactFile, 0, len(sources))
    var firstErr error
    for _, source := range sources {
        if !source.Enabled {
            continue
        }
        select {
        case <-ctx.Done():
            return artifacts, ctx.Err()
        default:
        }
        artifact, err := s.executeSource(ctx, remote, run, job, &source, logger)
        if err != nil && firstErr == nil {
            firstErr = err
        }
        if artifact != nil {
            artifacts = append(artifacts, *artifact)
        }
    }
    return artifacts, firstErr
}

func (s *Service) executeSource(ctx context.Context, remote *remoteClient, run *db.BackupRun, job *db.BackupJob, source *db.BackupSource, logger *runLogger) (*artifactFile, error) {
    now := time.Now().UTC()
    item := db.BackupRunItem{
        ID:            uuid.New(),
        RunID:         run.ID,
        SourceID:      &source.ID,
        ItemType:      source.Type,
        LogicalName:   source.Name,
        OutputFileName: "",
        Status:        StatusRunning,
        StartedAt:     &now,
        ExtraJSON:     datatypesJSON(`{}`),
    }
    if err := s.DB.WithContext(ctx).Create(&item).Error; err != nil {
        return nil, err
    }
    logger.Writef("source started type=%s name=%s", source.Type, source.Name)

    remoteOut, scriptBody, outputName, err := s.renderSourceScript(run, source, remote)
    if err != nil {
        failText := err.Error()
        item.Status = StatusFailed
        item.ErrorText = &failText
        finished := time.Now().UTC()
        item.FinishedAt = &finished
        _ = s.DB.WithContext(ctx).Save(&item).Error
        metricRunItemsTotal.WithLabelValues(StatusFailed).Inc()
        logger.Writef("source failed early name=%s err=%v", source.Name, err)
        return nil, err
    }
    item.OutputFileName = outputName
    item.RemoteSourcePath = &remoteOut
    scriptPath := path.Join(path.Dir(remoteOut), item.ID.String()+".sh")
    if err := uploadBytes(ctx, remote, []byte(scriptBody), scriptPath); err != nil {
        return s.failRunItem(ctx, &item, logger, fmt.Errorf("upload script: %w", err))
    }
    _, _, _ = runRemote(ctx, remote.ssh, fmt.Sprintf("chmod 700 %s", shellEscape(scriptPath)))
    if output, code, err := runRemote(ctx, remote.ssh, fmt.Sprintf("bash %s", shellEscape(scriptPath))); err != nil {
        logger.Writef("source execution failed name=%s code=%d output=%s", source.Name, code, strings.TrimSpace(output))
        return s.failRunItem(ctx, &item, logger, errors.New(strings.TrimSpace(firstNonEmpty(output, err.Error()))))
    }

    localPath := filepath.Join(strings.TrimSpace(*run.LocalWorkdir), outputName)
    if err := downloadFile(ctx, remote, remoteOut, localPath); err != nil {
        return s.failRunItem(ctx, &item, logger, fmt.Errorf("download artifact: %w", err))
    }
    size, err := fileSize(remote, remoteOut)
    if err != nil {
        return s.failRunItem(ctx, &item, logger, fmt.Errorf("remote stat: %w", err))
    }
    checksum, err := calculateFileChecksum(localPath)
    if err != nil {
        return s.failRunItem(ctx, &item, logger, fmt.Errorf("checksum: %w", err))
    }
    item.Status = StatusSuccess
    item.SizeBytes = size
    item.Checksum = &checksum
    finished := time.Now().UTC()
    item.FinishedAt = &finished
    if err := s.DB.WithContext(ctx).Save(&item).Error; err != nil {
        return nil, err
    }
    metricRunItemsTotal.WithLabelValues(StatusSuccess).Inc()
    run.TotalSizeBytes += size
    _ = s.DB.WithContext(ctx).Model(run).Update("total_size_bytes", run.TotalSizeBytes).Error
    logger.Writef("source finished name=%s file=%s size=%d", source.Name, outputName, size)
    return &artifactFile{ItemID: item.ID, ItemType: item.ItemType, LogicalName: item.LogicalName, LocalPath: localPath, ObjectName: outputName, Size: size, Checksum: checksum}, nil
}

func (s *Service) failRunItem(ctx context.Context, item *db.BackupRunItem, logger *runLogger, err error) (*artifactFile, error) {
    if item != nil {
        finished := time.Now().UTC()
        item.Status = StatusFailed
        text := err.Error()
        item.ErrorText = &text
        item.FinishedAt = &finished
        _ = s.DB.WithContext(ctx).Save(item).Error
    }
    metricRunItemsTotal.WithLabelValues(StatusFailed).Inc()
    if logger != nil {
        logger.Writef("source failed name=%s err=%v", item.LogicalName, err)
    }
    return nil, err
}

func (s *Service) renderSourceScript(run *db.BackupRun, source *db.BackupSource, remote *remoteClient) (string, string, string, error) {
    stamp := run.StartedAt.UTC().Format("20060102T150405Z")
    base := MakeArtifactName(source.Name, stamp)
    switch source.Type {
    case SourceFilePath, SourceDirectoryPath:
        var cfg PathSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := base + ".tar.gz"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\nTARGET=%s\nOUT=%s\nif [ ! -e \"$TARGET\" ]; then echo \"missing path: $TARGET\" >&2; exit 1; fi\n%s\n%s\n",
            shellEscape(cfg.Path),
            shellEscape(remoteOut),
            sudoCmd(fmt.Sprintf("tar -czf %s -P %s", shellEscape(remoteOut), shellEscape(cfg.Path)), remote.sudoPass, remote.usePass),
            sudoCmd(fmt.Sprintf("chown %s:%s %s", shellEscape("$USER"), shellEscape("$USER"), shellEscape(remoteOut)), remote.sudoPass, remote.usePass),
        )
        return remoteOut, strings.ReplaceAll(script, "$USER", "$(id -un)"), out, nil
    case SourceDockerVolume:
        var cfg DockerVolumeSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := base + ".tar.gz"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\nOUT=%s\nDIR=%s\nVOL=%s\n%s\n%s\n",
            shellEscape(remoteOut),
            shellEscape(*run.RemoteWorkdir),
            shellEscape(cfg.VolumeName),
            sudoCmd(fmt.Sprintf("docker run --rm -v %s:/source:ro -v %s:/out alpine:3.20 sh -lc %s", shellEscape(cfg.VolumeName), shellEscape(*run.RemoteWorkdir), shellEscape(fmt.Sprintf("tar -C /source -czf /out/%s .", out))), remote.sudoPass, remote.usePass),
            sudoCmd(fmt.Sprintf("chown $(id -un):$(id -gn) %s", shellEscape(remoteOut)), remote.sudoPass, remote.usePass),
        )
        return remoteOut, script, out, nil
    case SourcePostgresContainer:
        var cfg PostgresContainerSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := base + ".sql.gz"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        autoFlag := "0"
        if cfg.AutoDetectFromEnv {
            autoFlag = "1"
        }
        script := fmt.Sprintf(`set -euo pipefail
CONTAINER=%s
OUT=%s
DB_NAME=%s
DB_USER=%s
DB_PASS=%s
AUTO=%s
if [ "$AUTO" = "1" ]; then
  ENVS=$(%s)
  if [ -z "$DB_NAME" ]; then DB_NAME=$(printf '%%s\n' "$ENVS" | awk -F= '$1=="POSTGRES_DB"{print substr($0, index($0,"=")+1); exit}'); fi
  if [ -z "$DB_USER" ]; then DB_USER=$(printf '%%s\n' "$ENVS" | awk -F= '$1=="POSTGRES_USER"{print substr($0, index($0,"=")+1); exit}'); fi
  if [ -z "$DB_PASS" ]; then DB_PASS=$(printf '%%s\n' "$ENVS" | awk -F= '$1=="POSTGRES_PASSWORD"{print substr($0, index($0,"=")+1); exit}'); fi
fi
if [ -z "$DB_NAME" ]; then DB_NAME="$DB_USER"; fi
[ -n "$DB_NAME" ]
[ -n "$DB_USER" ]
%s | gzip -c > "$OUT"
%s
`,
            shellEscape(cfg.ContainerName),
            shellEscape(remoteOut),
            shellEscape(cfg.DBName),
            shellEscape(cfg.DBUser),
            shellEscape(cfg.DBPassword),
            autoFlag,
            sudoCmd(fmt.Sprintf("docker inspect --format {{range .Config.Env}}{{println .}}{{end}} %s", shellEscape(cfg.ContainerName)), remote.sudoPass, remote.usePass),
            sudoCmd(fmt.Sprintf("docker exec -e PGPASSWORD=\"$DB_PASS\" %s pg_dump -U \"$DB_USER\" \"$DB_NAME\" %s", shellEscape(cfg.ContainerName), cfg.DumpArgs), remote.sudoPass, remote.usePass),
            sudoCmd(fmt.Sprintf("chown $(id -un):$(id -gn) %s", shellEscape(remoteOut)), remote.sudoPass, remote.usePass),
        )
        return remoteOut, script, out, nil
    case SourcePostgresManual:
        var cfg PostgresManualSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := base + ".sql.gz"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf(`set -euo pipefail
OUT=%s
PGPASSWORD=%s pg_dump -h %s -p %d -U %s %s %s | gzip -c > "$OUT"
`, shellEscape(remoteOut), shellEscape(cfg.DBPassword), shellEscape(cfg.Host), cfg.Port, shellEscape(cfg.DBUser), shellEscape(cfg.DBName), cfg.DumpArgs)
        return remoteOut, script, out, nil
    case SourceNginxSnapshot:
        var cfg NginxSnapshotSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := MakeArtifactName(cfg.OutputName, stamp) + ".txt"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\n%s > %s\n%s\n", sudoCmd(cfg.Command, remote.sudoPass, remote.usePass), shellEscape(remoteOut), sudoCmd(fmt.Sprintf("chown $(id -un):$(id -gn) %s", shellEscape(remoteOut)), remote.sudoPass, remote.usePass))
        return remoteOut, script, out, nil
    case SourceCronSnapshot:
        var cfg CronSnapshotSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := MakeArtifactName(cfg.OutputName, stamp) + ".txt"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        extra := ""
        if cfg.IncludeSystem {
            extra = fmt.Sprintf("\necho '\n## /etc/crontab'; %s 2>/dev/null || true\necho '\n## /etc/cron.d'; %s 2>/dev/null || true",
                sudoCmd("cat /etc/crontab", remote.sudoPass, remote.usePass),
                sudoCmd("find /etc/cron.d -maxdepth 1 -type f -print -exec cat {} \\;", remote.sudoPass, remote.usePass),
            )
        }
        script := fmt.Sprintf("set -euo pipefail\n{ echo '## user crontab'; crontab -l 2>&1 || true %s\n} > %s\n", extra, shellEscape(remoteOut))
        return remoteOut, script, out, nil
    case SourceDockerInventory:
        var cfg DockerInventorySourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := MakeArtifactName(cfg.OutputName, stamp) + ".txt"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\n{ echo '## docker ps -a'; %s; echo '\n## docker volume ls'; %s; echo '\n## docker network ls'; %s; echo '\n## docker image ls'; %s; echo '\n## docker compose ls'; %s || true; } > %s\n",
            sudoCmd("docker ps -a", remote.sudoPass, remote.usePass),
            sudoCmd("docker volume ls", remote.sudoPass, remote.usePass),
            sudoCmd("docker network ls", remote.sudoPass, remote.usePass),
            sudoCmd("docker image ls", remote.sudoPass, remote.usePass),
            sudoCmd("docker compose ls", remote.sudoPass, remote.usePass),
            shellEscape(remoteOut),
        )
        return remoteOut, script, out, nil
    case SourceSystemSnapshot:
        var cfg SystemSnapshotSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        out := MakeArtifactName(cfg.OutputName, stamp) + ".txt"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\n{ hostnamectl 2>/dev/null || true; echo; uname -a; echo; ip addr 2>/dev/null || true; echo; ss -tulpn 2>/dev/null || true; echo; df -h; echo; free -m; } > %s\n", shellEscape(remoteOut))
        return remoteOut, script, out, nil
    case SourceCustomCommand:
        var cfg CustomCommandSourceConfig
        if err := json.Unmarshal(source.ConfigJSON, &cfg); err != nil {
            return "", "", "", err
        }
        if !s.AllowUnsafeCommands {
            return "", "", "", errors.New("unsafe custom commands are disabled")
        }
        out := MakeArtifactName(cfg.OutputName, stamp) + ".txt"
        remoteOut := path.Join(*run.RemoteWorkdir, out)
        script := fmt.Sprintf("set -euo pipefail\n%s > %s\n", cfg.Command, shellEscape(remoteOut))
        return remoteOut, script, out, nil
    default:
        return "", "", "", fmt.Errorf("unsupported source type: %s", source.Type)
    }
}

func (s *Service) uploadArtifacts(ctx context.Context, uploader storage.Uploader, remoteDir string, artifacts []artifactFile, logger *runLogger, run *db.BackupRun) error {
    var firstErr error
    for _, artifact := range artifacts {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        file, err := os.Open(artifact.LocalPath)
        if err != nil {
            if firstErr == nil {
                firstErr = err
            }
            continue
        }
        err = uploader.Upload(ctx, storage.UploadInput{RemoteDir: remoteDir, ObjectName: artifact.ObjectName, Reader: file, Size: artifact.Size})
        _ = file.Close()
        if err != nil {
            if firstErr == nil {
                firstErr = err
            }
            logger.Writef("upload failed object=%s err=%v", artifact.ObjectName, err)
            continue
        }
        run.UploadedSizeBytes += artifact.Size
        _ = s.DB.WithContext(ctx).Model(run).Update("uploaded_size_bytes", run.UploadedSizeBytes).Error
        logger.Writef("uploaded object=%s size=%d", artifact.ObjectName, artifact.Size)
    }
    return firstErr
}

func (s *Service) applyRetention(ctx context.Context, uploader storage.Uploader, job *db.BackupJob, logger *runLogger) error {
    if job.RetentionDays <= 0 {
        return nil
    }
    cutoff := time.Now().UTC().AddDate(0, 0, -job.RetentionDays)
    prefix := path.Join(fmt.Sprintf("org_%s", job.OrgID.String()), fmt.Sprintf("job_%s", job.ID.String()))
    objects, err := uploader.List(ctx, prefix)
    if err != nil {
        return err
    }
    var firstErr error
    for _, object := range objects {
        if object.ModifiedAt == nil || object.ModifiedAt.After(cutoff) {
            continue
        }
        if err := uploader.Delete(ctx, object.Path); err != nil && firstErr == nil {
            firstErr = err
            logger.Writef("retention delete failed path=%s err=%v", object.Path, err)
            continue
        }
        logger.Writef("retention deleted path=%s", object.Path)
    }
    return firstErr
}

func (s *Service) writeChecksumsManifest(artifacts []artifactFile, workdir string) (string, string, int64, error) {
    manifestPath := filepath.Join(workdir, "SHA256SUMS")
    lines := make([]string, 0, len(artifacts))
    for _, artifact := range artifacts {
        lines = append(lines, fmt.Sprintf("%s  %s", artifact.Checksum, artifact.ObjectName))
    }
    content := strings.Join(lines, "\n") + "\n"
    if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
        return "", "", 0, err
    }
    checksum, err := calculateFileChecksum(manifestPath)
    if err != nil {
        return "", "", 0, err
    }
    stat, err := os.Stat(manifestPath)
    if err != nil {
        return "", "", 0, err
    }
    return manifestPath, checksum, stat.Size(), nil
}

func calculateFileChecksum(path string) (string, error) {
    file, err := os.Open(path)
    if err != nil {
        return "", err
    }
    defer file.Close()
    h := sha256.New()
    if _, err := io.Copy(h, file); err != nil {
        return "", err
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}

func deriveFinalStatus(execErr, uploadErr, cancelErr error) string {
    if cancelErr != nil {
        return StatusCancelled
    }
    if execErr == nil && uploadErr == nil {
        return StatusSuccess
    }
    if execErr != nil && uploadErr != nil {
        return StatusFailed
    }
    return StatusPartialSuccess
}

func (s *Service) finishRunWithError(ctx context.Context, run *db.BackupRun, job *db.BackupJob, logger *runLogger, summary string, err error) error {
    finishedAt := time.Now().UTC()
    run.Status = StatusFailed
    run.FinishedAt = &finishedAt
    run.DurationMS = finishedAt.Sub(run.StartedAt).Milliseconds()
    if summary == "" {
        summary = errSummary(err)
    }
    if summary != "" {
        run.ErrorSummary = &summary
        if job != nil {
            job.LastError = &summary
        }
    }
    if logger != nil {
        logger.Writef("run failed: %s (%v)", summary, err)
        excerpt := logger.Excerpt()
        run.LogExcerpt = &excerpt
    }
    _ = s.DB.WithContext(ctx).Save(run).Error
    if job != nil {
        job.LastRunAt = &finishedAt
        job.LastStatus = run.Status
        _ = s.DB.WithContext(ctx).Save(job).Error
    }
    metricRunsTotal.WithLabelValues(run.Status).Inc()
    return err
}

func errSummary(err error) string {
    if err == nil {
        return ""
    }
    return strings.TrimSpace(err.Error())
}

func firstNonEmpty(values ...string) string {
    for _, value := range values {
        trimmed := strings.TrimSpace(value)
        if trimmed != "" {
            return trimmed
        }
    }
    return ""
}

func firstError(errs ...error) error {
    for _, err := range errs {
        if err != nil {
            return err
        }
    }
    return nil
}

func datatypesJSON(raw string) datatypes.JSON {
    return datatypes.JSON([]byte(raw))
}
