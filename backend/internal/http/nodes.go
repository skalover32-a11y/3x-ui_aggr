package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/services/sshclient"
)

type nodeCreateRequest struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Tags          []string        `json:"tags"`
	Host          string          `json:"host"`
	Region        string          `json:"region"`
	Provider      string          `json:"provider"`
	Capabilities  json.RawMessage `json:"capabilities"`
	AllowedRoots  []string        `json:"allowed_roots"`
	IsSandbox     *bool           `json:"is_sandbox"`
	AgentEnabled  *bool           `json:"agent_enabled"`
	AgentURL      string          `json:"agent_url"`
	AgentToken    string          `json:"agent_token"`
	AgentInsecure *bool           `json:"agent_allow_insecure_tls"`
	IsEnabled     *bool           `json:"is_enabled"`
	SSHEnabled    *bool           `json:"ssh_enabled"`
	SSHAuthMethod string          `json:"ssh_auth_method"`
	SSHPassword   string          `json:"ssh_password"`
	BaseURL       string          `json:"base_url"`
	PanelUsername string          `json:"panel_username"`
	PanelPassword string          `json:"panel_password"`
	SSHHost       string          `json:"ssh_host"`
	SSHPort       int             `json:"ssh_port"`
	SSHUser       string          `json:"ssh_user"`
	SSHKey        string          `json:"ssh_key"`
	VerifyTLS     *bool           `json:"verify_tls"`
}

type nodeUpdateRequest struct {
	Name          *string          `json:"name"`
	Kind          *string          `json:"kind"`
	Tags          *[]string        `json:"tags"`
	Host          *string          `json:"host"`
	Region        *string          `json:"region"`
	Provider      *string          `json:"provider"`
	Capabilities  *json.RawMessage `json:"capabilities"`
	AllowedRoots  *[]string        `json:"allowed_roots"`
	IsSandbox     *bool            `json:"is_sandbox"`
	AgentEnabled  *bool            `json:"agent_enabled"`
	AgentURL      *string          `json:"agent_url"`
	AgentToken    *string          `json:"agent_token"`
	AgentInsecure *bool            `json:"agent_allow_insecure_tls"`
	IsEnabled     *bool            `json:"is_enabled"`
	SSHEnabled    *bool            `json:"ssh_enabled"`
	SSHAuthMethod *string          `json:"ssh_auth_method"`
	SSHPassword   *string          `json:"ssh_password"`
	BaseURL       *string          `json:"base_url"`
	PanelUsername *string          `json:"panel_username"`
	PanelPassword *string          `json:"panel_password"`
	SSHHost       *string          `json:"ssh_host"`
	SSHPort       *int             `json:"ssh_port"`
	SSHUser       *string          `json:"ssh_user"`
	SSHKey        *string          `json:"ssh_key"`
	VerifyTLS     *bool            `json:"verify_tls"`
}

