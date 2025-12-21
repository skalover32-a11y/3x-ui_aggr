package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
)

type Node struct {
	ID                uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name              string         `gorm:"type:text;not null" json:"name"`
	Tags              pq.StringArray `gorm:"type:text[]" json:"tags"`
	BaseURL           string         `gorm:"type:text;not null" json:"base_url"`
	PanelUsername     string         `gorm:"type:text;not null" json:"panel_username"`
	PanelPasswordEnc  string         `gorm:"type:text;not null" json:"-"`
	SSHHost           string         `gorm:"type:text;not null" json:"ssh_host"`
	SSHPort           int            `gorm:"type:int;not null" json:"ssh_port"`
	SSHUser           string         `gorm:"type:text;not null" json:"ssh_user"`
	SSHKeyEnc         string         `gorm:"type:text;not null" json:"-"`
	VerifyTLS         bool           `gorm:"not null;default:true" json:"verify_tls"`
	XrayVersion       *string        `gorm:"type:text" json:"xray_version"`
	PanelVersion      *string        `gorm:"type:text" json:"panel_version"`
	VersionsCheckedAt *time.Time     `json:"versions_checked_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type AuditLog struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Actor     string         `gorm:"type:text;not null" json:"actor"`
	NodeID    *uuid.UUID     `gorm:"type:uuid" json:"node_id"`
	Action    string         `gorm:"type:text;not null" json:"action"`
	Payload   datatypes.JSON `gorm:"type:jsonb;not null" json:"payload"`
	Status    string         `gorm:"type:text;not null" json:"status"`
	Error     *string        `gorm:"type:text" json:"error"`
	CreatedAt time.Time      `json:"created_at"`
}

type NodeCheck struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID    uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	TS        time.Time `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	PanelOK   bool      `gorm:"not null" json:"panel_ok"`
	SSHOK     bool      `gorm:"not null" json:"ssh_ok"`
	LatencyMS int       `gorm:"type:int" json:"latency_ms"`
	Error     *string   `gorm:"type:text" json:"error"`
}

type NodeMetric struct {
	ID                uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID            uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	TS                time.Time `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	Load1             *float64  `json:"load1"`
	Load5             *float64  `json:"load5"`
	Load15            *float64  `json:"load15"`
	MemTotalBytes     *int64    `json:"mem_total_bytes"`
	MemAvailableBytes *int64    `json:"mem_available_bytes"`
	DiskTotalBytes    *int64    `json:"disk_total_bytes"`
	DiskUsedBytes     *int64    `json:"disk_used_bytes"`
	Error             *string   `json:"error"`
}
