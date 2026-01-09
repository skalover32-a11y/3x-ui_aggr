package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agr_3x_ui/internal/db"
)

const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobSuccess = "success"
	JobFailed  = "failed"
)

const (
	JobTypeReboot = "reboot_nodes"
	JobTypeUpdate = "update_xui_nodes"
)

type Service struct {
	DB       *gorm.DB
	Executor NodeExecutor
	stop     chan struct{}
	stopOnce sync.Once
}

type CreateJobRequest struct {
	Type        string
	NodeIDs     []string
	AllNodes    bool
	Parallelism int
	Params      map[string]any
	Actor       string
}

func New(dbConn *gorm.DB, exec NodeExecutor) *Service {
	return &Service{
		DB:       dbConn,
		Executor: exec,
		stop:     make(chan struct{}),
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.DB == nil || s.Executor == nil {
		return
	}
	go s.loop(ctx)
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

func (s *Service) CreateJob(ctx context.Context, req CreateJobRequest) (*db.OpsJob, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("db not configured")
	}
	typ := strings.TrimSpace(req.Type)
	if typ == "" {
		return nil, errors.New("type required")
	}
	if typ != JobTypeReboot && typ != JobTypeUpdate {
		return nil, errors.New("unsupported job type")
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "admin"
	}
	parallelism := req.Parallelism
	if parallelism <= 0 {
		parallelism = 5
	}
	nodeIDs, err := s.resolveNodeIDs(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(nodeIDs) == 0 {
		return nil, errors.New("no nodes selected")
	}
	targetsPayload, _ := json.Marshal(nodeIDs)
	paramsPayload, _ := json.Marshal(req.Params)

	job := db.OpsJob{
		Type:           typ,
		Status:         JobQueued,
		CreatedByActor: actor,
		Parallelism:    parallelism,
		Targets:        targetsPayload,
		Params:         paramsPayload,
	}

	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		items := make([]db.OpsJobItem, 0, len(nodeIDs))
		for _, id := range nodeIDs {
			items = append(items, db.OpsJobItem{
				JobID:  job.ID,
				NodeID: id,
				Status: JobQueued,
				Log:    "",
			})
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) GetJob(ctx context.Context, id string) (*db.OpsJob, error) {
	var job db.OpsJob
	if err := s.DB.WithContext(ctx).First(&job, "id::text = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) ListJobItems(ctx context.Context, id string) ([]db.OpsJobItem, error) {
	var items []db.OpsJobItem
	if err := s.DB.WithContext(ctx).Where("job_id::text = ?", id).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.pickAndRun(ctx)
		}
	}
}

func (s *Service) pickAndRun(ctx context.Context) {
	tx := s.DB.WithContext(ctx).Begin()
	var job db.OpsJob
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ?", JobQueued).
		Order("created_at").
		First(&job).Error
	if err != nil {
		_ = tx.Rollback()
		return
	}
	now := time.Now()
	job.Status = JobRunning
	job.StartedAt = &now
	if err := tx.Save(&job).Error; err != nil {
		_ = tx.Rollback()
		return
	}
	if err := tx.Commit().Error; err != nil {
		return
	}
	s.runJob(ctx, &job)
}

func (s *Service) runJob(ctx context.Context, job *db.OpsJob) {
	if job == nil {
		return
	}
	var items []db.OpsJobItem
	if err := s.DB.WithContext(ctx).Where("job_id = ? AND status = ?", job.ID, JobQueued).Find(&items).Error; err != nil {
		return
	}
	if len(items) == 0 {
		finished := time.Now()
		job.Status = JobSuccess
		job.FinishedAt = &finished
		_ = s.DB.WithContext(ctx).Save(job).Error
		return
	}
	parallelism := job.Parallelism
	if parallelism <= 0 {
		parallelism = 5
	}
	ch := make(chan db.OpsJobItem)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range ch {
				if err := s.executeItem(ctx, job, &item); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}
		}()
	}
	for _, item := range items {
		ch <- item
	}
	close(ch)
	wg.Wait()
	finished := time.Now()
	if failed > 0 {
		msg := fmt.Sprintf("%d items failed", failed)
		job.Status = JobFailed
		job.Error = &msg
	} else {
		job.Status = JobSuccess
		job.Error = nil
	}
	job.FinishedAt = &finished
	_ = s.DB.WithContext(ctx).Save(job).Error
}

func (s *Service) executeItem(ctx context.Context, job *db.OpsJob, item *db.OpsJobItem) error {
	if job == nil || item == nil {
		return errors.New("missing job")
	}
	started := time.Now()
	updates := map[string]any{
		"status":     JobRunning,
		"started_at": started,
	}
	_ = s.DB.WithContext(ctx).Model(&db.OpsJobItem{}).Where("id = ?", item.ID).Updates(updates).Error

	node, err := s.loadNode(ctx, item.NodeID)
	if err != nil {
		s.finishItem(ctx, item.ID, JobFailed, "", 1, err)
		return err
	}
	timeout := 2 * time.Minute
	if job.Type == JobTypeUpdate {
		timeout = 15 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var output string
	var runErr error
	exitCode := 0
	switch job.Type {
	case JobTypeReboot:
		output, exitCode, runErr = s.Executor.Reboot(cctx, node)
	case JobTypeUpdate:
		params := parseUpdateParams(job.Params)
		output, exitCode, runErr = s.Executor.Update(cctx, node, params)
	default:
		runErr = errors.New("unsupported job type")
		exitCode = 1
	}
	output = trimLog(output, 4096, 16384)
	if runErr != nil {
		s.finishItem(ctx, item.ID, JobFailed, output, exitCode, runErr)
		return runErr
	}
	s.finishItem(ctx, item.ID, JobSuccess, output, exitCode, nil)
	return nil
}

func (s *Service) finishItem(ctx context.Context, id uuid.UUID, status, logText string, exitCode int, err error) {
	finished := time.Now()
	updates := map[string]any{
		"status":      status,
		"log":         logText,
		"finished_at": finished,
		"exit_code":   exitCode,
	}
	if err != nil {
		msg := err.Error()
		updates["error"] = msg
	}
	_ = s.DB.WithContext(ctx).Model(&db.OpsJobItem{}).Where("id = ?", id).Updates(updates).Error
}

func (s *Service) resolveNodeIDs(ctx context.Context, req CreateJobRequest) ([]uuid.UUID, error) {
	if req.AllNodes || len(req.NodeIDs) == 0 {
		var nodes []db.Node
		if err := s.DB.WithContext(ctx).Find(&nodes).Error; err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, 0, len(nodes))
		for _, node := range nodes {
			ids = append(ids, node.ID)
		}
		return ids, nil
	}
	ids := make([]uuid.UUID, 0, len(req.NodeIDs))
	for _, raw := range req.NodeIDs {
		val, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid node id: %s", raw)
		}
		ids = append(ids, val)
	}
	return ids, nil
}

func (s *Service) loadNode(ctx context.Context, id uuid.UUID) (*db.Node, error) {
	var node db.Node
	if err := s.DB.WithContext(ctx).First(&node, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func parseUpdateParams(raw datatypes.JSON) UpdateParams {
	var params UpdateParams
	_ = json.Unmarshal(raw, &params)
	return params
}

func trimLog(input string, headSize int, tailSize int) string {
	if headSize <= 0 && tailSize <= 0 {
		return ""
	}
	if len(input) <= headSize+tailSize || tailSize == 0 {
		if len(input) > headSize && headSize > 0 {
			return input[:headSize]
		}
		return input
	}
	head := input[:headSize]
	tail := input[len(input)-tailSize:]
	return head + "\n...trimmed...\n" + tail
}
