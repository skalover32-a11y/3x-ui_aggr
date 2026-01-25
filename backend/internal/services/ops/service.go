package ops

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobSuccess = "success"
	JobFailed  = "failed"
)

const (
	JobTypeReboot      = "reboot_nodes"
	JobTypeUpdate      = "update_xui_nodes"
	JobTypeDeploy      = "deploy_agent"
	JobTypeUpdatePanel = "update_panel"
	JobTypeRebootAgent = "reboot_node"
	JobTypeRestartSvc  = "restart_service"
)

const maxParallelism = 10
const agentDesiredVersion = "v1.7"

type Service struct {
	DB            *gorm.DB
	Executor      NodeExecutor
	AgentExecutor NodeExecutor
	Encryptor     *security.Encryptor
	SudoPasswords []string
	AllowCIDR     string
	RepoPath      string
	AgentBinary   string
	Hub           *Hub
	stop          chan struct{}
	stopOnce      sync.Once
}

type CreateJobRequest struct {
	Type        string
	NodeIDs     []string
	AllNodes    bool
	Parallelism int
	Params      map[string]any
	Actor       string
}

type JobParams struct {
	DryRun           bool   `json:"dry_run"`
	SimulateDelayMs  int    `json:"simulate_delay_ms"`
	Confirm          string `json:"confirm"`
	Sandbox          bool   `json:"sandbox"`
	ForceRedeploy    bool   `json:"force_redeploy"`
	PrecheckOnly     bool   `json:"precheck_only"`
	InstallExpect    bool   `json:"install_expect"`
	AgentPort        int    `json:"agent_port"`
	AgentTokenMode   string `json:"agent_token_mode"`
	SharedAgentToken string `json:"shared_agent_token"`
	AllowCIDR        string `json:"allow_cidr"`
	StatsMode        string `json:"stats_mode"`
	XrayAccessLog    string `json:"xray_access_log_path"`
	RateLimitRPS     int    `json:"rate_limit_rps"`
	EnableUFW        bool   `json:"enable_ufw"`
	HealthCheck      bool   `json:"health_check"`
	InstallDocker    bool   `json:"install_docker"`
	RestartService   string `json:"restart_service"`
}