type nodeResponse struct {
	ID                string          `json:"id"`
	OrgID             *string         `json:"org_id,omitempty"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Tags              []string        `json:"tags"`
	Host              string          `json:"host"`
	Region            string          `json:"region"`
	Provider          string          `json:"provider"`
	Capabilities      json.RawMessage `json:"capabilities"`
	AllowedRoots      []string        `json:"allowed_roots"`
	IsSandbox         bool            `json:"is_sandbox"`
	AgentEnabled      bool            `json:"agent_enabled"`
	AgentURL          *string         `json:"agent_url"`
	AgentToken        *string         `json:"agent_token,omitempty"`
	AgentInsecureTLS  bool            `json:"agent_allow_insecure_tls"`
	AgentInstalled    bool            `json:"agent_installed"`
	AgentVersion      *string         `json:"agent_version"`
	AgentLastSeenAt   *time.Time      `json:"agent_last_seen_at"`
	AgentOnline       bool            `json:"agent_online"`
	Online            bool            `json:"online"`
	IsEnabled         bool            `json:"is_enabled"`
	SSHEnabled        bool            `json:"ssh_enabled"`
	SSHAuthMethod     string          `json:"ssh_auth_method"`
	BaseURL           string          `json:"base_url"`
	PanelUsername     string          `json:"panel_username"`
	SSHHost           string          `json:"ssh_host"`
	SSHPort           int             `json:"ssh_port"`
	SSHUser           string          `json:"ssh_user"`
	VerifyTLS         bool            `json:"verify_tls"`
	XrayVersion       *string         `json:"xray_version"`
	PanelVersion      *string         `json:"panel_version"`
	VersionsCheckedAt *time.Time      `json:"versions_checked_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func toNodeResponse(node *db.Node) nodeResponse {
	agentInstalled := node.AgentInstalled || node.AgentEnabled
	agentOnline := computeAgentOnline(node.AgentLastSeenAt, agentInstalled, 90*time.Second)
	var orgID *string
	if node.OrgID != nil {
		idStr := node.OrgID.String()
		orgID = &idStr
	}
	return nodeResponse{
		ID:                node.ID.String(),
		OrgID:             orgID,
		Name:              node.Name,
		Kind:              node.Kind,
		Tags:              []string(node.Tags),
		Host:              node.Host,
		Region:            node.Region,
		Provider:          node.Provider,
		Capabilities:      json.RawMessage(node.Capabilities),
		AllowedRoots:      []string(node.AllowedRoots),
		IsSandbox:         node.IsSandbox,
		AgentEnabled:      node.AgentEnabled,
		AgentURL:          node.AgentURL,
		AgentInsecureTLS:  node.AgentInsecureTLS,
		AgentInstalled:    agentInstalled,
		AgentVersion:      node.AgentVersion,
		AgentLastSeenAt:   node.AgentLastSeenAt,
		AgentOnline:       agentOnline,
		Online:            agentOnline,
		IsEnabled:         node.IsEnabled,
		SSHEnabled:        node.SSHEnabled,
		SSHAuthMethod:     node.SSHAuthMethod,
		BaseURL:           node.BaseURL,
		PanelUsername:     node.PanelUsername,
		SSHHost:           node.SSHHost,
		SSHPort:           node.SSHPort,
		SSHUser:           node.SSHUser,
		VerifyTLS:         node.VerifyTLS,
		XrayVersion:       node.XrayVersion,
		PanelVersion:      node.PanelVersion,
		VersionsCheckedAt: node.VersionsCheckedAt,
		CreatedAt:         node.CreatedAt,
		UpdatedAt:         node.UpdatedAt,
	}
}

func computeAgentOnline(lastSeen *time.Time, installed bool, ttl time.Duration) bool {
	if !installed || lastSeen == nil {
		return false
	}
	return time.Since(*lastSeen) <= ttl
}

