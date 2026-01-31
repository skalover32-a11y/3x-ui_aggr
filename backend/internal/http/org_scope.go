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
	if err := h.DB.WithContext(c.Request.Context()).First(&user, "username = ?", actor).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (h *Handler) scopedNodesQuery(c *gin.Context) (*gorm.DB, error) {
	if h.actorIsGlobalAdmin(c) {
		return h.DB.WithContext(c.Request.Context()).Model(&db.Node{}), nil
	}
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	return h.DB.WithContext(c.Request.Context()).
		Model(&db.Node{}).
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
		Where("nodes.org_id IS NOT NULL"), nil
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
	if h.actorIsGlobalAdmin(c) {
		var service db.Service
		if err := query.First(&service, "id = ?", serviceID).Error; err != nil {
			return nil, err
		}
		return &service, nil
	}
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	var service db.Service
	if err := query.
		Joins("JOIN nodes ON nodes.id = services.node_id").
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
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
	if h.actorIsGlobalAdmin(c) {
		var bot db.Bot
		if err := query.First(&bot, "id = ?", botID).Error; err != nil {
			return nil, err
		}
		return &bot, nil
	}
	user, err := h.actorUser(c)
	if err != nil {
		return nil, err
	}
	var bot db.Bot
	if err := query.
		Joins("JOIN nodes ON nodes.id = bots.node_id").
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
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
	if h.actorIsGlobalAdmin(c) {
		return &check, nil
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

func (h *Handler) accessibleNodeIDs(c *gin.Context) (map[uuid.UUID]struct{}, bool, error) {
	if h.actorIsGlobalAdmin(c) {
		return nil, true, nil
	}
	user, err := h.actorUser(c)
	if err != nil {
		return nil, false, err
	}
	var ids []uuid.UUID
	if err := h.DB.WithContext(c.Request.Context()).
		Table("nodes").
		Select("nodes.id").
		Joins("JOIN organization_members om ON om.org_id = nodes.org_id").
		Where("om.user_id = ?", user.ID).
		Where("nodes.org_id IS NOT NULL").
		Scan(&ids).Error; err != nil {
		return nil, false, err
	}
	out := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, false, nil
}