func New(dbConn *gorm.DB, exec NodeExecutor, agentExec NodeExecutor, enc *security.Encryptor, sudoPasswords []string, allowCIDR string, repoPath string) *Service {
	if repoPath == "" {
		repoPath = "/opt/3x-ui_aggr"
	}
	return &Service{
		DB:            dbConn,
		Executor:      exec,
		AgentExecutor: agentExec,
		Encryptor:     enc,
		SudoPasswords: sudoPasswords,
		AllowCIDR:     allowCIDR,
		RepoPath:      repoPath,
		AgentBinary:   "/app/bin/vlf-agent",
		Hub:           NewHub(),
		stop:          make(chan struct{}),
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
	if !isSupportedJobType(typ) {
		return nil, errors.New("unsupported job type")
	}
	if isAgentJobType(typ) && s.AgentExecutor == nil {
		return nil, errors.New("agent executor not configured")
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "admin"
	}
	jobParams := parseJobParamsFromMap(req.Params)
	parallelism := req.Parallelism
	if parallelism <= 0 {
		parallelism = 5
	}
	if parallelism > maxParallelism {
		parallelism = maxParallelism
	}
	if req.AllNodes && !jobParams.DryRun {
		if typ == JobTypeReboot || typ == JobTypeUpdate || typ == JobTypeUpdatePanel || typ == JobTypeRebootAgent {
			if strings.TrimSpace(jobParams.Confirm) != "REALLY_DO_IT" {
				return nil, errors.New("confirmation required")
			}
		}
		if typ == JobTypeDeploy {
			if strings.TrimSpace(jobParams.Confirm) != "DEPLOY_AGENT" {
				return nil, errors.New("confirmation required")
			}
		}
	}
	nodeIDs, err := s.resolveNodeIDs(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(nodeIDs) == 0 {
		return nil, errors.New("no nodes selected")
	}
	if jobParams.Sandbox {
		if err := s.ensureSandboxTargets(ctx, nodeIDs); err != nil {
			return nil, err
		}
	}
	if isAgentJobType(typ) {
		if err := s.ensureAgentTargets(ctx, nodeIDs); err != nil {
			return nil, err
		}
	}
	if typ == JobTypeRestartSvc {
		if err := validateRestartService(jobParams.RestartService); err != nil {
			return nil, err
		}
	}
	targetsPayload, _ := json.Marshal(nodeIDs)
	paramsPayload, _ := json.Marshal(req.Params)

	publicToken, publicTokenHash, err := generatePublicToken()
	if err != nil {
		return nil, err
	}
	job := db.OpsJob{
		Type:            typ,
		Status:          JobQueued,
		CreatedByActor:  actor,
		Parallelism:     parallelism,
		Targets:         targetsPayload,
		Params:          paramsPayload,
		PublicTokenHash: &publicTokenHash,
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
	job.PublicToken = &publicToken
	return &job, nil
}

func (s *Service) Subscribe(jobID string) (<-chan Event, func()) {
	if s == nil || s.Hub == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	return s.Hub.Subscribe(jobID)
}

func (s *Service) GetJob(ctx context.Context, id string) (*db.OpsJob, error) {
	var job db.OpsJob
	if err := s.DB.WithContext(ctx).First(&job, "id::text = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Service) GetJobSummary(ctx context.Context, id string) (*db.OpsJobSummary, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("db not configured")
	}
	var rows []struct {
		Status string
		Count  int
	}
	if err := s.DB.WithContext(ctx).
		Model(&db.OpsJobItem{}).
		Select("status, count(*) as count").
		Where("job_id::text = ?", id).
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	summary := &db.OpsJobSummary{}
	for _, row := range rows {
		switch row.Status {
		case JobQueued:
			summary.Queued = row.Count
		case JobRunning:
			summary.Running = row.Count
		case JobSuccess:
			summary.Success = row.Count
		case JobFailed:
			summary.Failed = row.Count
		}
		summary.Total += row.Count
	}
	return summary, nil
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
	s.publishJobStatus(&job)
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
		s.publishJobStatus(job)
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
	status, errMsg := jobStatusFromFailures(failed)
	job.Status = status
	job.Error = errMsg
	job.FinishedAt = &finished
	_ = s.DB.WithContext(ctx).Save(job).Error
	s.publishJobStatus(job)
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
	s.publishItemStatus(job.ID, item, JobRunning, &started, nil)

	jobParams := parseJobParams(job.Params)
	if jobParams.DryRun {
		logText := fmt.Sprintf("DRY_RUN: would execute %s on node %s", describeJobAction(job.Type, jobParams), item.NodeID)
		delay := dryRunDelay(jobParams.SimulateDelayMs)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobFailed, logText, 1, &started, ctx.Err())
			return ctx.Err()
		case <-timer.C:
		}
		s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobSuccess, logText, 0, &started, nil)
		return nil
	}

	node, err := s.loadNode(ctx, item.NodeID)
	if err != nil {
		s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobFailed, "", 1, &started, err)
		return err
	}
	if job.Type == JobTypeUpdatePanel {
		if !node.AgentEnabled || !node.AgentInstalled {
			logText := "SKIPPED: agent not installed"
			s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobSuccess, logText, 0, &started, nil)
			return nil
		}
	}
	if job.Type == JobTypeDeploy {
		params := parseJobParams(job.Params)
		if node.AgentInstalled && !params.ForceRedeploy && !params.InstallDocker {
			logText := "ALREADY_INSTALLED: agent already installed"
			s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobSuccess, logText, 0, &started, nil)
			return nil
		}
		if node.AgentInstalled && params.ForceRedeploy {
			current := ""
			if node.AgentVersion != nil {
				current = strings.TrimSpace(*node.AgentVersion)
			}
			if current != "" && current == agentDesiredVersion && !params.InstallDocker {
				logText := fmt.Sprintf("ALREADY_INSTALLED: agent version matches (%s)", current)
				s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobSuccess, logText, 0, &started, nil)
				return nil
			}
		}
	}
	timeout := 2 * time.Minute
	if job.Type == JobTypeUpdate || job.Type == JobTypeUpdatePanel {
		timeout = 15 * time.Minute
	}
	if job.Type == JobTypeRebootAgent {
		timeout = 5 * time.Minute
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
	case JobTypeUpdatePanel:
		output, exitCode, runErr = s.AgentExecutor.Update(cctx, node, UpdateParams{})
	case JobTypeRebootAgent:
		jobParams := parseJobParams(job.Params)
		rebootCtx := withRebootConfirm(cctx, jobParams.Confirm)
		output, exitCode, runErr = s.AgentExecutor.Reboot(rebootCtx, node)
	case JobTypeRestartSvc:
		params := parseJobParams(job.Params)
		output, exitCode, runErr = s.AgentExecutor.RestartService(cctx, node, params.RestartService)
	case JobTypeDeploy:
		params, err := s.buildDeployParams(ctx, node, job.Params)
		if err != nil {
			runErr = err
			exitCode = 1
			break
		}
		output, exitCode, runErr = s.Executor.DeployAgent(cctx, node, params)
		if params.PreLog != "" {
			if output != "" {
				output = params.PreLog + "\n" + output
			} else {
				output = params.PreLog
			}
		}
		if runErr == nil {
			if err := s.persistAgentSettings(ctx, node, params); err != nil {
				runErr = err
				exitCode = 1
			}
		}
		if runErr == nil && params.HealthCheck {
			info, err := s.waitForAgentFirstContact(cctx, job.ID, item.ID, item.NodeID, params)
			if info != "" {
				if output != "" {
					output += "\n" + info
				} else {
					output = info
				}
			}
			if err != nil {
				runErr = err
				exitCode = 1
			}
		}
	default:
		runErr = errors.New("unsupported job type")
		exitCode = 1
	}
	panelVersion := ""
	if job.Type == JobTypeUpdatePanel {
		panelVersion = parsePanelVersionFromLog(output)
		if runErr == nil && panelVersion != "" {
			if err := s.persistPanelVersion(ctx, node.ID, panelVersion); err != nil {
				output = appendPreLog(output, fmt.Sprintf("panel_version update failed: %v", err))
			} else {
				output = appendPreLog(output, fmt.Sprintf("panel_version stored: %s", panelVersion))
			}
		}
		if runErr != nil {
			log.Printf("update_panel job=%s node=%s status=failed err=%v", job.ID, item.NodeID, runErr)
		} else {
			log.Printf("update_panel job=%s node=%s status=success version=%s", job.ID, item.NodeID, panelVersion)
		}
	}
	output = trimLog(output, 4096, 16384)
	if runErr != nil {
		if strings.TrimSpace(output) == "" {
			output = runErr.Error()
		}
		s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobFailed, output, exitCode, &started, runErr)
		return runErr
	}
	s.finishItem(ctx, job.ID, item.ID, item.NodeID, JobSuccess, output, exitCode, &started, nil)
	return nil
}