func (h *Handler) ListNodes(c *gin.Context) {
	query, err := h.scopedNodesQuery(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
		return
	}
	var nodes []db.Node
	if err := query.Find(&nodes).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list nodes")
		return
	}
	resp := make([]nodeResponse, 0, len(nodes))
	for i := range nodes {
		resp = append(resp, toNodeResponse(&nodes[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) GetNode(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	resp := toNodeResponse(node)
	role := c.GetString("role")
	if role == middleware.RoleAdmin && h.Encryptor != nil && node.AgentTokenEnc != nil && strings.TrimSpace(*node.AgentTokenEnc) != "" {
		if token, err := h.Encryptor.DecryptString(*node.AgentTokenEnc); err == nil {
			token = strings.TrimSpace(token)
			if token != "" {
				resp.AgentToken = &token
			}
		}
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateNode(c *gin.Context) {
	var req nodeCreateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	kind, err := normalizeNodeKind(req.Kind)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_KIND", err.Error())
		return
	}
	if err := validateNodeCreate(kind, &req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", err.Error())
		return
	}
	encPass, err := h.Encryptor.EncryptString(req.PanelPassword)
	if err != nil {
		msg := "failed to encrypt panel password"
		h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name, "base_url": req.BaseURL}, errString(err))
		respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
		return
	}
	encKey, err := h.Encryptor.EncryptString(req.SSHKey)
	if err != nil {
		msg := "failed to encrypt ssh key"
		h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name, "base_url": req.BaseURL}, errString(err))
		respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh key")
		return
	}
	verifyTLS := true
	if req.VerifyTLS != nil {
		verifyTLS = *req.VerifyTLS
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}
	isSandbox := false
	if req.IsSandbox != nil {
		isSandbox = *req.IsSandbox
	}
	agentEnabled := false
	if req.AgentEnabled != nil {
		agentEnabled = *req.AgentEnabled
	}
	agentInsecure := false
	if req.AgentInsecure != nil {
		agentInsecure = *req.AgentInsecure
	}
	sshEnabled := true
	if req.SSHEnabled != nil {
		sshEnabled = *req.SSHEnabled
	}
	authMethod := strings.TrimSpace(req.SSHAuthMethod)
	if authMethod == "" {
		authMethod = "key"
	}
	caps, err := parseCapabilities(req.Capabilities)
	if err != nil {
		msg := "invalid capabilities"
		h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name}, errString(err))
		respondError(c, http.StatusBadRequest, "INVALID_CAPS", "invalid capabilities")
		return
	}
	var sshPassEnc *string
	if strings.TrimSpace(req.SSHPassword) != "" {
		encPass, err := h.Encryptor.EncryptString(req.SSHPassword)
		if err != nil {
			msg := "failed to encrypt ssh password"
			h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh password")
			return
		}
		sshPassEnc = &encPass
	}

	node := db.Node{
		Name:             req.Name,
		Kind:             kind,
		Tags:             req.Tags,
		Host:             req.Host,
		Region:           req.Region,
		Provider:         req.Provider,
		Capabilities:     caps,
		AllowedRoots:     req.AllowedRoots,
		IsSandbox:        isSandbox,
		AgentEnabled:     agentEnabled,
		AgentURL:         nilifyString(req.AgentURL),
		AgentTokenEnc:    nil,
		AgentInsecureTLS: agentInsecure,
		IsEnabled:        isEnabled,
		SSHEnabled:       sshEnabled,
		SSHAuthMethod:    authMethod,
		SSHPasswordEnc:   sshPassEnc,
		BaseURL:          req.BaseURL,
		PanelUsername:    req.PanelUsername,
		PanelPasswordEnc: encPass,
		SSHHost:          req.SSHHost,
		SSHPort:          req.SSHPort,
		SSHUser:          req.SSHUser,
		SSHKeyEnc:        encKey,
		VerifyTLS:        verifyTLS,
	}
	if kind == "HOST" {
		node.BaseURL = ""
		node.PanelUsername = ""
		encEmpty, err := h.Encryptor.EncryptString("")
		if err != nil {
			msg := "failed to encrypt panel password"
			h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
			return
		}
		node.PanelPasswordEnc = encEmpty
	}
	if strings.TrimSpace(req.AgentToken) != "" {
		encToken, err := h.Encryptor.EncryptString(req.AgentToken)
		if err != nil {
			msg := "failed to encrypt agent token"
			h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt agent token")
			return
		}
		node.AgentTokenEnc = &encToken
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&node).Error; err != nil {
		msg := "failed to create node"
		h.auditEvent(c, nil, "NODE_CREATE", "error", &msg, gin.H{"name": req.Name, "base_url": req.BaseURL}, errString(err))
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create node")
		return
	}
	h.auditEvent(c, &node.ID, "NODE_CREATE", "ok", nil, gin.H{"name": node.Name, "base_url": node.BaseURL}, nil)
	respondStatus(c, http.StatusCreated, toNodeResponse(&node))
}

