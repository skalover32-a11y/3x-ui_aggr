package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type Service struct {
	db *gorm.DB
}

func New(dbConn *gorm.DB) *Service {
	return &Service{db: dbConn}
}

func (s *Service) Write(ctx context.Context, actor string, nodeID *uuid.UUID, action string, payload any, status string, errMsg *string) {
	if s == nil || s.db == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	entry := db.AuditLog{
		Actor:     actor,
		NodeID:    nodeID,
		Action:    action,
		Payload:   datatypes.JSON(raw),
		Status:    status,
		Error:     errMsg,
		CreatedAt: time.Now(),
	}
	_ = s.db.WithContext(ctx).Create(&entry).Error
}
