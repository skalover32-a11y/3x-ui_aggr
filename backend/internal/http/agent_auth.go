package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/agentauth"
)

type agentInfo struct {
	Version  string `json:"version"`
	OS       string `json:"os"`
	Hostname string `json:"hostname"`
}

type agentRegisterRequest struct {
	NodeID            string    `json:"node_id"`
	RegistrationToken string    `json:"registration_token"`
	AgentInfo         agentInfo `json:"agent_info"`
}

type agentRegisterResponse struct {
	AgentToken string `json:"agent_token"`
	Node       struct {
		ID    string  `json:"id"`
		OrgID *string `json:"org_id,omitempty"`
		Name  string  `json:"name"`
	} `json:"node"`
}

func (h *Handler) AgentRegister(c *gin.Context) {
	var req agentRegisterRequest
	if !parseJSONBody(c, &req) {
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_NODE", "invalid node id")
		return
	}
	regToken := strings.TrimSpace(req.RegistrationToken)
	if regToken == "" {
		respondError(c, http.StatusBadRequest, "INVALID_TOKEN", "registration token required")
		return
	}
	hash := agentauth.HashToken(regToken, h.TokenSalt)
	var reg db.NodeRegistrationToken
	if err := h.DB.WithContext(c.Request.Context()).
		Where("node_id = ? AND token_hash = ?", nodeID, hash).
		First(&reg).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			respondError(c, http.StatusUnauthorized, "REG_TOKEN_INVALID", "invalid registration token")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read registration token")
		return
	}
	if reg.UsedAt != nil {
		respondError(c, http.StatusUnauthorized, "REG_TOKEN_USED", "registration token already used")
		return
	}
	if time.Now().After(reg.ExpiresAt) {
		respondError(c, http.StatusUnauthorized, "REG_TOKEN_EXPIRED", "registration token expired")
		return
	}
	var node db.Node
	if err := h.DB.WithContext(c.Request.Context()).First(&node, "id = ?", nodeID).Error; err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	agentToken, err := agentauth.GenerateToken("AGENT_")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_GEN", "failed to generate agent token")
		return
	}
	agentHash := agentauth.HashToken(agentToken, h.TokenSalt)
	now := time.Now().UTC()
	if err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.NodeRegistrationToken{}).
			Where("id = ? AND used_at IS NULL", reg.ID).
			Update("used_at", now).Error; err != nil {
			return err
		}
		cred := db.AgentCredential{
			NodeID:     nodeID,
			TokenHash:  agentHash,
			CreatedAt:  now,
			LastSeenAt: &now,
			RevokedAt:  nil,
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_id"}},
			DoUpdates: clause.Assignments(map[string]any{"token_hash": agentHash, "revoked_at": nil, "last_seen_at": now}),
		}).Create(&cred).Error
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to register agent")
		return
	}
	_ = h.DB.WithContext(c.Request.Context()).Model(&db.Node{}).Where("id = ?", nodeID).Update("agent_last_seen_at", now).Error
	resp := agentRegisterResponse{AgentToken: agentToken}
	resp.Node.ID = node.ID.String()
	resp.Node.Name = node.Name
	if node.OrgID != nil {
		idStr := node.OrgID.String()
		resp.Node.OrgID = &idStr
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) AgentHeartbeat(c *gin.Context) {
	nodeID, ok := c.Get("agent_node_id")
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}
	id, ok := nodeID.(uuid.UUID)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}
	now := time.Now().UTC()
	if err := h.DB.WithContext(c.Request.Context()).Model(&db.AgentCredential{}).
		Where("node_id = ? AND revoked_at IS NULL", id).
		Update("last_seen_at", now).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update heartbeat")
		return
	}
	_ = h.DB.WithContext(c.Request.Context()).Model(&db.Node{}).Where("id = ?", id).Update("agent_last_seen_at", now).Error
	respondStatus(c, http.StatusOK, gin.H{"ok": true, "ts": now})
}

func (h *Handler) AgentAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		token = strings.TrimSpace(token)
		if token == "" {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			c.Abort()
			return
		}
		hash := agentauth.HashToken(token, h.TokenSalt)
		var cred db.AgentCredential
		if err := h.DB.WithContext(c.Request.Context()).
			Where("token_hash = ? AND revoked_at IS NULL", hash).
			First(&cred).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
				c.Abort()
				return
			}
			respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read agent credentials")
			c.Abort()
			return
		}
		c.Set("agent_node_id", cred.NodeID)
		c.Next()
	}
}
