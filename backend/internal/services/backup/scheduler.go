package backup

import (
    "context"
    "log"
    "time"

    "github.com/google/uuid"
    "github.com/robfig/cron/v3"

    "agr_3x_ui/internal/db"
)

func (s *Service) schedulerLoop(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-s.stop:
            return
        case <-ticker.C:
            if err := s.enqueueScheduledRuns(ctx); err != nil {
                log.Printf("backup scheduler error: %v", err)
            }
        }
    }
}

func (s *Service) enqueueScheduledRuns(ctx context.Context) error {
    if s == nil || s.DB == nil {
        return nil
    }
    jobs, err := s.listEnabledJobs(ctx)
    if err != nil {
        return err
    }
    now := time.Now().UTC()
    for _, job := range jobs {
        due, err := jobDueAt(job, now)
        if err != nil || !due {
            continue
        }
        if running := s.isJobRunning(ctx, job.ID); running {
            continue
        }
        lastMinute := now.Truncate(time.Minute)
        if job.LastRunAt != nil && !job.LastRunAt.Before(lastMinute) {
            continue
        }
        if _, err := s.TriggerRun(ctx, job.OrgID, job.ID, TriggerScheduled, nil); err != nil {
            log.Printf("backup scheduled run failed to enqueue job=%s: %v", job.ID, err)
        }
    }
    return nil
}

func (s *Service) listEnabledJobs(ctx context.Context) ([]db.BackupJob, error) {
    var rows []db.BackupJob
    err := s.DB.WithContext(ctx).Where("enabled = true").Find(&rows).Error
    return rows, err
}

func (s *Service) isJobRunning(ctx context.Context, jobID uuid.UUID) bool {
    var count int64
    if err := s.DB.WithContext(ctx).Model(&db.BackupRun{}).Where("job_id = ? AND status IN ?", jobID, []string{StatusQueued, StatusRunning}).Count(&count).Error; err != nil {
        return false
    }
    return count > 0
}

func jobDueAt(job db.BackupJob, now time.Time) (bool, error) {
    schedule, err := cron.ParseStandard(job.CronExpression)
    if err != nil {
        return false, err
    }
    location, err := time.LoadLocation(NormalizeTimezone(job.Timezone))
    if err != nil {
        location = time.UTC
    }
    localNow := now.In(location)
    lastMinute := localNow.Truncate(time.Minute)
    previous := lastMinute.Add(-time.Minute)
    next := schedule.Next(previous)
    return !next.After(lastMinute), nil
}
