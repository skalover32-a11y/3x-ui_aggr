package httpapi

import (
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
