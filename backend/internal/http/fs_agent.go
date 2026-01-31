package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

type fsListResponse struct {
	OK      bool          `json:"ok"`
	Path    string        `json:"path"`
	Entries []fileEntryFS `json:"entries"`
}

type fileEntryFS struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	Modified time.Time `json:"modified"`
	UID      int       `json:"uid,omitempty"`
	GID      int       `json:"gid,omitempty"`
}

type fsReadData struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

type fsReadResponse struct {
	OK   bool       `json:"ok"`
	Data fsReadData `json:"data"`
}

type fsWriteRequest struct {
	Content string  `json:"content"`
	Mode    *string `json:"mode,omitempty"`
}

type fsPathRequest struct {
	Path string `json:"path"`
}

type fsRenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type fsDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func (h *Handler) FSList(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		pathValue = "/"
	}
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodGet, "/fs/list?path="+url.QueryEscape(pathValue), nil, "")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "list", pathValue, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "list", pathValue, false, map[string]any{"status": resp.StatusCode})
		return
	}
	var payload fsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", "invalid agent response")
		return
	}
	h.fsAudit(c, node, "list", pathValue, true, nil)
	respondStatus(c, http.StatusOK, payload.Entries)
}

func (h *Handler) FSStat(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodGet, "/fs/stat?path="+url.QueryEscape(pathValue), nil, "")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "stat", pathValue, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "stat", pathValue, false, map[string]any{"status": resp.StatusCode})
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", "invalid agent response")
		return
	}
	h.fsAudit(c, node, "stat", pathValue, true, nil)
	respondStatus(c, http.StatusOK, payload)
}

func (h *Handler) FSRead(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodGet, "/fs/read?path="+url.QueryEscape(pathValue), nil, "")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "read", pathValue, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "read", pathValue, false, map[string]any{"status": resp.StatusCode})
		return
	}
	var payload fsReadResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", "invalid agent response")
		return
	}
	h.fsAudit(c, node, "read", pathValue, true, map[string]any{"bytes": payload.Data.Size})
	respondStatus(c, http.StatusOK, payload.Data)
}

func (h *Handler) FSWrite(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	if !canFSWriteRole(c.GetString("role")) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	var req fsWriteRequest
	if !parseJSONBody(c, &req) {
		return
	}
	payload, _ := json.Marshal(req)
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodPut, "/fs/write?path="+url.QueryEscape(pathValue), bytes.NewReader(payload), "application/json")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "write", pathValue, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "write", pathValue, false, map[string]any{"status": resp.StatusCode})
		return
	}
	h.fsAudit(c, node, "write", pathValue, true, map[string]any{"bytes": len(req.Content)})
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) FSMkdir(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	if !canFSWriteRole(c.GetString("role")) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		return
	}
	var req fsPathRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	payload, _ := json.Marshal(req)
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodPost, "/fs/mkdir", bytes.NewReader(payload), "application/json")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "mkdir", req.Path, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "mkdir", req.Path, false, map[string]any{"status": resp.StatusCode})
		return
	}
	h.fsAudit(c, node, "mkdir", req.Path, true, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) FSRename(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	if !canFSWriteRole(c.GetString("role")) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		return
	}
	var req fsRenameRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "from/to required")
		return
	}
	payload, _ := json.Marshal(req)
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodPost, "/fs/rename", bytes.NewReader(payload), "application/json")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "rename", req.From, false, map[string]any{"error": err.Error(), "to": req.To})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "rename", req.From, false, map[string]any{"status": resp.StatusCode, "to": req.To})
		return
	}
	h.fsAudit(c, node, "rename", req.From, true, map[string]any{"to": req.To})
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) FSDelete(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	if !canFSWriteRole(c.GetString("role")) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		return
	}
	var req fsDeleteRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	payload, _ := json.Marshal(req)
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodPost, "/fs/delete", bytes.NewReader(payload), "application/json")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "delete", req.Path, false, map[string]any{"error": err.Error(), "recursive": req.Recursive})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "delete", req.Path, false, map[string]any{"status": resp.StatusCode, "recursive": req.Recursive})
		return
	}
	h.fsAudit(c, node, "delete", req.Path, true, map[string]any{"recursive": req.Recursive})
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) FSUpload(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	if !canFSWriteRole(c.GetString("role")) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "file required")
		return
	}
	src, err := file.Open()
	if err != nil {
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "failed to open file")
		return
	}
	defer src.Close()
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		part, err := writer.CreateFormFile("file", file.Filename)
		if err != nil {
			_ = writer.Close()
			return
		}
		_, _ = io.Copy(part, src)
		_ = writer.Close()
	}()
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodPost, "/fs/upload?path="+url.QueryEscape(pathValue), pr, writer.FormDataContentType())
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "upload", pathValue, false, map[string]any{"error": err.Error(), "filename": file.Filename})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "upload", pathValue, false, map[string]any{"status": resp.StatusCode, "filename": file.Filename})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = body
	h.fsAudit(c, node, "upload", pathValue, true, map[string]any{"filename": file.Filename, "size": file.Size})
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) FSDownload(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	pathValue := strings.TrimSpace(c.Query("path"))
	if pathValue == "" {
		respondError(c, http.StatusBadRequest, "PATH_REQUIRED", "path is required")
		return
	}
	resp, err := h.agentDo(c.Request.Context(), node, http.MethodGet, "/fs/download?path="+url.QueryEscape(pathValue), nil, "")
	if err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		h.fsAudit(c, node, "download", pathValue, false, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.proxyAgentError(c, resp)
		h.fsAudit(c, node, "download", pathValue, false, map[string]any{"status": resp.StatusCode})
		return
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		c.Header("Content-Disposition", cd)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resp.Body)
	h.fsAudit(c, node, "download", pathValue, true, nil)
}

func canFSWriteRole(role string) bool {
	return role == middleware.RoleAdmin
}

func (h *Handler) agentDo(ctx context.Context, node *db.Node, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	agentURL, token, transport, err := h.agentTransport(node)
	if err != nil {
		return nil, err
	}
	full := strings.TrimRight(agentURL.String(), "/") + path
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: transport, Timeout: 0}
	return client.Do(req)
}

func (h *Handler) proxyAgentError(c *gin.Context, resp *http.Response) {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if len(raw) == 0 {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", "agent error")
		return
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), raw)
}

func respondAgentError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "not configured") {
		respondError(c, http.StatusBadRequest, "AGENT_CONFIG", msg)
		return
	}
	respondError(c, http.StatusBadGateway, "AGENT_ERROR", msg)
}

func (h *Handler) fsAudit(c *gin.Context, node *db.Node, op string, pathValue string, ok bool, extra map[string]any) {
	if h == nil || h.DB == nil || node == nil {
		return
	}
	actor := getActor(c)
	var userID *uuid.UUID
	if actor != "" {
		var user db.User
		if err := h.DB.WithContext(c.Request.Context()).Where("username = ?", actor).First(&user).Error; err == nil {
			userID = &user.ID
		}
	}
	payload := map[string]any{}
	for k, v := range extra {
		payload[k] = v
	}
	row := db.FSAuditLog{
		UserID: userID,
		Actor:  actor,
		NodeID: node.ID,
		Op:     op,
		Path:   pathValue,
		Extra:  encodeJSON(payload),
		OK:     ok,
	}
	_ = h.DB.WithContext(c.Request.Context()).Create(&row).Error
}

func encodeJSON(payload map[string]any) datatypes.JSON {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(raw)
}
