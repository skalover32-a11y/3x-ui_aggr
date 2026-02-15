package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type orgBackupNode struct {
	ID               uuid.UUID      `json:"id"`
	Name             string         `json:"name"`
	Kind             string         `json:"kind"`
	Tags             []string       `json:"tags"`
	Host             string         `json:"host"`
	Region           string         `json:"region"`
	Provider         string         `json:"provider"`
	BaseURL          string         `json:"base_url"`
	PanelUsername    string         `json:"panel_username"`
	PanelPasswordEnc string         `json:"panel_password_enc"`
	Capabilities     datatypes.JSON `json:"capabilities"`
	AllowedRoots     []string       `json:"allowed_roots"`
	IsSandbox        bool           `json:"is_sandbox"`
	AgentEnabled     bool           `json:"agent_enabled"`
	AgentURL         *string        `json:"agent_url,omitempty"`
	AgentTokenEnc    *string        `json:"agent_token_enc,omitempty"`
	AgentInsecureTLS bool           `json:"agent_allow_insecure_tls"`
	IsEnabled        bool           `json:"is_enabled"`
	SSHEnabled       bool           `json:"ssh_enabled"`
	SSHHost          string         `json:"ssh_host"`
	SSHPort          int            `json:"ssh_port"`
	SSHUser          string         `json:"ssh_user"`
	SSHAuthMethod    string         `json:"ssh_auth_method"`
	SSHPasswordEnc   *string        `json:"ssh_password_enc,omitempty"`
	SSHKeyEnc        string         `json:"ssh_key_enc"`
	VerifyTLS        bool           `json:"verify_tls"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type orgBackupService struct {
	ID             uuid.UUID      `json:"id"`
	NodeID         uuid.UUID      `json:"node_id"`
	Kind           string         `json:"kind"`
	URL            *string        `json:"url,omitempty"`
	Host           *string        `json:"host,omitempty"`
	Port           *int           `json:"port,omitempty"`
	TLSMode        *string        `json:"tls_mode,omitempty"`
	HealthPath     *string        `json:"health_path,omitempty"`
	ExpectedStatus []int64        `json:"expected_status"`
	Headers        datatypes.JSON `json:"headers"`
	AuthRef        *string        `json:"auth_ref,omitempty"`
	IsEnabled      bool           `json:"is_enabled"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type orgBackupBot struct {
	ID              uuid.UUID `json:"id"`
	NodeID          uuid.UUID `json:"node_id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	DockerContainer *string   `json:"docker_container,omitempty"`
	SystemdUnit     *string   `json:"systemd_unit,omitempty"`
	HealthURL       *string   `json:"health_url,omitempty"`
	HealthPath      *string   `json:"health_path,omitempty"`
	ExpectedStatus  []int64   `json:"expected_status"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type orgBackupCheck struct {
	ID             uuid.UUID      `json:"id"`
	TargetType     string         `json:"target_type"`
	TargetID       uuid.UUID      `json:"target_id"`
	Type           string         `json:"type"`
	IntervalSec    int            `json:"interval_sec"`
	TimeoutMS      int            `json:"timeout_ms"`
	Retries        int            `json:"retries"`
	FailAfterSec   int            `json:"fail_after_sec"`
	RecoverAfterOK int            `json:"recover_after_ok"`
	MuteUntil      *time.Time     `json:"mute_until,omitempty"`
	Enabled        bool           `json:"enabled"`
	SeverityRules  datatypes.JSON `json:"severity_rules"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type orgBackupKey struct {
	ID            uuid.UUID  `json:"id"`
	Filename      string     `json:"filename"`
	Ext           string     `json:"ext"`
	ContentEnc    string     `json:"content_enc"`
	SizeBytes     int        `json:"size_bytes"`
	Label         *string    `json:"label,omitempty"`
	Description   *string    `json:"description,omitempty"`
	Fingerprint   *string    `json:"fingerprint,omitempty"`
	NodeID        *uuid.UUID `json:"node_id,omitempty"`
	CreatedByUser *uuid.UUID `json:"created_by_user_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type orgBackupPayload struct {
	Version    string             `json:"version"`
	ExportedAt time.Time          `json:"exported_at"`
	OrgID      uuid.UUID          `json:"org_id"`
	OrgName    string             `json:"org_name"`
	Nodes      []orgBackupNode    `json:"nodes"`
	Services   []orgBackupService `json:"services"`
	Bots       []orgBackupBot     `json:"bots"`
	Checks     []orgBackupCheck   `json:"checks"`
	Keys       []orgBackupKey     `json:"keys"`
}

func (h *Handler) ExportOrgConfig(c *gin.Context) {
	orgID, ok := parseOrgIDFromContext(c)
	if !ok {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	ctx := c.Request.Context()

	var org db.Organization
	if err := h.DB.WithContext(ctx).First(&org, "id = ?", orgID).Error; err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "organization not found")
		return
	}

	var nodes []db.Node
	if err := h.DB.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at").Find(&nodes).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to load nodes")
		return
	}
	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	nodeBackup := make([]orgBackupNode, 0, len(nodes))
	for _, n := range nodes {
		nodeIDs = append(nodeIDs, n.ID)
		nodeBackup = append(nodeBackup, orgBackupNode{
			ID:               n.ID,
			Name:             n.Name,
			Kind:             n.Kind,
			Tags:             append([]string(nil), n.Tags...),
			Host:             n.Host,
			Region:           n.Region,
			Provider:         n.Provider,
			BaseURL:          n.BaseURL,
			PanelUsername:    n.PanelUsername,
			PanelPasswordEnc: n.PanelPasswordEnc,
			Capabilities:     cloneJSON(n.Capabilities),
			AllowedRoots:     append([]string(nil), n.AllowedRoots...),
			IsSandbox:        n.IsSandbox,
			AgentEnabled:     n.AgentEnabled,
			AgentURL:         n.AgentURL,
			AgentTokenEnc:    n.AgentTokenEnc,
			AgentInsecureTLS: n.AgentInsecureTLS,
			IsEnabled:        n.IsEnabled,
			SSHEnabled:       n.SSHEnabled,
			SSHHost:          n.SSHHost,
			SSHPort:          n.SSHPort,
			SSHUser:          n.SSHUser,
			SSHAuthMethod:    n.SSHAuthMethod,
			SSHPasswordEnc:   n.SSHPasswordEnc,
			SSHKeyEnc:        n.SSHKeyEnc,
			VerifyTLS:        n.VerifyTLS,
			CreatedAt:        n.CreatedAt,
			UpdatedAt:        n.UpdatedAt,
		})
	}

	var services []db.Service
	if len(nodeIDs) > 0 {
		if err := h.DB.WithContext(ctx).Where("node_id IN ?", nodeIDs).Order("created_at").Find(&services).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to load services")
			return
		}
	}
	serviceIDs := make([]uuid.UUID, 0, len(services))
	serviceBackup := make([]orgBackupService, 0, len(services))
	for _, s := range services {
		serviceIDs = append(serviceIDs, s.ID)
		serviceBackup = append(serviceBackup, orgBackupService{
			ID:             s.ID,
			NodeID:         s.NodeID,
			Kind:           s.Kind,
			URL:            s.URL,
			Host:           s.Host,
			Port:           s.Port,
			TLSMode:        s.TLSMode,
			HealthPath:     s.HealthPath,
			ExpectedStatus: append([]int64(nil), s.ExpectedStatus...),
			Headers:        cloneJSON(s.Headers),
			AuthRef:        s.AuthRef,
			IsEnabled:      s.IsEnabled,
			CreatedAt:      s.CreatedAt,
			UpdatedAt:      s.UpdatedAt,
		})
	}

	var bots []db.Bot
	if len(nodeIDs) > 0 {
		if err := h.DB.WithContext(ctx).Where("node_id IN ?", nodeIDs).Order("created_at").Find(&bots).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to load bots")
			return
		}
	}
	botIDs := make([]uuid.UUID, 0, len(bots))
	botBackup := make([]orgBackupBot, 0, len(bots))
	for _, b := range bots {
		botIDs = append(botIDs, b.ID)
		botBackup = append(botBackup, orgBackupBot{
			ID:              b.ID,
			NodeID:          b.NodeID,
			Name:            b.Name,
			Kind:            b.Kind,
			DockerContainer: b.DockerContainer,
			SystemdUnit:     b.SystemdUnit,
			HealthURL:       b.HealthURL,
			HealthPath:      b.HealthPath,
			ExpectedStatus:  append([]int64(nil), b.ExpectedStatus...),
			IsEnabled:       b.IsEnabled,
			CreatedAt:       b.CreatedAt,
			UpdatedAt:       b.UpdatedAt,
		})
	}

	var checks []db.Check
	checkBackup := make([]orgBackupCheck, 0)
	if len(nodeIDs) > 0 || len(serviceIDs) > 0 || len(botIDs) > 0 {
		nodeIDs = nonEmptyUUIDs(nodeIDs)
		serviceIDs = nonEmptyUUIDs(serviceIDs)
		botIDs = nonEmptyUUIDs(botIDs)
		if err := h.DB.WithContext(ctx).
			Where("(target_type = 'node' AND target_id IN ?) OR (target_type = 'service' AND target_id IN ?) OR (target_type = 'bot' AND target_id IN ?)", nodeIDs, serviceIDs, botIDs).
			Order("created_at").
			Find(&checks).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to load checks")
			return
		}
		for _, chk := range checks {
			checkBackup = append(checkBackup, orgBackupCheck{
				ID:             chk.ID,
				TargetType:     chk.TargetType,
				TargetID:       chk.TargetID,
				Type:           chk.Type,
				IntervalSec:    chk.IntervalSec,
				TimeoutMS:      chk.TimeoutMS,
				Retries:        chk.Retries,
				FailAfterSec:   chk.FailAfterSec,
				RecoverAfterOK: chk.RecoverAfterOK,
				MuteUntil:      chk.MuteUntil,
				Enabled:        chk.Enabled,
				SeverityRules:  cloneJSON(chk.SeverityRules),
				CreatedAt:      chk.CreatedAt,
				UpdatedAt:      chk.UpdatedAt,
			})
		}
	}

	var keys []db.OrgKey
	if err := h.DB.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at").Find(&keys).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to load keys")
		return
	}
	keyBackup := make([]orgBackupKey, 0, len(keys))
	for _, k := range keys {
		keyBackup = append(keyBackup, orgBackupKey{
			ID:            k.ID,
			Filename:      k.Filename,
			Ext:           k.Ext,
			ContentEnc:    k.ContentEnc,
			SizeBytes:     k.SizeBytes,
			Label:         k.Label,
			Description:   k.Description,
			Fingerprint:   k.Fingerprint,
			NodeID:        k.NodeID,
			CreatedByUser: k.CreatedByUser,
			CreatedAt:     k.CreatedAt,
		})
	}

	payload := orgBackupPayload{
		Version:    "v1",
		ExportedAt: time.Now().UTC(),
		OrgID:      orgID,
		OrgName:    org.Name,
		Nodes:      nodeBackup,
		Services:   serviceBackup,
		Bots:       botBackup,
		Checks:     checkBackup,
		Keys:       keyBackup,
	}

	filename := "org-backup-" + slugifyFilename(org.Name) + "-" + time.Now().UTC().Format("20060102-150405") + ".json"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	respondStatus(c, http.StatusOK, payload)
}

