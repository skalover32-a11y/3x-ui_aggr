package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
	if err := validateInboundPayload(payload); err != nil {
		respondError(c, http.StatusBadRequest, "INBOUND_VALIDATE", err.Error())
		return
	}
	resp, err := panel.AddInbound(payload)
	if err != nil {
		msg := "failed to add inbound"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_ADD", payload, "error", &msg)
		respondError(c, http.StatusBadGateway, "INBOUND_ADD", err.Error())
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
	if err := validateInboundPayload(merged); err != nil {
		respondError(c, http.StatusBadRequest, "INBOUND_VALIDATE", err.Error())
		return
	}
	resp, err := panel.UpdateInbound(c.Param("inboundId"), merged)
	if err != nil {
		msg := "failed to update inbound"
		h.Audit.Write(c.Request.Context(), getActor(c), &node.ID, "INBOUND_UPDATE", patch, "error", &msg)
		respondError(c, http.StatusBadGateway, "INBOUND_UPDATE", err.Error())
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
		respondError(c, http.StatusBadGateway, "INBOUND_DELETE", err.Error())
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

var shortIDRe = regexp.MustCompile(`^[0-9a-fA-F]+$`)

func validateInboundPayload(payload map[string]any) error {
	if payload == nil {
		return errors.New("empty payload")
	}
	if port, ok := payload["port"]; ok {
		p, err := asInt(port)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid port")
		}
	}

	settings := extractMap(payload["settings"])
	if settings != nil {
		if clients, ok := settings["clients"].([]any); ok {
			for _, raw := range clients {
				client, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, ok := client["id"].(string); ok && id != "" {
					if _, err := uuid.Parse(id); err != nil {
						return fmt.Errorf("invalid client uuid")
					}
				}
			}
		}
	}

	stream := extractMap(payload["streamSettings"])
	if stream != nil {
		if security, ok := stream["security"].(string); ok && security == "reality" {
			if reality := extractMap(stream["realitySettings"]); reality != nil {
				if dest, ok := reality["dest"].(string); ok && dest != "" {
					if _, _, err := net.SplitHostPort(dest); err != nil {
						return fmt.Errorf("invalid reality dest")
					}
				}
				if shortIDs, ok := reality["shortIds"].([]any); ok {
					for _, raw := range shortIDs {
						val, ok := raw.(string)
						if !ok || val == "" {
							continue
						}
						if !shortIDRe.MatchString(val) {
							return fmt.Errorf("invalid reality shortId")
						}
					}
				}
			}
		}
	}
	return nil
}

func extractMap(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case string:
		var decoded map[string]any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
	}
	return nil
}

func asInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("invalid number")
	}
}
