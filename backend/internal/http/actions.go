package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type rebootRequest struct {
	Confirm string `json:"confirm"`
}

type actionPlanResponse struct {
	Action string   `json:"action"`
	NodeID string   `json:"node_id"`
	Steps  []string `json:"steps"`
}

type actionRunRequest struct {
	Confirm string `json:"confirm"`
}

func (h *Handler) RestartXray(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	panel, err := h.newPanelClient(node)
	if err != nil {
		msg := "failed to init panel client"
		h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, errString(err))
		respondError(c, http.StatusInternalServerError, "PANEL_CLIENT", "failed to init panel client")
		return
	}
	if err := panel.Login(); err != nil {
		msg := "panel login failed"
		h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, errString(err))
		respondError(c, http.StatusBadGateway, "PANEL_LOGIN", "panel login failed")
		return
	}
	resp, err := panel.RestartXray()
	if err != nil {
		msg := "failed to restart xray"
		h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, nil)
		respondError(c, http.StatusBadGateway, "XRAY_RESTART", msg)
		return
	}
	h.auditEvent(c, &node.ID, "XRAY_RESTART", "ok", nil, gin.H{}, nil)
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) RebootServer(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req rebootRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if !validateConfirm(req.Confirm) {
		msg := "confirm must be REBOOT"
		h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, nil)
		respondError(c, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm must be REBOOT")
		return
	}
	key, err := h.decryptSSHKey(node)
	if err != nil {
		msg := "failed to decrypt ssh key"
		h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, errString(err))
		respondError(c, http.StatusInternalServerError, "DEC_FAIL", "failed to decrypt ssh key")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.SSHClient.Reboot(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key); err != nil {
		msg := "reboot failed"
		h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, errString(err))
		respondError(c, http.StatusBadGateway, "SERVER_REBOOT", msg)
		return
	}
	h.auditEvent(c, &node.ID, "SERVER_REBOOT", "ok", nil, gin.H{}, nil)
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func normalizeAction(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
}

func (h *Handler) PlanNodeAction(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	action := normalizeAction(c.Param("action"))
	var steps []string
	switch action {
	case "restart_xray":
		steps = []string{
			"Will call 3x-ui panel API: restartXrayService",
		}
	case "reboot":
		steps = []string{
			"Will run via SSH: sudo /sbin/reboot",
		}
	case "delete_node":
		steps = []string{
			"Will delete node record",
			"Will delete related services, checks, check results, alerts, and audit logs",
		}
	default:
		respondError(c, http.StatusBadRequest, "INVALID_ACTION", "unknown action")
		return
	}
	respondStatus(c, http.StatusOK, actionPlanResponse{
		Action: action,
		NodeID: node.ID.String(),
		Steps:  steps,
	})
}

func (h *Handler) RunNodeAction(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	action := normalizeAction(c.Param("action"))
	var req actionRunRequest
	_ = c.ShouldBindJSON(&req)
	switch action {
	case "restart_xray":
		panel, err := h.newPanelClient(node)
		if err != nil {
			msg := "failed to init panel client"
			h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusInternalServerError, "PANEL_CLIENT", msg)
			return
		}
		if err := panel.Login(); err != nil {
			msg := "panel login failed"
			h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusBadGateway, "PANEL_LOGIN", msg)
			return
		}
		resp, err := panel.RestartXray()
		if err != nil {
			msg := "failed to restart xray"
			h.auditEvent(c, &node.ID, "XRAY_RESTART", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusBadGateway, "XRAY_RESTART", msg)
			return
		}
		h.auditEvent(c, &node.ID, "XRAY_RESTART", "ok", nil, gin.H{}, nil)
		respondStatus(c, http.StatusOK, resp)
	case "reboot":
		if !validateConfirm(req.Confirm) {
			msg := "confirm must be REBOOT"
			h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, nil)
			respondError(c, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm must be REBOOT")
			return
		}
		key, err := h.decryptSSHKey(node)
		if err != nil {
			msg := "failed to decrypt ssh key"
			h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusInternalServerError, "DEC_FAIL", msg)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.SSHClient.Reboot(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key); err != nil {
			msg := "reboot failed"
			h.auditEvent(c, &node.ID, "SERVER_REBOOT", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusBadGateway, "SERVER_REBOOT", msg)
			return
		}
		h.auditEvent(c, &node.ID, "SERVER_REBOOT", "ok", nil, gin.H{}, nil)
		respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
	case "delete_node":
		if strings.TrimSpace(req.Confirm) != "DELETE" {
			msg := "confirm must be DELETE"
			h.auditEvent(c, &node.ID, "NODE_DELETE", "error", &msg, gin.H{}, nil)
			respondError(c, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm must be DELETE")
			return
		}
		if err := h.deleteNodeRecords(c.Request.Context(), node); err != nil {
			msg := "failed to delete node"
			h.auditEvent(c, &node.ID, "NODE_DELETE", "error", &msg, gin.H{"name": node.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "DB_DELETE", msg)
			return
		}
		h.auditEvent(c, &node.ID, "NODE_DELETE", "ok", nil, gin.H{"name": node.Name}, nil)
		respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
	default:
		respondError(c, http.StatusBadRequest, "INVALID_ACTION", "unknown action")
	}
}
