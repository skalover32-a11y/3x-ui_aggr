package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/services/ops"
)

type createTaskRequest struct {
	Type        string         `json:"type"`
	NodeIDs     []string       `json:"node_ids"`
	All         bool           `json:"all"`
	Parallelism int            `json:"parallelism"`
	Params      map[string]any `json:"params"`
}

func (h *Handler) CreateTaskBulk(c *gin.Context) {
	if h.Ops == nil {
		respondError(c, http.StatusServiceUnavailable, "OPS_DISABLED", "ops service not configured")
		return
	}
	var req createTaskRequest
	if !parseJSONBody(c, &req) {
		return
	}
	jobType, err := mapTaskType(req.Type)
	if err != nil {
		respondError(c, http.StatusBadRequest, "TASK_TYPE", err.Error())
		return
	}
	job, err := h.Ops.CreateJob(c.Request.Context(), ops.CreateJobRequest{
		Type:        jobType,
		NodeIDs:     req.NodeIDs,
		AllNodes:    req.All,
		Parallelism: req.Parallelism,
		Params:      req.Params,
		Actor:       getActor(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, "TASK_CREATE", err.Error())
		return
	}
	respondStatus(c, http.StatusCreated, job)
}

func (h *Handler) GetTask(c *gin.Context) {
	h.GetOpsJob(c)
}

func (h *Handler) GetTaskItems(c *gin.Context) {
	h.GetOpsJobItems(c)
}

func mapTaskType(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "update_panel":
		return ops.JobTypeUpdatePanel, nil
	case "reboot_node":
		return ops.JobTypeRebootAgent, nil
	case "restart_service":
		return ops.JobTypeRestartSvc, nil
	default:
		return "", errors.New("unsupported task type")
	}
}
