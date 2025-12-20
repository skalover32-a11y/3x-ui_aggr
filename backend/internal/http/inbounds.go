package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/utils"
)

func (h *Handler) ListInbounds(c *gin.Context) {
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
	data, err := panel.ListInbounds()
	if err != nil {
		respondError(c, http.StatusBadGateway, "INBOUNDS_LIST", "failed to list inbounds")
		return
	}
	respondStatus(c, http.StatusOK, data)
}

func (h *Handler) AddInbound(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var payload map[string]any
	if !parseJSONBody(c, &payload) {
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
	resp, err := panel.AddInbound(payload)
	if err != nil {
		msg := "failed to add inbound"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_ADD", payload, "error", &msg)
		respondError(c, http.StatusBadGateway, "INBOUND_ADD", "failed to add inbound")
		return
	}
	h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_ADD", payload, "ok", nil)
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) UpdateInbound(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var patch map[string]any
	if !parseJSONBody(c, &patch) {
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
	inbounds, err := panel.ListInbounds()
	if err != nil {
		respondError(c, http.StatusBadGateway, "INBOUNDS_LIST", "failed to list inbounds")
		return
	}
	current, err := findInboundByID(inbounds, c.Param("inboundId"))
	if err != nil {
		respondError(c, http.StatusNotFound, "INBOUND_NOT_FOUND", "inbound not found")
		return
	}
	prepareMerge(current, patch, "settings")
	prepareMerge(current, patch, "streamSettings")
	merged := utils.MergeMaps(current, patch)
	resp, err := panel.UpdateInbound(c.Param("inboundId"), merged)
	if err != nil {
		msg := "failed to update inbound"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_UPDATE", patch, "error", &msg)
		respondError(c, http.StatusBadGateway, "INBOUND_UPDATE", "failed to update inbound")
		return
	}
	h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_UPDATE", patch, "ok", nil)
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) DeleteInbound(c *gin.Context) {
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
	resp, err := panel.DeleteInbound(c.Param("inboundId"))
	if err != nil {
		msg := "failed to delete inbound"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_DELETE", gin.H{"id": c.Param("inboundId")}, "error", &msg)
		respondError(c, http.StatusBadGateway, "INBOUND_DELETE", "failed to delete inbound")
		return
	}
	h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_DELETE", gin.H{"id": c.Param("inboundId")}, "ok", nil)
	respondStatus(c, http.StatusOK, resp)
}

func findInboundByID(listResp map[string]any, idStr string) (map[string]any, error) {
	obj, ok := listResp["obj"]
	if !ok {
		return nil, fmt.Errorf("missing obj")
	}
	arr, ok := obj.([]any)
	if !ok {
		return nil, fmt.Errorf("obj not array")
	}
	idNum, _ := strconv.Atoi(idStr)
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch v := entry["id"].(type) {
		case float64:
			if int(v) == idNum {
				return entry, nil
			}
		case string:
			if v == idStr {
				return entry, nil
			}
		}
	}
	return nil, fmt.Errorf("not found")
}

func prepareMerge(existing map[string]any, patch map[string]any, key string) {
	patchVal, ok := patch[key]
	if !ok {
		return
	}
	patchMap, ok := patchVal.(map[string]any)
	if !ok {
		return
	}
	if existingStr, ok := existing[key].(string); ok && existingStr != "" {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(existingStr), &decoded); err == nil {
			existing[key] = decoded
		}
	}
	if existingMap, ok := existing[key].(map[string]any); ok {
		existing[key] = utils.MergeMaps(existingMap, patchMap)
		delete(patch, key)
	}
}
