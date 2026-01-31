package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/agent"
	"agr_3x_ui/internal/db"
)

type sqliteStartRequest struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly *bool  `json:"read_only"`
}

type adminerStartRequest struct {
	Engine string `json:"engine"`
}

func (h *Handler) ListNodeSqlite(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	var resp map[string]any
	if err := h.agentJSON(c.Request.Context(), node, http.MethodGet, "/apps/db/sqlite/list", nil, &resp); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) StartNodeSqlite(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	var req sqliteStartRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if req.ReadOnly == nil {
		value := true
		req.ReadOnly = &value
	}
	var resp map[string]any
	if err := h.agentJSON(c.Request.Context(), node, http.MethodPost, "/apps/db/sqlite/start", req, &resp); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		return
	}
	resp["proxy_path"] = "/nodes/" + node.ID.String() + "/db/sqlite/ui/"
	h.auditEvent(c, &node.ID, "db_sqlite_open", "success", nil, map[string]any{"path": req.Path, "name": req.Name}, nil)
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) StartNodeAdminer(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	var req adminerStartRequest
	if !parseJSONBody(c, &req) {
		return
	}
	var resp map[string]any
	if err := h.agentJSON(c.Request.Context(), node, http.MethodPost, "/apps/db/adminer/start", req, &resp); err != nil {
		respondError(c, http.StatusBadGateway, "AGENT_ERROR", err.Error())
		return
	}
	resp["proxy_path"] = "/nodes/" + node.ID.String() + "/db/adminer/ui/"
	h.auditEvent(c, &node.ID, "db_adminer_open", "success", nil, map[string]any{"engine": req.Engine}, nil)
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) ProxyNodeSqlite(c *gin.Context) {
	h.proxyAgentUI(c, "/apps/db/sqlite/ui")
}

func (h *Handler) ProxyNodeAdminer(c *gin.Context) {
	h.proxyAgentUI(c, "/apps/db/adminer/ui")
}

func (h *Handler) proxyAgentUI(c *gin.Context, agentBase string) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NODE_NOT_FOUND", "node not found")
		return
	}
	agentURL, token, transport, err := h.agentTransport(node)
	if err != nil {
		respondError(c, http.StatusBadRequest, "AGENT_CONFIG", err.Error())
		return
	}
	targetPath := agentBase + c.Param("path")
	if targetPath == agentBase {
		targetPath = agentBase + "/"
	}
	proxy := httputil.NewSingleHostReverseProxy(agentURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = targetPath
		req.URL.RawPath = targetPath
		req.URL.RawQuery = c.Request.URL.RawQuery
		req.Host = agentURL.Host
		externalPrefix := strings.TrimSuffix(c.Request.URL.Path, c.Param("path"))
		if externalPrefix == "" {
			externalPrefix = c.Request.URL.Path
		}
		req.Header.Set("X-Forwarded-Prefix", externalPrefix)
		if tokenParam := strings.TrimSpace(c.Query("token")); tokenParam != "" {
			req.Header.Set("X-Forwarded-Token", tokenParam)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		respondError(c, http.StatusBadGateway, "AGENT_PROXY", "agent proxy failed")
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func (h *Handler) agentJSON(ctx context.Context, node *db.Node, method, path string, body any, dest any) error {
	agentURL, token, transport, err := h.agentTransport(node)
	if err != nil {
		return err
	}
	full := strings.TrimRight(agentURL.String(), "/") + path
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return errors.New(strings.TrimSpace(string(raw)))
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (h *Handler) agentTransport(node *db.Node) (*url.URL, string, http.RoundTripper, error) {
	if node == nil || !node.AgentEnabled || node.AgentURL == nil || strings.TrimSpace(*node.AgentURL) == "" {
		return nil, "", nil, errors.New("agent not configured")
	}
	resolved := agent.ResolveURL(strings.TrimSpace(*node.AgentURL))
	parsed, err := url.Parse(resolved)
	if err != nil {
		return nil, "", nil, err
	}
	token := ""
	if node.AgentTokenEnc != nil && strings.TrimSpace(*node.AgentTokenEnc) != "" && h.Encryptor != nil {
		if val, err := h.Encryptor.DecryptString(*node.AgentTokenEnc); err == nil {
			token = strings.TrimSpace(val)
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if node.AgentInsecureTLS && parsed.Scheme == "https" {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return parsed, token, tr, nil
}
