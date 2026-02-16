package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"agr_3x_ui/internal/db"
)

const (
	defaultAckMuteMinutes = 24 * 60
	defaultMuteMinutes    = 60
	maxPolicyMinutes      = 30 * 24 * 60
)

func normalizeAlertPolicyMinutes(ackMuteMinutes, muteMinutes int) (int, int) {
	ack := ackMuteMinutes
	if ack <= 0 {
		ack = defaultAckMuteMinutes
	}
	if ack > maxPolicyMinutes {
		ack = maxPolicyMinutes
	}
	mute := muteMinutes
	if mute <= 0 {
		mute = defaultMuteMinutes
	}
	if mute > maxPolicyMinutes {
		mute = maxPolicyMinutes
	}
	return ack, mute
}

func (h *Handler) alertPolicyForOrg(ctx context.Context, orgID *uuid.UUID) (int, int) {
	ack, mute := normalizeAlertPolicyMinutes(0, 0)
	if h == nil || h.DB == nil || orgID == nil || *orgID == uuid.Nil {
		return ack, mute
	}
	var row db.TelegramSettings
	if err := h.DB.WithContext(ctx).
		Where("org_id = ?", *orgID).
		Order("created_at desc").
		First(&row).Error; err != nil {
		return ack, mute
	}
	return normalizeAlertPolicyMinutes(row.AckMuteMinutes, row.MuteMinutes)
}

func (h *Handler) orgIDForIncident(ctx context.Context, row *db.Incident) *uuid.UUID {
	if row == nil || h == nil || h.DB == nil {
		return nil
	}
	if row.OrgID != nil && *row.OrgID != uuid.Nil {
		id := *row.OrgID
		return &id
	}
	if row.NodeID != nil && *row.NodeID != uuid.Nil {
		var node db.Node
		if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", *row.NodeID).Error; err == nil && node.OrgID != nil {
			id := *node.OrgID
			return &id
		}
	}
	if row.ServiceID != nil && *row.ServiceID != uuid.Nil {
		var svc db.Service
		if err := h.DB.WithContext(ctx).Select("node_id").First(&svc, "id = ?", *row.ServiceID).Error; err == nil {
			var node db.Node
			if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", svc.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	if row.BotID != nil && *row.BotID != uuid.Nil {
		var bot db.Bot
		if err := h.DB.WithContext(ctx).Select("node_id").First(&bot, "id = ?", *row.BotID).Error; err == nil {
			var node db.Node
			if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", bot.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	return nil
}

func (h *Handler) orgIDForAlertState(ctx context.Context, row *db.AlertState) *uuid.UUID {
	if row == nil || h == nil || h.DB == nil {
		return nil
	}
	if row.IncidentID != nil && *row.IncidentID != uuid.Nil {
		var incident db.Incident
		if err := h.DB.WithContext(ctx).Select("org_id").First(&incident, "id = ?", *row.IncidentID).Error; err == nil && incident.OrgID != nil {
			id := *incident.OrgID
			return &id
		}
	}
	incidentByFingerprint := strings.TrimSpace(row.Fingerprint)
	if incidentByFingerprint != "" {
		var incident db.Incident
		if err := h.DB.WithContext(ctx).Select("org_id").First(&incident, "fingerprint = ?", incidentByFingerprint).Error; err == nil && incident.OrgID != nil {
			id := *incident.OrgID
			return &id
		}
	}
	if row.NodeID != nil && *row.NodeID != uuid.Nil {
		var node db.Node
		if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", *row.NodeID).Error; err == nil && node.OrgID != nil {
			id := *node.OrgID
			return &id
		}
	}
	if row.ServiceID != nil && *row.ServiceID != uuid.Nil {
		var svc db.Service
		if err := h.DB.WithContext(ctx).Select("node_id").First(&svc, "id = ?", *row.ServiceID).Error; err == nil {
			var node db.Node
			if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", svc.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	if row.BotID != nil && *row.BotID != uuid.Nil {
		var bot db.Bot
		if err := h.DB.WithContext(ctx).Select("node_id").First(&bot, "id = ?", *row.BotID).Error; err == nil {
			var node db.Node
			if err := h.DB.WithContext(ctx).Select("org_id").First(&node, "id = ?", bot.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	return nil
}

func parseBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