func (h *Handler) ImportOrgConfig(c *gin.Context) {
	orgID, ok := parseOrgIDFromContext(c)
	if !ok {
		respondError(c, http.StatusBadRequest, "INVALID_ORG", "invalid org")
		return
	}
	var payload orgBackupPayload
	if !parseJSONBody(c, &payload) {
		return
	}
	if len(payload.Nodes) == 0 && len(payload.Services) == 0 && len(payload.Bots) == 0 && len(payload.Checks) == 0 && len(payload.Keys) == 0 {
		respondError(c, http.StatusBadRequest, "EMPTY_BACKUP", "backup payload is empty")
		return
	}

	ctx := c.Request.Context()
	nodeMap := make(map[uuid.UUID]uuid.UUID, len(payload.Nodes))
	serviceMap := make(map[uuid.UUID]uuid.UUID, len(payload.Services))
	botMap := make(map[uuid.UUID]uuid.UUID, len(payload.Bots))

	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := purgeOrgBackupData(tx, orgID); err != nil {
			return err
		}
		now := time.Now().UTC()
		emptySecret := ""
		if h.Encryptor != nil {
			if enc, err := h.Encryptor.EncryptString(""); err == nil {
				emptySecret = enc
			}
		}

		for _, row := range payload.Nodes {
			newID := uuid.New()
			nodeMap[row.ID] = newID
			kind := strings.ToUpper(strings.TrimSpace(row.Kind))
			if kind != "HOST" && kind != "SERVER" {
				kind = "SERVER"
			}
			sshPort := row.SSHPort
			if sshPort <= 0 {
				sshPort = 22
			}
			sshAuthMethod := strings.TrimSpace(row.SSHAuthMethod)
			if sshAuthMethod == "" {
				sshAuthMethod = "key"
			}
			panelPasswordEnc := strings.TrimSpace(row.PanelPasswordEnc)
			if panelPasswordEnc == "" {
				panelPasswordEnc = emptySecret
			}
			sshKeyEnc := strings.TrimSpace(row.SSHKeyEnc)
			if sshKeyEnc == "" {
				sshKeyEnc = emptySecret
			}
			createdAt := row.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			updatedAt := row.UpdatedAt
			if updatedAt.IsZero() {
				updatedAt = createdAt
			}
			n := db.Node{
				ID:               newID,
				OrgID:            &orgID,
				Name:             row.Name,
				Kind:             kind,
				Tags:             pq.StringArray(append([]string(nil), row.Tags...)),
				Host:             row.Host,
				Region:           row.Region,
				Provider:         row.Provider,
				BaseURL:          row.BaseURL,
				PanelUsername:    row.PanelUsername,
				PanelPasswordEnc: panelPasswordEnc,
				Capabilities:     cloneJSON(row.Capabilities),
				AllowedRoots:     pq.StringArray(append([]string(nil), row.AllowedRoots...)),
				IsSandbox:        row.IsSandbox,
				AgentEnabled:     row.AgentEnabled,
				AgentURL:         row.AgentURL,
				AgentTokenEnc:    row.AgentTokenEnc,
				AgentInsecureTLS: row.AgentInsecureTLS,
				IsEnabled:        row.IsEnabled,
				SSHEnabled:       row.SSHEnabled,
				SSHHost:          row.SSHHost,
				SSHPort:          sshPort,
				SSHUser:          row.SSHUser,
				SSHAuthMethod:    sshAuthMethod,
				SSHPasswordEnc:   row.SSHPasswordEnc,
				SSHKeyEnc:        sshKeyEnc,
				VerifyTLS:        row.VerifyTLS,
				CreatedAt:        createdAt,
				UpdatedAt:        updatedAt,
			}
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}

		for _, row := range payload.Services {
			newNodeID, ok := nodeMap[row.NodeID]
			if !ok {
				continue
			}
			newID := uuid.New()
			serviceMap[row.ID] = newID
			createdAt := row.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			updatedAt := row.UpdatedAt
			if updatedAt.IsZero() {
				updatedAt = createdAt
			}
			s := db.Service{
				ID:             newID,
				NodeID:         newNodeID,
				Kind:           row.Kind,
				URL:            row.URL,
				Host:           row.Host,
				Port:           row.Port,
				TLSMode:        row.TLSMode,
				HealthPath:     row.HealthPath,
				ExpectedStatus: pq.Int64Array(append([]int64(nil), row.ExpectedStatus...)),
				Headers:        cloneJSON(row.Headers),
				AuthRef:        row.AuthRef,
				IsEnabled:      row.IsEnabled,
				CreatedAt:      createdAt,
				UpdatedAt:      updatedAt,
			}
			if err := tx.Create(&s).Error; err != nil {
				return err
			}
		}

		for _, row := range payload.Bots {
			newNodeID, ok := nodeMap[row.NodeID]
			if !ok {
				continue
			}
			newID := uuid.New()
			botMap[row.ID] = newID
			createdAt := row.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			updatedAt := row.UpdatedAt
			if updatedAt.IsZero() {
				updatedAt = createdAt
			}
			b := db.Bot{
				ID:              newID,
				NodeID:          newNodeID,
				Name:            row.Name,
				Kind:            row.Kind,
				DockerContainer: row.DockerContainer,
				SystemdUnit:     row.SystemdUnit,
				HealthURL:       row.HealthURL,
				HealthPath:      row.HealthPath,
				ExpectedStatus:  pq.Int64Array(append([]int64(nil), row.ExpectedStatus...)),
				IsEnabled:       row.IsEnabled,
				CreatedAt:       createdAt,
				UpdatedAt:       updatedAt,
			}
			if err := tx.Create(&b).Error; err != nil {
				return err
			}
		}

		for _, row := range payload.Checks {
			targetType := strings.ToLower(strings.TrimSpace(row.TargetType))
			targetID := uuid.Nil
			switch targetType {
			case "node":
				targetID = nodeMap[row.TargetID]
			case "service":
				targetID = serviceMap[row.TargetID]
			case "bot":
				targetID = botMap[row.TargetID]
			default:
				continue
			}
			if targetID == uuid.Nil {
				continue
			}
			createdAt := row.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			updatedAt := row.UpdatedAt
			if updatedAt.IsZero() {
				updatedAt = createdAt
			}
			chk := db.Check{
				ID:             uuid.New(),
				TargetType:     targetType,
				TargetID:       targetID,
				Type:           row.Type,
				IntervalSec:    row.IntervalSec,
				TimeoutMS:      row.TimeoutMS,
				Retries:        row.Retries,
				FailAfterSec:   row.FailAfterSec,
				RecoverAfterOK: row.RecoverAfterOK,
				MuteUntil:      row.MuteUntil,
				Enabled:        row.Enabled,
				SeverityRules:  cloneJSON(row.SeverityRules),
				CreatedAt:      createdAt,
				UpdatedAt:      updatedAt,
			}
			if chk.IntervalSec <= 0 {
				chk.IntervalSec = 60
			}
			if chk.TimeoutMS <= 0 {
				chk.TimeoutMS = 3000
			}
			if chk.RecoverAfterOK <= 0 {
				chk.RecoverAfterOK = 2
			}
			if err := tx.Create(&chk).Error; err != nil {
				return err
			}
		}

		for _, row := range payload.Keys {
			var nodeID *uuid.UUID
			if row.NodeID != nil {
				if mapped := nodeMap[*row.NodeID]; mapped != uuid.Nil {
					nodeID = &mapped
				}
			}
			createdAt := row.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			key := db.OrgKey{
				ID:            uuid.New(),
				OrgID:         orgID,
				Filename:      row.Filename,
				Ext:           row.Ext,
				ContentEnc:    row.ContentEnc,
				SizeBytes:     row.SizeBytes,
				Label:         row.Label,
				Description:   row.Description,
				Fingerprint:   row.Fingerprint,
				NodeID:        nodeID,
				CreatedByUser: row.CreatedByUser,
				CreatedAt:     createdAt,
			}
			if err := tx.Create(&key).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_IMPORT", "failed to import organization backup")
		return
	}

	respondStatus(c, http.StatusOK, gin.H{
		"status": "ok",
		"counts": gin.H{
			"nodes":    len(payload.Nodes),
			"services": len(payload.Services),
			"bots":     len(payload.Bots),
			"checks":   len(payload.Checks),
			"keys":     len(payload.Keys),
		},
	})
}

