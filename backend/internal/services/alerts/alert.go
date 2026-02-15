package alerts

import (
	"time"

	"github.com/google/uuid"
)

type AlertType string

const (
	AlertCPU        AlertType = "cpu"
	AlertMemory     AlertType = "memory"
	AlertDisk       AlertType = "disk"
	AlertConnection AlertType = "connection"
	AlertTLS        AlertType = "tls"
	AlertGeneric    AlertType = "generic"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type AlertMetrics struct {
	Load1      float64
	Threshold  float64
	UsedPct    float64
	FreePct    float64
	LatencyMS  int
	StatusCode int
}
type Alert struct {
	Type        AlertType
	OrgID       *uuid.UUID
	NodeID      uuid.UUID
	ServiceID   uuid.UUID
	BotID       uuid.UUID
	CheckID     uuid.UUID
	NodeName    string
	ServiceKind string
	BotKind     string
	CheckType   string
	Target      string
	TargetType  string
	Status      string
	TS          time.Time
	Severity    Severity
	Metrics     AlertMetrics
	PanelOK     bool
	SSHOK       bool
	PanelURL    string
	IP          string
	Error       string
	AlertID     string
	IncidentID  string
	Fingerprint string
	Occurrences int
	FailAfterSec  int
	RecoverAfterOK int
	MuteUntil      *time.Time
}
