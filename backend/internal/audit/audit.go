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

func (s *Service) Write(ctx context.Context, actor string, actorUser *string, ip *string, nodeID *uuid.UUID, action string, status string, message *string, payload any, errMsg *string) {
	if s == nil || s.db == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	entry := db.AuditLog{
		TS:          time.Now(),
		Actor:       actor,
		ActorUser:   actorUser,
		IP:          ip,
		NodeID:      nodeID,
		Action:      action,
		Status:      status,
		Message:     message,
		Payload:     datatypes.JSON(raw),
		PayloadJSON: datatypes.JSON(raw),
		Error:       errMsg,
		CreatedAt:   time.Now(),
	}
	_ = s.db.WithContext(ctx).Create(&entry).Error
}
