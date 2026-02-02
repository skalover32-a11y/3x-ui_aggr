package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

const maxKeyFileSize = 512 * 1024

type orgKeyResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Ext         string    `json:"ext"`
	SizeBytes   int       `json:"size_bytes"`
	Label       *string   `json:"label,omitempty"`
	Desc        *string   `json:"description,omitempty"`
	Fingerprint *string   `json:"fingerprint,omitempty"`
	NodeID      *string   `json:"node_id,omitempty"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *Handler) ListOrgKeys(c *gin.Context) {
	orgID, ok := getOrgIDParam(c)
	if !ok {
		return
	}
	type row struct {
		ID          uuid.UUID
		Filename    string
		Ext         string
		SizeBytes   int
		CreatedAt   time.Time
		Username    *string
		Label       *string
		Description *string
		Fingerprint *string
		NodeID      *uuid.UUID
	}
	var rows []row
	if err := h.DB.WithContext(c.Request.Context()).
		Table("org_keys AS k").
		Select("k.id, k.filename, k.ext, k.size_bytes, k.created_at, u.username, k.label, k.description, k.fingerprint, k.node_id").
		Joins("LEFT JOIN users u ON u.id = k.created_by_user_id").
		Where("k.org_id = ?", orgID).
		Order("k.created_at DESC").
		Scan(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list keys")
		return
	}
	resp := make([]orgKeyResponse, 0, len(rows))
	for _, row := range rows {
		var nodeID *string
		if row.NodeID != nil {
			val := row.NodeID.String()
			nodeID = &val
		}
		resp = append(resp, orgKeyResponse{
			ID:          row.ID.String(),
			Filename:    row.Filename,
			Ext:         row.Ext,
			SizeBytes:   row.SizeBytes,
			Label:       row.Label,
			Desc:        row.Description,
			Fingerprint: row.Fingerprint,
			NodeID:      nodeID,
			CreatedBy:   row.Username,
			CreatedAt:   row.CreatedAt,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) UploadOrgKey(c *gin.Context) {
	orgID, ok := getOrgIDParam(c)
	if !ok {
		return
	}
	if h.Encryptor == nil {
		respondError(c, http.StatusInternalServerError, "ENC", "encryptor not configured")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "FILE", "file required")
		return
	}
	label := strings.TrimSpace(c.PostForm("label"))
	description := strings.TrimSpace(c.PostForm("description"))
	nodeIDRaw := strings.TrimSpace(c.PostForm("node_id"))
	var nodeID *uuid.UUID
	if nodeIDRaw != "" {
		parsed, err := uuid.Parse(nodeIDRaw)
		if err != nil {
			respondError(c, http.StatusBadRequest, "NODE_ID", "invalid node id")
			return
		}
		if _, err := h.getNodeForActor(c, parsed.String()); err != nil {
			respondError(c, http.StatusNotFound, "NODE_ID", "node not found")
			return
		}
		nodeID = &parsed
	}
	if file.Size <= 0 || file.Size > maxKeyFileSize {
		respondError(c, http.StatusBadRequest, "FILE_SIZE", "file too large")
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".pem" && ext != ".ppk" && ext != ".key" {
		respondError(c, http.StatusBadRequest, "FILE_EXT", "invalid key extension")
		return
	}
	f, err := file.Open()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "FILE_READ", "failed to read file")
		return
	}
	defer f.Close()
	data := make([]byte, file.Size)
	n, err := f.Read(data)
	if err != nil || int64(n) != file.Size {
		respondError(c, http.StatusInternalServerError, "FILE_READ", "failed to read file")
		return
	}
	sum := sha256.Sum256(data)
	fingerprint := "sha256:" + hex.EncodeToString(sum[:])
	encoded := base64.StdEncoding.EncodeToString(data)
	enc, err := h.Encryptor.EncryptString(encoded)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "ENC", "failed to encrypt")
		return
	}
	actor := getActor(c)
	var createdBy *uuid.UUID
	if actor != "" {
		if user, err := h.actorUser(c); err == nil {
			createdBy = &user.ID
		}
	}
	row := db.OrgKey{
		OrgID:         orgID,
		Filename:      file.Filename,
		Ext:           ext,
		ContentEnc:    enc,
		SizeBytes:     int(file.Size),
		Label:         nullIfEmpty(label),
		Description:   nullIfEmpty(description),
		Fingerprint:   &fingerprint,
		NodeID:        nodeID,
		CreatedByUser: createdBy,
		CreatedAt:     time.Now().UTC(),
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to save key")
		return
	}
	var nodeIDStr *string
	if row.NodeID != nil {
		val := row.NodeID.String()
		nodeIDStr = &val
	}
	respondStatus(c, http.StatusCreated, orgKeyResponse{
		ID:          row.ID.String(),
		Filename:    row.Filename,
		Ext:         row.Ext,
		SizeBytes:   row.SizeBytes,
		Label:       row.Label,
		Desc:        row.Description,
		Fingerprint: row.Fingerprint,
		NodeID:      nodeIDStr,
		CreatedAt:   row.CreatedAt,
	})
}

type orgKeyUpdateRequest struct {
	Label       *string `json:"label"`
	Description *string `json:"description"`
	NodeID      *string `json:"node_id"`
}

func (h *Handler) UpdateOrgKey(c *gin.Context) {
	orgID, ok := getOrgIDParam(c)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "KEY_ID", "invalid key id")
		return
	}
	var req orgKeyUpdateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	var row db.OrgKey
	if err := h.DB.WithContext(c.Request.Context()).
		First(&row, "id = ? AND org_id = ?", keyID, orgID).Error; err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "key not found")
		return
	}
	if req.NodeID != nil {
		if strings.TrimSpace(*req.NodeID) == "" {
			row.NodeID = nil
		} else {
			parsed, err := uuid.Parse(strings.TrimSpace(*req.NodeID))
			if err != nil {
				respondError(c, http.StatusBadRequest, "NODE_ID", "invalid node id")
				return
			}
			if _, err := h.getNodeForActor(c, parsed.String()); err != nil {
				respondError(c, http.StatusNotFound, "NODE_ID", "node not found")
				return
			}
			row.NodeID = &parsed
		}
	}
	if req.Label != nil {
		row.Label = nullIfEmpty(strings.TrimSpace(*req.Label))
	}
	if req.Description != nil {
		row.Description = nullIfEmpty(strings.TrimSpace(*req.Description))
	}
	if err := h.DB.WithContext(c.Request.Context()).Save(&row).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update key")
		return
	}
	var nodeIDStr *string
	if row.NodeID != nil {
		val := row.NodeID.String()
		nodeIDStr = &val
	}
	respondStatus(c, http.StatusOK, orgKeyResponse{
		ID:          row.ID.String(),
		Filename:    row.Filename,
		Ext:         row.Ext,
		SizeBytes:   row.SizeBytes,
		Label:       row.Label,
		Desc:        row.Description,
		Fingerprint: row.Fingerprint,
		NodeID:      nodeIDStr,
		CreatedAt:   row.CreatedAt,
	})
}

func (h *Handler) DownloadOrgKey(c *gin.Context) {
	orgID, ok := getOrgIDParam(c)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "KEY_ID", "invalid key id")
		return
	}
	var row db.OrgKey
	if err := h.DB.WithContext(c.Request.Context()).
		First(&row, "id = ? AND org_id = ?", keyID, orgID).Error; err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "key not found")
		return
	}
	if h.Encryptor == nil {
		respondError(c, http.StatusInternalServerError, "ENC", "encryptor not configured")
		return
	}
	dec, err := h.Encryptor.DecryptString(row.ContentEnc)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DEC", "failed to decrypt")
		return
	}
	data, err := base64.StdEncoding.DecodeString(dec)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DEC", "failed to decode")
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+row.Filename+"\"")
	c.Data(http.StatusOK, "application/octet-stream", data)
}

func (h *Handler) DeleteOrgKey(c *gin.Context) {
	orgID, ok := getOrgIDParam(c)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "KEY_ID", "invalid key id")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).
		Where("id = ? AND org_id = ?", keyID, orgID).
		Delete(&db.OrgKey{}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete key")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func getOrgIDParam(c *gin.Context) (uuid.UUID, bool) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(orgID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return uuid.Nil, false
	}
	return parsed, true
}

func nullIfEmpty(val string) *string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	v := strings.TrimSpace(val)
	return &v
}