func purgeOrgBackupData(tx *gorm.DB, orgID uuid.UUID) error {
	if tx == nil {
		return nil
	}
	nodeSub := tx.Table("nodes").Select("id").Where("org_id = ?", orgID)
	serviceSub := tx.Table("services").Select("id").Where("node_id IN (?)", nodeSub)
	botSub := tx.Table("bots").Select("id").Where("node_id IN (?)", nodeSub)
	checkSub := tx.Table("checks").Select("id").
		Where("(target_type = 'node' AND target_id IN (?)) OR (target_type = 'service' AND target_id IN (?)) OR (target_type = 'bot' AND target_id IN (?))", nodeSub, serviceSub, botSub)

	if err := tx.Where("check_id IN (?)", checkSub).Delete(&db.CheckResult{}).Error; err != nil {
		return err
	}
	if err := tx.Where("incident_id IN (SELECT id FROM incidents WHERE org_id = ?)", orgID).Delete(&db.AlertState{}).Error; err != nil {
		return err
	}
	if err := tx.Where("org_id = ? OR node_id IN (?) OR service_id IN (?) OR bot_id IN (?)", orgID, nodeSub, serviceSub, botSub).Delete(&db.Incident{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?) OR service_id IN (?) OR bot_id IN (?)", nodeSub, serviceSub, botSub).Delete(&db.AlertState{}).Error; err != nil {
		return err
	}
	if err := tx.Where("id IN (?)", checkSub).Delete(&db.Check{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?)", nodeSub).Delete(&db.Service{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?)", nodeSub).Delete(&db.Bot{}).Error; err != nil {
		return err
	}
	if err := tx.Where("org_id = ?", orgID).Delete(&db.OrgKey{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?)", nodeSub).Delete(&db.NodeCheck{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?)", nodeSub).Delete(&db.NodeMetric{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id IN (?)", nodeSub).Delete(&db.NodeMetricsLatest{}).Error; err != nil {
		return err
	}
	if err := tx.Where("org_id = ?", orgID).Delete(&db.Node{}).Error; err != nil {
		return err
	}
	return nil
}

func parseOrgIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	orgIDRaw := strings.TrimSpace(c.GetString("org_id"))
	if orgIDRaw == "" {
		return uuid.Nil, false
	}
	orgID, err := uuid.Parse(orgIDRaw)
	if err != nil {
		return uuid.Nil, false
	}
	return orgID, true
}

func nonEmptyUUIDs(values []uuid.UUID) []uuid.UUID {
	if len(values) > 0 {
		return values
	}
	return []uuid.UUID{uuid.Nil}
}

func slugifyFilename(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "org"
	}
	repl := strings.NewReplacer(" ", "-", "_", "-", "/", "-", "\\", "-", ":", "-", ";", "-", ",", "-", ".", "-", "\"", "", "'", "", "`", "", "(", "", ")", "", "[", "", "]", "", "{", "", "}", "", "|", "", "*", "", "?", "", "!", "", "@", "", "#", "", "$", "", "%", "", "^", "", "&", "", "+", "-", "=", "-", "~", "")
	s = repl.Replace(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return "org"
	}
	return s
}

func cloneJSON(data datatypes.JSON) datatypes.JSON {
	if len(data) == 0 {
		return datatypes.JSON([]byte("{}"))
	}
	out := make([]byte, len(data))
	copy(out, data)
	return datatypes.JSON(out)
}
