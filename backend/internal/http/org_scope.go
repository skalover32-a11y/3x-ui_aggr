package httpapi

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

func (h *Handler) actorIsGlobalAdmin(c *gin.Context) bool {
	actor := getActor(c)
	return h != nil && h.AdminUser != "" && strings.EqualFold(actor, h.AdminUser)
}

func (h *Handler) actorUser(c *gin.Context) (*db.User, error) {
	if h == nil || h.DB == nil {
		return nil, errors.New("db not available")
	}
	actor := getActor(c)
	if actor == "" {
		return nil, errors.New("missing actor")
	}
	var user db.User
	if err := h.DB.WithContext(c.Request.Context()).First(&user, "lower(username) = lower(?)", actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && h.actorIsGlobalAdmin(c) {
			// Self-heal legacy env-admin sessions where user row/org membership was not created yet.
			if _, ensureErr := h.EnsureRootOrg(c.Request.Context()); ensureErr == nil {
				if retryErr := h.DB.WithContext(c.Request.Context()).First(&user, "lower(username) = lower(?)", actor).Error; retryErr == nil {
					return &user, nil
				}
			}
		}
		return nil, err
	}
	return &user, nil
}

func (h *Handler) scopedNodesQuery(c *gin.Context) (*gorm.DB, error) {
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		return nil, err
	}
	return h.DB.WithContext(c.Request.Context()).
		Model(&db.Node{}).
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
		Where("nodes.org_id IS NOT NULL").
		Scopes(func(tx *gorm.DB) *gorm.DB {
			if orgID == nil {
				return tx
			}
			return tx.Where("nodes.org_id = ?", *orgID)
		}), nil
}

func (h *Handler) getNodeForActor(c *gin.Context, idStr string) (*db.Node, error) {
	nodeID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	query, err := h.scopedNodesQuery(c)
	if err != nil {
		return nil, err
	}
	var node db.Node
	if err := query.Where("nodes.id = ?", nodeID).First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (h *Handler) getServiceForActor(c *gin.Context, idStr string) (*db.Service, error) {
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	query := h.DB.WithContext(c.Request.Context()).Model(&db.Service{})
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		return nil, err
	}
	var service db.Service
	if err := query.
		Joins("JOIN organization_members om ON om.org_id = services.org_id").
		Where("om.user_id = ?", user.ID).
		Scopes(func(tx *gorm.DB) *gorm.DB {
			if orgID == nil {
				return tx
			}
			return tx.Where("services.org_id = ?", *orgID)
		}).
		Where("services.id = ?", serviceID).
		First(&service).Error; err != nil {
		return nil, err
	}
	return &service, nil
}

func (h *Handler) getBotForActor(c *gin.Context, idStr string) (*db.Bot, error) {
	botID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	query := h.DB.WithContext(c.Request.Context()).Model(&db.Bot{})
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		return nil, err
	}
	var bot db.Bot
	if err := query.
		Joins("JOIN nodes ON nodes.id = bots.node_id").
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
		Scopes(func(tx *gorm.DB) *gorm.DB {
			if orgID == nil {
				return tx
			}
			return tx.Where("nodes.org_id = ?", *orgID)
		}).
		Where("bots.id = ?", botID).
		First(&bot).Error; err != nil {
		return nil, err
	}
	return &bot, nil
}

func (h *Handler) getCheckForActor(c *gin.Context, idStr string) (*db.Check, error) {
	checkID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	var check db.Check
	if err := h.DB.WithContext(c.Request.Context()).First(&check, "id = ?", checkID).Error; err != nil {
		return nil, err
	}
	switch check.TargetType {
	case "node":
		if _, err := h.getNodeForActor(c, check.TargetID.String()); err != nil {
			return nil, err
		}
	case "service":
		if _, err := h.getServiceForActor(c, check.TargetID.String()); err != nil {
			return nil, err
		}
	case "bot":
		if _, err := h.getBotForActor(c, check.TargetID.String()); err != nil {
			return nil, err
		}
	default:
		return nil, gorm.ErrRecordNotFound
	}
	return &check, nil
}

func (h *Handler) accessibleNodeIDs(c *gin.Context) (map[uuid.UUID]struct{}, error) {
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	orgID, err := h.orgIDFromRequest(c, user.ID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	query := h.DB.WithContext(c.Request.Context()).
		Table("nodes").
		Select("nodes.id").
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
		Where("nodes.org_id IS NOT NULL")
	if orgID != nil {
		query = query.Where("nodes.org_id = ?", *orgID)
	}
	if err := query.Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func (h *Handler) orgIDFromRequest(c *gin.Context, userID uuid.UUID) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.GetHeader("X-Org-ID"))
	if raw == "" {
		raw = strings.TrimSpace(c.Query("org_id"))
	}
	if raw != "" {
		orgID, err := uuid.Parse(raw)
		if err == nil {
			var member db.OrganizationMember
			if err := h.DB.WithContext(c.Request.Context()).
				Where("org_id = ? AND user_id = ?", orgID, userID).
				First(&member).Error; err == nil {
				return &orgID, nil
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}
		// For stale/invalid/foreign org id: fall back to a deterministic org instead of "all orgs".
	}
	firstOrg, err := h.firstOrgForUser(c, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &firstOrg, nil
}

func (h *Handler) firstOrgForUser(c *gin.Context, userID uuid.UUID) (uuid.UUID, error) {
	var member db.OrganizationMember
	if err := h.DB.WithContext(c.Request.Context()).
		Where("user_id = ?", userID).
		Order("created_at").
		First(&member).Error; err != nil {
		return uuid.Nil, err
	}
	return member.OrgID, nil
}
