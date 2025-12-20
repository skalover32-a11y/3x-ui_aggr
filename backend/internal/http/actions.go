package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type rebootRequest struct {
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
		respondError(c, http.StatusInternalServerError, "PANEL_CLIENT", "failed to init panel client")
		return
	}
	if err := panel.Login(); err != nil {
		respondError(c, http.StatusBadGateway, "PANEL_LOGIN", "panel login failed")
		return
	}
	resp, err := panel.RestartXray()
	if err != nil {
		msg := "failed to restart xray"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "XRAY_RESTART", gin.H{}, "error", &msg)
		respondError(c, http.StatusBadGateway, "XRAY_RESTART", msg)
		return
	}
	h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "XRAY_RESTART", gin.H{}, "ok", nil)
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
		respondError(c, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm must be REBOOT")
		return
	}
	key, err := h.decryptSSHKey(node)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DEC_FAIL", "failed to decrypt ssh key")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.SSHClient.Reboot(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key); err != nil {
		msg := "reboot failed"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "SERVER_REBOOT", gin.H{}, "error", &msg)
		respondError(c, http.StatusBadGateway, "SERVER_REBOOT", msg)
		return
	}
	h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "SERVER_REBOOT", gin.H{}, "ok", nil)
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}