func (h *Handler) UpdateNode(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req nodeUpdateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if req.Name != nil {
		node.Name = *req.Name
	}
	if req.Tags != nil {
		node.Tags = *req.Tags
	}
	if req.Host != nil {
		node.Host = *req.Host
	}
	if req.Region != nil {
		node.Region = *req.Region
	}
	if req.Provider != nil {
		node.Provider = *req.Provider
	}
	if req.Capabilities != nil {
		caps, err := parseCapabilities(*req.Capabilities)
		if err != nil {
			msg := "invalid capabilities"
			h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
			respondError(c, http.StatusBadRequest, "INVALID_CAPS", "invalid capabilities")
			return
		}
		node.Capabilities = caps
	}
	if req.AllowedRoots != nil {
		node.AllowedRoots = *req.AllowedRoots
	}
	if req.IsSandbox != nil {
		node.IsSandbox = *req.IsSandbox
	}
	if req.AgentEnabled != nil {
		node.AgentEnabled = *req.AgentEnabled
	}
	if req.AgentURL != nil {
		node.AgentURL = nilifyString(*req.AgentURL)
	}
	if req.AgentInsecure != nil {
		node.AgentInsecureTLS = *req.AgentInsecure
	}
	if req.IsEnabled != nil {
		node.IsEnabled = *req.IsEnabled
	}
	if req.SSHEnabled != nil {
		node.SSHEnabled = *req.SSHEnabled
	}
	if req.SSHAuthMethod != nil {
		val := strings.TrimSpace(*req.SSHAuthMethod)
		if val == "" {
			val = "key"
		}
		node.SSHAuthMethod = val
	}
	if req.SSHPassword != nil {
		if strings.TrimSpace(*req.SSHPassword) == "" {
			node.SSHPasswordEnc = nil
		} else {
			encPass, err := h.Encryptor.EncryptString(*req.SSHPassword)
			if err != nil {
				msg := "failed to encrypt ssh password"
				h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
				respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh password")
				return
			}
			node.SSHPasswordEnc = &encPass
		}
	}
	if req.BaseURL != nil {
		node.BaseURL = *req.BaseURL
	}
	if req.PanelUsername != nil {
		node.PanelUsername = *req.PanelUsername
	}
	if req.PanelPassword != nil {
		encPass, err := h.Encryptor.EncryptString(*req.PanelPassword)
		if err != nil {
			msg := "failed to encrypt panel password"
			h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
			return
		}
		node.PanelPasswordEnc = encPass
	}
	kind := node.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "PANEL"
	}
	if req.Kind != nil {
		val, err := normalizeNodeKind(*req.Kind)
		if err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_KIND", err.Error())
			return
		}
		kind = val
		node.Kind = val
	}
	if err := validateNodeUpdate(kind, node, &req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", err.Error())
		return
	}
	if kind == "HOST" {
		node.BaseURL = ""
		node.PanelUsername = ""
		encPass, err := h.Encryptor.EncryptString("")
		if err != nil {
			msg := "failed to encrypt panel password"
			h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
			return
		}
		node.PanelPasswordEnc = encPass
	}
	if req.SSHHost != nil {
		node.SSHHost = *req.SSHHost
	}
	if req.SSHPort != nil {
		node.SSHPort = *req.SSHPort
	}
	if req.SSHUser != nil {
		node.SSHUser = *req.SSHUser
	}
	if req.SSHKey != nil {
		encKey, err := h.Encryptor.EncryptString(*req.SSHKey)
		if err != nil {
			msg := "failed to encrypt ssh key"
			h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh key")
			return
		}
		node.SSHKeyEnc = encKey
	}
	if req.VerifyTLS != nil {
		node.VerifyTLS = *req.VerifyTLS
	}
	if req.AgentToken != nil {
		if strings.TrimSpace(*req.AgentToken) == "" {
			node.AgentTokenEnc = nil
		} else {
			encToken, err := h.Encryptor.EncryptString(*req.AgentToken)
			if err != nil {
				msg := "failed to encrypt agent token"
				h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name}, errString(err))
				respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt agent token")
				return
			}
			node.AgentTokenEnc = &encToken
		}
	}
	if err := h.DB.WithContext(c.Request.Context()).Save(node).Error; err != nil {
		msg := "failed to update node"
		h.auditEvent(c, &node.ID, "NODE_UPDATE", "error", &msg, gin.H{"name": node.Name, "base_url": node.BaseURL}, errString(err))
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update node")
		return
	}
	h.auditEvent(c, &node.ID, "NODE_UPDATE", "ok", nil, gin.H{"name": node.Name, "base_url": node.BaseURL}, nil)
	respondStatus(c, http.StatusOK, toNodeResponse(node))
}

func parseCapabilities(raw json.RawMessage) (datatypes.JSON, error) {
	if len(raw) == 0 {
		return datatypes.JSON([]byte("{}")), nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("invalid json")
	}
	return datatypes.JSON(raw), nil
}

func nilifyString(val string) *string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	v := strings.TrimSpace(val)
	return &v
}

func (h *Handler) DeleteNode(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
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
}

