package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/agentauth"
)

type orgResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type orgCreateRequest struct {
	Name string `json:"name"`
}

type orgNodeCreateResponse struct {
	Node              nodeResponse `json:"node"`
	RegistrationToken string       `json:"registration_token"`
	InstallCommand    string       `json:"install_command"`
}

func (h *Handler) ListOrgs(c *gin.Context) {
	actor := getActor(c)
	if strings.EqualFold(actor, h.AdminUser) {
		var orgs []db.Organization
		if err := h.DB.WithContext(c.Request.Context()).Find(&orgs).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list orgs")
			return
		}
		resp := make([]orgResponse, 0, len(orgs))
		for _, org := range orgs {
			resp = append(resp, orgResponse{ID: org.ID.String(), Name: org.Name, Role: "owner", CreatedAt: org.CreatedAt})
		}
		respondStatus(c, http.StatusOK, resp)
		return
	}
	user, err := h.findUserByActor(c, actor)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	type row struct {
		ID        uuid.UUID
		Name      string
		Role      string
		CreatedAt time.Time
	}
	var rows []row
	if err := h.DB.WithContext(c.Request.Context()).Table("organizations AS o").
		Select("o.id, o.name, m.role, o.created_at").
		Joins("JOIN organization_members m ON m.org_id = o.id").
		Where("m.user_id = ?", user.ID).
		Order("o.created_at DESC").
		Scan(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list orgs")
		return
	}
	resp := make([]orgResponse, 0, len(rows))
	for _, org := range rows {
		resp = append(resp, orgResponse{ID: org.ID.String(), Name: org.Name, Role: org.Role, CreatedAt: org.CreatedAt})
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateOrg(c *gin.Context) {
	var req orgCreateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(c, http.StatusBadRequest, "ORG_NAME", "name required")
		return
	}
	actor := getActor(c)
	user, err := h.findUserByActor(c, actor)
	if err != nil && !strings.EqualFold(actor, h.AdminUser) {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	ownerID := uuid.Nil
	if user != nil {
		ownerID = user.ID
	}
	var org db.Organization
	var role string
	role = "owner"
	if err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		org = db.Organization{
			Name:        name,
			OwnerUserID: ownerID,
		}
		if err := tx.Create(&org).Error; err != nil {
			return err
		}
		if user != nil {
			member := db.OrganizationMember{
				OrgID:  org.ID,
				UserID: user.ID,
				Role:   role,
			}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create org")
		return
	}
	respondStatus(c, http.StatusCreated, orgResponse{ID: org.ID.String(), Name: org.Name, Role: role, CreatedAt: org.CreatedAt})
}

func (h *Handler) ListOrgNodes(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	parsed, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	var nodes []db.Node
	if err := h.DB.WithContext(c.Request.Context()).Where("org_id = ?", parsed).Find(&nodes).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list nodes")
		return
	}
	resp := make([]nodeResponse, 0, len(nodes))
	for i := range nodes {
		resp = append(resp, toNodeResponse(&nodes[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateOrgNode(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	parsedOrg, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	if strings.TrimSpace(h.PublicBaseURL) == "" {
		respondError(c, http.StatusBadRequest, "PUBLIC_BASE_URL", "public base url required")
		return
	}
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
		respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
		return
	}
	encKey, err := h.Encryptor.EncryptString(req.SSHKey)
	if err != nil {
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
		respondError(c, http.StatusBadRequest, "INVALID_CAPS", "invalid capabilities")
		return
	}
	var sshPassEnc *string
	if strings.TrimSpace(req.SSHPassword) != "" {
		encPass, err := h.Encryptor.EncryptString(req.SSHPassword)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh password")
			return
		}
		sshPassEnc = &encPass
	}

	node := db.Node{
		OrgID:            &parsedOrg,
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
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt panel password")
			return
		}
		node.PanelPasswordEnc = encEmpty
	}
	if strings.TrimSpace(req.AgentToken) != "" {
		encToken, err := h.Encryptor.EncryptString(req.AgentToken)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt agent token")
			return
		}
		node.AgentTokenEnc = &encToken
	}
	regToken, err := agentauth.GenerateToken("REG_")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_GEN", "failed to generate registration token")
		return
	}
	regHash := agentauth.HashToken(regToken, h.TokenSalt)
	expiresAt := time.Now().Add(20 * time.Minute)
	regRow := db.NodeRegistrationToken{
		NodeID:    node.ID,
		TokenHash: regHash,
		ExpiresAt: expiresAt,
	}
	if err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&node).Error; err != nil {
			return err
		}
		regRow.NodeID = node.ID
		return tx.Create(&regRow).Error
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create node")
		return
	}
	base := strings.TrimRight(h.PublicBaseURL, "/")
	install := "curl -fsSL " + base + "/agent/install.sh | bash -s -- --server " + base + " --reg-token " + regToken + " --node-id " + node.ID.String()
	respondStatus(c, http.StatusCreated, orgNodeCreateResponse{Node: toNodeResponse(&node), RegistrationToken: regToken, InstallCommand: install})
}

func (h *Handler) GetOrgNode(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	parsedOrg, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	nodeID, err := uuid.Parse(c.Param("nodeId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", "invalid node id")
		return
	}
	node, err := h.ensureNodeInOrg(c.Request.Context(), parsedOrg, nodeID)
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	respondStatus(c, http.StatusOK, toNodeResponse(node))
}

func (h *Handler) DeleteOrgNode(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	parsedOrg, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	nodeID, err := uuid.Parse(c.Param("nodeId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", "invalid node id")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Where("id = ? AND org_id = ?", nodeID, parsedOrg).Delete(&db.Node{}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete node")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) RevokeAgent(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	parsedOrg, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	nodeID, err := uuid.Parse(c.Param("nodeId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", "invalid node id")
		return
	}
	if _, err := h.ensureNodeInOrg(c.Request.Context(), parsedOrg, nodeID); err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	now := time.Now().UTC()
	if err := h.DB.WithContext(c.Request.Context()).Model(&db.AgentCredential{}).
		Where("node_id = ? AND revoked_at IS NULL", nodeID).
		Update("revoked_at", now).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to revoke agent")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) ensureNodeInOrg(ctx context.Context, orgID uuid.UUID, nodeID uuid.UUID) (*db.Node, error) {
	var node db.Node
	if err := h.DB.WithContext(ctx).First(&node, "id = ? AND org_id = ?", nodeID, orgID).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (h *Handler) findUserByActor(c *gin.Context, actor string) (*db.User, error) {
	if strings.TrimSpace(actor) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var user db.User
	if err := h.DB.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", actor).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