func (s *Service) finishItem(ctx context.Context, jobID uuid.UUID, id uuid.UUID, nodeID uuid.UUID, status, logText string, exitCode int, startedAt *time.Time, err error) {
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
	if logText != "" {
		s.publishItemLog(jobID, id, nodeID, logText)
	}
	s.publishItemStatus(jobID, &db.OpsJobItem{ID: id, NodeID: nodeID}, status, startedAt, &finished)
	s.publishItemDone(jobID, id, nodeID, status, exitCode, err)
}

func (s *Service) resolveNodeIDs(ctx context.Context, req CreateJobRequest) ([]uuid.UUID, error) {
	if req.AllNodes {
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
	if len(req.NodeIDs) == 0 {
		return nil, errors.New("node_ids required when all=false")
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
	jobParams := parseJobParams(raw)
	return UpdateParams{
		PrecheckOnly:  jobParams.PrecheckOnly,
		InstallExpect: jobParams.InstallExpect,
	}
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

func parsePanelVersionFromLog(logText string) string {
	if strings.TrimSpace(logText) == "" {
		return ""
	}
	lines := strings.Split(logText, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "panel_version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "panel_version:"))
		}
		if strings.HasPrefix(line, "panel_version=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "panel_version="))
		}
	}
	return ""
}

func parseJobParams(raw datatypes.JSON) JobParams {
	var params JobParams
	_ = json.Unmarshal(raw, &params)
	return params
}

func parseJobParamsFromMap(raw map[string]any) JobParams {
	if len(raw) == 0 {
		return JobParams{}
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return JobParams{}
	}
	return parseJobParams(payload)
}

func describeJobAction(jobType string, params JobParams) string {
	switch jobType {
	case JobTypeReboot:
		return "reboot (sudo /sbin/reboot)"
	case JobTypeUpdate:
		if params.PrecheckOnly {
			return "update_xui_nodes precheck"
		}
		return "update_xui_nodes (expect x-ui)"
	case JobTypeUpdatePanel:
		return "update panel (agent)"
	case JobTypeRebootAgent:
		return "reboot (agent)"
	case JobTypeRestartSvc:
		return fmt.Sprintf("restart service %s (agent)", strings.TrimSpace(params.RestartService))
	default:
		return jobType
	}
}

func dryRunDelay(ms int) time.Duration {
	if ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	rng := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	return time.Duration(100+rng.Intn(201)) * time.Millisecond
}

func (s *Service) buildDeployParams(ctx context.Context, node *db.Node, raw datatypes.JSON) (DeployAgentParams, error) {
	if node == nil {
		return DeployAgentParams{}, errors.New("node missing")
	}
	params := parseJobParams(raw)
	rawMap := map[string]any{}
	_ = json.Unmarshal(raw, &rawMap)

	agentPort := params.AgentPort
	if agentPort <= 0 {
		agentPort = 9191
	}
	rawAllowCIDR := strings.TrimSpace(params.AllowCIDR)
	if rawAllowCIDR == "" {
		rawAllowCIDR = strings.TrimSpace(s.AllowCIDR)
	}
	allowCIDRs := normalizeAllowCIDRs(rawAllowCIDR)
	if len(allowCIDRs) == 0 {
		return DeployAgentParams{}, errors.New("allow_cidr is required")
	}
	allowCIDR := allowCIDRs[0]
	if len(s.SudoPasswords) == 0 {
		return DeployAgentParams{}, errors.New("sudo passwords not configured")
	}

	statsMode := strings.TrimSpace(params.StatsMode)
	if statsMode == "" {
		statsMode = "log"
	}
	xrayPath := strings.TrimSpace(params.XrayAccessLog)
	if xrayPath == "" {
		xrayPath = "/var/log/xray/access.log"
	}
	rateLimit := params.RateLimitRPS
	if rateLimit <= 0 {
		rateLimit = 5
	}
	healthCheck := true
	if _, ok := rawMap["health_check"]; ok {
		healthCheck = params.HealthCheck
	}
	enableUFW := false
	if _, ok := rawMap["enable_ufw"]; ok {
		enableUFW = params.EnableUFW
	}

	tokenMode := strings.TrimSpace(params.AgentTokenMode)
	if tokenMode == "" {
		tokenMode = "per-node"
	}
	var token string
	switch tokenMode {
	case "per-node":
		gen, err := generateToken()
		if err != nil {
			return DeployAgentParams{}, err
		}
		token = gen
	case "shared":
		token = strings.TrimSpace(params.SharedAgentToken)
		if token == "" {
			return DeployAgentParams{}, errors.New("shared_agent_token required for shared mode")
		}
	default:
		return DeployAgentParams{}, fmt.Errorf("unsupported agent_token_mode: %s", tokenMode)
	}

	binaryPath, preLog, err := s.ensureAgentBinary()
	if err != nil {
		return DeployAgentParams{}, err
	}
	serviceContent, servicePath, err := s.loadAgentServiceTemplate()
	if err != nil {
		return DeployAgentParams{}, err
	}
	nodeHost := strings.TrimSpace(node.SSHHost)
	if nodeHost == "" {
		nodeHost = strings.TrimSpace(node.Host)
	}
	allowCIDRHost := strings.Split(allowCIDR, "/")[0]
	if nodeHost != "" && allowCIDRHost != "" && strings.EqualFold(nodeHost, allowCIDRHost) {
		const dockerBridgeCIDR = "172.17.0.0/16"
		if !cidrInList(allowCIDRs, dockerBridgeCIDR) {
			allowCIDRs = append(allowCIDRs, dockerBridgeCIDR)
		}
	}

	configContent := buildAgentConfig(agentPort, token, allowCIDRs, statsMode, xrayPath, rateLimit)
	preLog = appendPreLog(preLog, fmt.Sprintf("service template: %s", servicePath))
	preLog = appendPreLog(preLog, fmt.Sprintf("allow_cidrs: %s", strings.Join(allowCIDRs, ", ")))

	return DeployAgentParams{
		BinaryPath:     binaryPath,
		ServiceContent: serviceContent,
		ConfigContent:  configContent,
		AgentPort:      agentPort,
		AllowCIDR:      allowCIDR,
		Token:          token,
		EnableUFW:      enableUFW,
		HealthCheck:    healthCheck,
		InstallDocker:  params.InstallDocker,
		SudoPasswords:  s.SudoPasswords,
		NodeHost:       nodeHost,
		PreLog:         preLog,
	}, nil
}

func (s *Service) persistAgentSettings(ctx context.Context, node *db.Node, params DeployAgentParams) error {
	if s == nil || s.DB == nil {
		return errors.New("db not configured")
	}
	if node == nil {
		return errors.New("node missing")
	}
	if s.Encryptor == nil {
		return errors.New("encryptor missing")
	}
	if params.Token == "" {
		return errors.New("agent token missing")
	}
	encToken, err := s.Encryptor.EncryptString(params.Token)
	if err != nil {
		return err
	}
	host := strings.TrimSpace(params.NodeHost)
	if host == "" {
		host = strings.TrimSpace(node.SSHHost)
	}
	if host == "" {
		host = strings.TrimSpace(node.Host)
	}
	if host == "" {
		return errors.New("node host missing for agent_url")
	}
	url := fmt.Sprintf("http://%s:%d", host, params.AgentPort)
	updates := map[string]any{
		"agent_enabled":      true,
		"agent_url":          url,
		"agent_token_enc":    encToken,
		"agent_insecure_tls": false,
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", node.ID).Updates(updates).Error
}

func (s *Service) persistAgentContact(ctx context.Context, nodeID uuid.UUID, lastSeen time.Time, agentVersion string) error {
	if s == nil || s.DB == nil {
		return errors.New("db not configured")
	}
	updates := map[string]any{
		"agent_installed":    true,
		"agent_last_seen_at": lastSeen,
	}
	if strings.TrimSpace(agentVersion) != "" {
		updates["agent_version"] = strings.TrimSpace(agentVersion)
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Updates(updates).Error
}

func (s *Service) persistPanelVersion(ctx context.Context, nodeID uuid.UUID, version string) error {
	if s == nil || s.DB == nil {
		return errors.New("db not configured")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"panel_version":       version,
		"versions_checked_at": now,
	}
	if err := s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Updates(updates).Error; err != nil {
		return err
	}
	row := db.NodeMetricsLatest{
		NodeID:       nodeID,
		CollectedAt:  now,
		PanelVersion: &version,
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"panel_version": version,
			"collected_at":  now,
		}),
	}).Create(&row).Error
}

func (s *Service) ensureAgentBinary() (string, string, error) {
	if s == nil {
		return "", "", errors.New("service not configured")
	}
	path := strings.TrimSpace(s.AgentBinary)
	if path == "" {
		path = "/app/bin/vlf-agent"
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("agent binary not found in container at %s; rebuild backend image", path)
	}
	if info.Mode()&0111 == 0 {
		return "", "", fmt.Errorf("agent binary not executable at %s; rebuild backend image", path)
	}
	hash, err := hashFile(path)
	if err != nil {
		return "", "", fmt.Errorf("agent binary unreadable at %s: %w", path, err)
	}
	preLog := fmt.Sprintf("agent binary: %s size=%d sha256=%s", path, info.Size(), hash)
	return path, preLog, nil
}

func (s *Service) loadAgentServiceTemplate() ([]byte, string, error) {
	repoPath := strings.TrimSpace(s.RepoPath)
	if repoPath == "" {
		repoPath = "/opt/3x-ui_aggr"
	}
	paths := []string{
		filepath.Join("/app", "deploy", "agent", "vlf-agent.service"),
		filepath.Join(repoPath, "deploy", "agent", "vlf-agent.service"),
	}
	var data []byte
	var err error
	var usedPath string
	for _, path := range paths {
		data, err = os.ReadFile(path)
		if err == nil {
			usedPath = path
			break
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("read service template: %w", err)
	}
	return data, usedPath, nil
}

func buildAgentConfig(port int, token string, allowCIDRs []string, statsMode string, accessLog string, rateLimit int) []byte {
	lines := []string{
		fmt.Sprintf("listen: \"0.0.0.0:%d\"", port),
		fmt.Sprintf("token: %q", escapeYAMLString(token)),
		"allow_cidrs:",
	}
	for _, cidr := range allowCIDRs {
		lines = append(lines, fmt.Sprintf("  - %q", escapeYAMLString(cidr)))
	}
	lines = append(lines,
		fmt.Sprintf("xray_access_log_path: %q", escapeYAMLString(accessLog)),
		fmt.Sprintf("poll_window_seconds: %d", 60),
		fmt.Sprintf("stats_mode: %q", escapeYAMLString(statsMode)),
		fmt.Sprintf("rate_limit_rps: %d", rateLimit),
	)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(buf), nil
}

func generatePublicToken() (string, string, error) {
	token, err := generateToken()
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func hashPublicToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func escapeYAMLString(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func normalizeAllowCIDR(raw string) string {
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		trim := strings.TrimSpace(part)
		if trim != "" {
			return trim
		}
	}
	return ""
}

func normalizeAllowCIDRs(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trim := strings.TrimSpace(part)
		if trim == "" {
			continue
		}
		if !strings.Contains(trim, "/") {
			trim = trim + "/32"
		}
		if seen[trim] {
			continue
		}
		seen[trim] = true
		out = append(out, trim)
	}
	return out
}

func cidrInList(list []string, cidr string) bool {
	for _, item := range list {
		if item == cidr {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func appendPreLog(existing, entry string) string {
	if entry == "" {
		return existing
	}
	if existing == "" {
		return entry
	}
	return existing + "\n" + entry
}

type agentHealth struct {
	OK           bool   `json:"ok"`
	AgentVersion string `json:"agent_version"`
	BootID       string `json:"boot_id"`
}

func (s *Service) waitForAgentFirstContact(ctx context.Context, jobID uuid.UUID, itemID uuid.UUID, nodeID uuid.UUID, params DeployAgentParams) (string, error) {
	if params.AgentPort <= 0 {
		return "waiting_first_contact skipped: missing agent port", nil
	}
	host := strings.TrimSpace(params.NodeHost)
	if host == "" {
		return "", errors.New("agent health check missing node host")
	}
	url := fmt.Sprintf("http://%s:%d/health", host, params.AgentPort)
	s.publishItemStage(jobID, itemID, nodeID, "waiting_first_contact")
	info := fmt.Sprintf("waiting_first_contact url=%s auth=%t", url, strings.TrimSpace(params.Token) != "")

	deadline := time.Now().Add(60 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		if time.Now().After(deadline) {
			return info + " status=timeout", fmt.Errorf("agent first contact timeout")
		}
		status, resp, err := s.checkAgentHealth(ctx, url, params.Token)
		if err == nil && status == http.StatusOK && resp.OK {
			_ = s.persistAgentContact(ctx, nodeID, time.Now().UTC(), resp.AgentVersion)
			return info + " status=ok", nil
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return info + fmt.Sprintf(" status=%d", status), fmt.Errorf("agent health unauthorized: status %d", status)
		}
		select {
		case <-ctx.Done():
			return info + " status=cancelled", ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) checkAgentHealth(ctx context.Context, url string, token string) (int, agentHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, agentHealth{}, err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, agentHealth{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, agentHealth{}, nil
	}
	var payload agentHealth
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return resp.StatusCode, agentHealth{}, err
	}
	return resp.StatusCode, payload, nil
}

func (s *Service) ensureSandboxTargets(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return errors.New("no nodes selected")
	}
	var nodes []db.Node
	if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Find(&nodes).Error; err != nil {
		return err
	}
	nonSandbox := 0
	for _, node := range nodes {
		if !node.IsSandbox {
			nonSandbox++
		}
	}
	if nonSandbox > 0 {
		return fmt.Errorf("sandbox mode: %d non-sandbox nodes in targets", nonSandbox)
	}
	return nil
}

func (s *Service) ensureAgentTargets(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return errors.New("no nodes selected")
	}
	var nodes []db.Node
	if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Find(&nodes).Error; err != nil {
		return err
	}
	missing := 0
	for _, node := range nodes {
		if !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
			missing++
		}
	}
	if missing > 0 {
		return fmt.Errorf("agent not configured for %d nodes", missing)
	}
	return nil
}

func isSupportedJobType(typ string) bool {
	switch typ {
	case JobTypeReboot, JobTypeUpdate, JobTypeDeploy, JobTypeUpdatePanel, JobTypeRebootAgent, JobTypeRestartSvc:
		return true
	default:
		return false
	}
}

func isAgentJobType(typ string) bool {
	return typ == JobTypeUpdatePanel || typ == JobTypeRebootAgent || typ == JobTypeRestartSvc
}

func validateRestartService(service string) error {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "3x-ui", "x-ui", "xray", "sing-box", "docker", "adguard", "agent":
		return nil
	default:
		return fmt.Errorf("unsupported service: %s", service)
	}
}

func (s *Service) publishJobStatus(job *db.OpsJob) {
	if s == nil || s.Hub == nil || job == nil {
		return
	}
	data := map[string]any{
		"status":      job.Status,
		"started_at":  formatTimePtr(job.StartedAt),
		"finished_at": formatTimePtr(job.FinishedAt),
	}
	s.Hub.Publish(job.ID.String(), newEvent(job.ID.String(), EventJobStatus, data))
}

func (s *Service) publishItemStatus(jobID uuid.UUID, item *db.OpsJobItem, status string, startedAt *time.Time, finishedAt *time.Time) {
	if s == nil || s.Hub == nil || item == nil {
		return
	}
	data := map[string]any{
		"node_id":     item.NodeID.String(),
		"item_id":     item.ID.String(),
		"status":      status,
		"started_at":  formatTimePtr(startedAt),
		"finished_at": formatTimePtr(finishedAt),
	}
	s.Hub.Publish(jobID.String(), newEvent(jobID.String(), EventItemStatus, data))
}

func (s *Service) publishItemStage(jobID uuid.UUID, itemID uuid.UUID, nodeID uuid.UUID, stage string) {
	if s == nil || s.Hub == nil || strings.TrimSpace(stage) == "" {
		return
	}
	data := map[string]any{
		"node_id": nodeID.String(),
		"item_id": itemID.String(),
		"status":  JobRunning,
		"stage":   stage,
	}
	s.Hub.Publish(jobID.String(), newEvent(jobID.String(), EventItemStatus, data))
}

func (s *Service) publishItemLog(jobID uuid.UUID, itemID uuid.UUID, nodeID uuid.UUID, chunk string) {
	if s == nil || s.Hub == nil {
		return
	}
	data := map[string]any{
		"node_id": nodeID.String(),
		"item_id": itemID.String(),
		"chunk":   chunk,
	}
	s.Hub.Publish(jobID.String(), newEvent(jobID.String(), EventItemLogAppend, data))
}

func (s *Service) publishItemDone(jobID uuid.UUID, itemID uuid.UUID, nodeID uuid.UUID, status string, exitCode int, err error) {
	if s == nil || s.Hub == nil {
		return
	}
	var errMsg any = nil
	if err != nil {
		errMsg = err.Error()
	}
	data := map[string]any{
		"node_id":   nodeID.String(),
		"item_id":   itemID.String(),
		"status":    status,
		"exit_code": exitCode,
		"error":     errMsg,
	}
	s.Hub.Publish(jobID.String(), newEvent(jobID.String(), EventItemDone, data))
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func jobStatusFromFailures(failed int) (string, *string) {
	if failed > 0 {
		msg := fmt.Sprintf("%d items failed", failed)
		return JobFailed, &msg
	}
	return JobSuccess, nil
}
