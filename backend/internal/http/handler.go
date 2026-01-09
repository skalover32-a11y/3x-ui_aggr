package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/audit"
	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/alerts"
	"agr_3x_ui/internal/services/checks"
	"agr_3x_ui/internal/services/panelclient"
	"agr_3x_ui/internal/services/sshclient"
	"agr_3x_ui/internal/services/sshws"
)

type Handler struct {
	DB                  *gorm.DB
	Encryptor           *security.Encryptor
	Audit               *audit.Service
	Alerts              *alerts.Service
	Checks              *checks.Worker
	AdminUser           string
	AdminPass           string
	JWTSecret           []byte
	JWTExpiry           time.Duration
	RefreshTTL          time.Duration
	WebAuthnRegisterTTL time.Duration
	WebAuthnLoginTTL    time.Duration
	FileAllowedRoots    []string
	FilePreviewMaxBytes int64
	FileTailMaxBytes    int64
	WebAuthn            WebAuthnProvider
	SSHClient           *sshclient.Client
	SSHManager          *sshws.Manager
	SSHIdleTimeout      time.Duration
}

func (h *Handler) getNode(ctx context.Context, idStr string) (*db.Node, error) {
	nodeID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	var node db.Node
	if err := h.DB.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (h *Handler) newPanelClient(node *db.Node) (*panelclient.Client, error) {
	pass, err := h.Encryptor.DecryptString(node.PanelPasswordEnc)
	if err != nil {
		return nil, err
	}
	return panelclient.New(node.BaseURL, node.PanelUsername, pass, node.VerifyTLS)
}

func (h *Handler) decryptSSHKey(node *db.Node) (string, error) {
	return h.Encryptor.DecryptString(node.SSHKeyEnc)
}

func (h *Handler) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 20*time.Second)
}

func getActor(c *gin.Context) string {
	actor := c.GetString("actor")
	if actor == "" {
		actor = "admin"
	}
	return actor
}

func (h *Handler) auditEvent(c *gin.Context, nodeID *uuid.UUID, action string, status string, message *string, payload any, errMsg *string) {
	if h == nil || h.Audit == nil {
		return
	}
	actor := getActor(c)
	ip := c.ClientIP()
	h.Audit.Write(c.Request.Context(), actor, &actor, &ip, nodeID, action, status, message, payload, errMsg)
}

func validateConfirm(confirm string) bool {
	return strings.TrimSpace(confirm) == "REBOOT"
}

func errString(err error) *string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	return &msg
}

func respondStatus(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func parseJSONBody(c *gin.Context, dest any) bool {
	if err := c.ShouldBindJSON(dest); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_JSON", fmt.Sprintf("invalid json: %v", err))
		return false
	}
	return true
}
