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
}

type Alert struct {
	Type        AlertType
	NodeID      uuid.UUID
	NodeName    string
	TS          time.Time
	Severity    Severity
	Metrics     AlertMetrics
	PanelOK     bool
	SSHOK       bool
	PanelURL    string
	IP          string
	Error       string
	AlertID     string
	Fingerprint string
	Occurrences int
}
