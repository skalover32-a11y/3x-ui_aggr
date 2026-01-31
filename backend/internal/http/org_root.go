package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

const RootOrgName = "VLF Root"

func (h *Handler) EnsureRootOrg(ctx context.Context) (uuid.UUID, error) {
	if h == nil || h.DB == nil {
		return uuid.Nil, errors.New("db not available")
	}
	adminName := strings.TrimSpace(h.AdminUser)
	if adminName == "" {
		return uuid.Nil, errors.New("admin user not configured")
	}
	now := time.Now().UTC()
	var admin db.User
	if err := h.DB.WithContext(ctx).Where("lower(username) = lower(?)", adminName).First(&admin).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(h.AdminPass), bcrypt.DefaultCost)
		if err != nil {
			return uuid.Nil, err
		}
		admin = db.User{
			ID:           uuid.New(),
			Username:     adminName,
			PasswordHash: string(hash),
			Role:         middleware.RoleAdmin,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := h.DB.WithContext(ctx).Create(&admin).Error; err != nil {
			return uuid.Nil, err
		}
	} else if admin.Role != middleware.RoleAdmin {
		_ = h.DB.WithContext(ctx).Model(&db.User{}).
			Where("id = ?", admin.ID).
			Update("role", middleware.RoleAdmin).Error
	}

	var org db.Organization
	if err := h.DB.WithContext(ctx).Where("name = ?", RootOrgName).First(&org).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, err
		}
		org = db.Organization{
			ID:          uuid.New(),
			Name:        RootOrgName,
			OwnerUserID: admin.ID,
			CreatedAt:   now,
		}
		if err := h.DB.WithContext(ctx).Create(&org).Error; err != nil {
			return uuid.Nil, err
		}
	}

	var member db.OrganizationMember
	if err := h.DB.WithContext(ctx).First(&member, "org_id = ? AND user_id = ?", org.ID, admin.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			member = db.OrganizationMember{
				OrgID:     org.ID,
				UserID:    admin.ID,
				Role:      "owner",
				CreatedAt: now,
			}
			if err := h.DB.WithContext(ctx).Create(&member).Error; err != nil {
				return uuid.Nil, err
			}
		} else {
			return uuid.Nil, err
		}
	}

	if err := h.DB.WithContext(ctx).Model(&db.Node{}).
		Where("org_id IS NULL").
		Update("org_id", org.ID).Error; err != nil {
		return uuid.Nil, err
	}

	return org.ID, nil
}