func (h *Handler) deleteNodeRecords(ctx context.Context, node *db.Node) error {
	tx := h.DB.WithContext(ctx).Begin()
	if err := tx.Exec("DELETE FROM check_results WHERE check_id IN (SELECT id FROM checks WHERE target_type = 'node' AND target_id = ?)", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM check_results WHERE check_id IN (SELECT id FROM checks WHERE target_type = 'service' AND target_id IN (SELECT id FROM services WHERE node_id = ?))", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM check_results WHERE check_id IN (SELECT id FROM checks WHERE target_type = 'bot' AND target_id IN (SELECT id FROM bots WHERE node_id = ?))", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM alert_states WHERE node_id = ? OR service_id IN (SELECT id FROM services WHERE node_id = ?)", node.ID, node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM alert_states WHERE bot_id IN (SELECT id FROM bots WHERE node_id = ?)", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM checks WHERE target_type = 'service' AND target_id IN (SELECT id FROM services WHERE node_id = ?)", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM checks WHERE target_type = 'bot' AND target_id IN (SELECT id FROM bots WHERE node_id = ?)", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec("DELETE FROM checks WHERE target_type = 'node' AND target_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.Service{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.Bot{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.NodeCheck{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.NodeMetric{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.AuditLog{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&db.Node{}, "id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func (h *Handler) TestNode(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	ctx, cancel := h.withTimeout(context.Background())
	defer cancel()
	if isPanelNode(node) {
		panel, err := h.newPanelClient(node)
		if err != nil {
			msg := "failed to init panel client"
			h.auditEvent(c, &node.ID, "NODE_TEST", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusInternalServerError, "PANEL_CLIENT", "failed to init panel client")
			return
		}
		if err := panel.Login(); err != nil {
			msg := "panel login failed"
			h.auditEvent(c, &node.ID, "NODE_TEST", "error", &msg, gin.H{}, errString(err))
			respondError(c, http.StatusBadGateway, "PANEL_LOGIN", "panel login failed")
			return
		}
	}
	key, err := h.decryptSSHKey(node)
	if err != nil {
		msg := "failed to decrypt ssh key"
		h.auditEvent(c, &node.ID, "NODE_TEST", "error", &msg, gin.H{}, errString(err))
		respondError(c, http.StatusInternalServerError, "DEC_FAIL", "failed to decrypt ssh key")
		return
	}
	if err := h.SSHClient.Run(ctx, node.SSHHost, node.SSHPort, node.SSHUser, key, "true"); err != nil {
		log.Printf("ssh test failed node=%s host=%s:%d user=%s err=%v", node.ID.String(), node.SSHHost, node.SSHPort, node.SSHUser, err)
		stage := "command"
		msg := err.Error()
		var sshErr *sshclient.Error
		if errors.As(err, &sshErr) {
			stage = sshErr.Stage
			msg = sshErr.Err.Error()
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"ok":    false,
			"stage": stage,
			"error": msg,
		})
		logMsg := "ssh test failed"
		h.auditEvent(c, &node.ID, "NODE_TEST", "error", &logMsg, gin.H{"stage": stage}, &msg)
		return
	}
	h.auditEvent(c, &node.ID, "NODE_TEST", "ok", nil, gin.H{}, nil)
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func normalizeNodeKind(raw string) (string, error) {
	val := strings.ToUpper(strings.TrimSpace(raw))
	if val == "" {
		return "PANEL", nil
	}
	if val != "PANEL" && val != "HOST" {
		return "", errors.New("kind must be PANEL or HOST")
	}
	return val, nil
}

func validateNodeCreate(kind string, req *nodeCreateRequest) error {
	if kind == "PANEL" {
		if strings.TrimSpace(req.BaseURL) == "" {
			return errors.New("base_url required for PANEL node")
		}
		if strings.TrimSpace(req.PanelUsername) == "" {
			return errors.New("panel_username required for PANEL node")
		}
		if strings.TrimSpace(req.PanelPassword) == "" {
			return errors.New("panel_password required for PANEL node")
		}
	}
	return nil
}

func validateNodeUpdate(kind string, node *db.Node, req *nodeUpdateRequest) error {
	if kind != "PANEL" {
		return nil
	}
	baseURL := node.BaseURL
	if req.BaseURL != nil {
		baseURL = strings.TrimSpace(*req.BaseURL)
	}
	panelUser := node.PanelUsername
	if req.PanelUsername != nil {
		panelUser = strings.TrimSpace(*req.PanelUsername)
	}
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("base_url required for PANEL node")
	}
	if strings.TrimSpace(panelUser) == "" {
		return errors.New("panel_username required for PANEL node")
	}
	if req.PanelPassword != nil && strings.TrimSpace(*req.PanelPassword) == "" {
		return errors.New("panel_password required for PANEL node")
	}
	return nil
}

func isPanelNode(node *db.Node) bool {
	if node == nil {
		return false
	}
	if strings.TrimSpace(node.Kind) == "" {
		return strings.TrimSpace(node.BaseURL) != ""
	}
	return strings.EqualFold(node.Kind, "PANEL") && strings.TrimSpace(node.BaseURL) != ""
}
