package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/sshclient"
)

type nodeCreateRequest struct {
	Name          string   `json:"name"`
	Tags          []string `json:"tags"`
	BaseURL       string   `json:"base_url"`
	PanelUsername string   `json:"panel_username"`
	PanelPassword string   `json:"panel_password"`
	SSHHost       string   `json:"ssh_host"`
	SSHPort       int      `json:"ssh_port"`
	SSHUser       string   `json:"ssh_user"`
	SSHKey        string   `json:"ssh_key"`
	VerifyTLS     *bool    `json:"verify_tls"`
}

type nodeUpdateRequest struct {
	Name          *string   `json:"name"`
	Tags          *[]string `json:"tags"`
	BaseURL       *string   `json:"base_url"`
	PanelUsername *string   `json:"panel_username"`
	PanelPassword *string   `json:"panel_password"`
	SSHHost       *string   `json:"ssh_host"`
	SSHPort       *int      `json:"ssh_port"`
	SSHUser       *string   `json:"ssh_user"`
	SSHKey        *string   `json:"ssh_key"`
	VerifyTLS     *bool     `json:"verify_tls"`
}

type nodeResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Tags              []string   `json:"tags"`
	BaseURL           string     `json:"base_url"`
	PanelUsername     string     `json:"panel_username"`
	SSHHost           string     `json:"ssh_host"`
	SSHPort           int        `json:"ssh_port"`
	SSHUser           string     `json:"ssh_user"`
	VerifyTLS         bool       `json:"verify_tls"`
	XrayVersion       *string    `json:"xray_version"`
	PanelVersion      *string    `json:"panel_version"`
	VersionsCheckedAt *time.Time `json:"versions_checked_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func toNodeResponse(node *db.Node) nodeResponse {
	return nodeResponse{
		ID:                node.ID.String(),
		Name:              node.Name,
		Tags:              []string(node.Tags),
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

func (h *Handler) ListNodes(c *gin.Context) {
	var nodes []db.Node
	if err := h.DB.WithContext(c.Request.Context()).Find(&nodes).Error; err != nil {
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
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	respondStatus(c, http.StatusOK, toNodeResponse(node))
}

func (h *Handler) CreateNode(c *gin.Context) {
	var req nodeCreateRequest
	if !parseJSONBody(c, &req) {
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
	node := db.Node{
		Name:             req.Name,
		Tags:             req.Tags,
		BaseURL:          req.BaseURL,
		PanelUsername:    req.PanelUsername,
		PanelPasswordEnc: encPass,
		SSHHost:          req.SSHHost,
		SSHPort:          req.SSHPort,
		SSHUser:          req.SSHUser,
		SSHKeyEnc:        encKey,
		VerifyTLS:        verifyTLS,
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&node).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create node")
		return
	}
	respondStatus(c, http.StatusCreated, toNodeResponse(&node))
}

func (h *Handler) UpdateNode(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
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
	if req.BaseURL != nil {
		node.BaseURL = *req.BaseURL
	}
	if req.PanelUsername != nil {
		node.PanelUsername = *req.PanelUsername
	}
	if req.PanelPassword != nil {
		encPass, err := h.Encryptor.EncryptString(*req.PanelPassword)
		if err != nil {
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
			respondError(c, http.StatusInternalServerError, "ENC_FAIL", "failed to encrypt ssh key")
			return
		}
		node.SSHKeyEnc = encKey
	}
	if req.VerifyTLS != nil {
		node.VerifyTLS = *req.VerifyTLS
	}
	if err := h.DB.WithContext(c.Request.Context()).Save(node).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update node")
		return
	}
	respondStatus(c, http.StatusOK, toNodeResponse(node))
}

func (h *Handler) DeleteNode(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	ctx := c.Request.Context()
	tx := h.DB.WithContext(ctx).Begin()
	if err := tx.Delete(&db.NodeCheck{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete node checks")
		return
	}
	if err := tx.Delete(&db.AuditLog{}, "node_id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete audit logs")
		return
	}
	if err := tx.Delete(&db.Node{}, "id = ?", node.ID).Error; err != nil {
		tx.Rollback()
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete node")
		return
	}
	if err := tx.Commit().Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to commit delete")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) TestNode(c *gin.Context) {
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
	ctx, cancel := h.withTimeout(context.Background())
	defer cancel()
	if err := panel.Login(); err != nil {
		respondError(c, http.StatusBadGateway, "PANEL_LOGIN", "panel login failed")
		return
	}
	key, err := h.decryptSSHKey(node)
	if err != nil {
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
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}
