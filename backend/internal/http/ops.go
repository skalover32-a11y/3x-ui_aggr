package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/services/ops"
)

type createOpsJobRequest struct {
	Type        string         `json:"type"`
	NodeIDs     []string       `json:"node_ids"`
	All         bool           `json:"all"`
	Parallelism int            `json:"parallelism"`
	Params      map[string]any `json:"params"`
}

type createDeployAgentRequest struct {
	NodeIDs     []string       `json:"node_ids"`
	All         bool           `json:"all"`
	Parallelism int            `json:"parallelism"`
	Params      map[string]any `json:"params"`
}

type publicJobResponse struct {
	Job   *opsJobPublic `json:"job"`
	Items []any         `json:"items"`
}

type opsJobPublic struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Error      *string `json:"error"`
	CreatedAt  any     `json:"created_at"`
	StartedAt  any     `json:"started_at"`
	FinishedAt any     `json:"finished_at"`
}

func (h *Handler) CreateOpsJob(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createOpsJobRequest
	if !parseJSONBody(c, &req) {
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        strings.TrimSpace(req.Type),
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "JOB_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) CreateDeployAgent(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createDeployAgentRequest
	if !parseJSONBody(c, &req) {
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        ops.JobTypeDeploy,
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "JOB_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) CreateInstallVLFProto(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createDeployAgentRequest
	if !parseJSONBody(c, &req) {
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        ops.JobTypeInstallVLF,
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "JOB_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) CreateRemnaGeodataInstall(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createDeployAgentRequest
	if !parseJSONBody(c, &req) {
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        ops.JobTypeRemnaInstall,
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "JOB_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) CreateRemnaGeodataRun(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createDeployAgentRequest
	if !parseJSONBody(c, &req) {
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        ops.JobTypeRemnaRun,
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "JOB_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) GetOpsJob(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	job, err := h.Ops.GetJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	if summary, err := h.Ops.GetJobSummary(c.Request.Context(), c.Param("id")); err == nil {
		job.Summary = summary
	}
	respondStatus(c, http.StatusOK, job)
}

func (h *Handler) GetOpsJobItems(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	if _, err := h.Ops.GetJob(c.Request.Context(), c.Param("id")); err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	items, err := h.Ops.ListJobItems(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	respondStatus(c, http.StatusOK, items)
}

func (h *Handler) GetOpsJobPublic(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	token := strings.TrimSpace(c.GetHeader("X-Job-Token"))
	if token == "" {
		token = strings.TrimSpace(c.Query("token"))
	}
	if token == "" {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing job token")
		return
	}
	job, err := h.Ops.GetJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	if job.PublicTokenHash == nil || *job.PublicTokenHash == "" {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "job token not configured")
		return
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	if hash != *job.PublicTokenHash {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid job token")
		return
	}
	items, err := h.Ops.ListJobItems(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	respJob := &opsJobPublic{
		ID:         job.ID.String(),
		Type:       job.Type,
		Status:     job.Status,
		Error:      job.Error,
		CreatedAt:  job.CreatedAt,
		StartedAt:  job.StartedAt,
		FinishedAt: job.FinishedAt,
	}
	respItems := make([]any, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, item)
	}
	respondStatus(c, http.StatusOK, publicJobResponse{
		Job:   respJob,
		Items: respItems,
	})
}
