package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type nodeDiagnosticsMetrics struct {
	CollectedAt    *time.Time `json:"collected_at,omitempty"`
	CPUPct         *float64   `json:"cpu_pct,omitempty"`
	Load1          *float64   `json:"load1,omitempty"`
	Load5          *float64   `json:"load5,omitempty"`
	Load15         *float64   `json:"load15,omitempty"`
	RAMUsedBytes   *int64     `json:"ram_used_bytes,omitempty"`
	RAMTotalBytes  *int64     `json:"ram_total_bytes,omitempty"`
	DiskUsedBytes  *int64     `json:"disk_used_bytes,omitempty"`
	DiskTotalBytes *int64     `json:"disk_total_bytes,omitempty"`
	NetRxBps       *int64     `json:"net_rx_bps,omitempty"`
	NetTxBps       *int64     `json:"net_tx_bps,omitempty"`
	NetRxBytes     *int64     `json:"net_rx_bytes,omitempty"`
	NetTxBytes     *int64     `json:"net_tx_bytes,omitempty"`
	NetIface       *string    `json:"net_iface,omitempty"`
	UptimeSec      *int64     `json:"uptime_sec,omitempty"`
	PingMS         *int64     `json:"ping_ms,omitempty"`
	TCPConnections *int64     `json:"tcp_connections,omitempty"`
	UDPConnections *int64     `json:"udp_connections,omitempty"`
	LastError      *string    `json:"last_error,omitempty"`
}

type nodeDiagnosticsCheck struct {
	TS               time.Time `json:"ts"`
	PanelOK          bool      `json:"panel_ok"`
	SSHOK            bool      `json:"ssh_ok"`
	LatencyMS        int       `json:"latency_ms"`
	Error            *string   `json:"error,omitempty"`
	PanelErrorCode   *string   `json:"panel_error_code,omitempty"`
	PanelErrorDetail *string   `json:"panel_error_detail,omitempty"`
}

type nodeDiagnosticsGroup struct {
	Total          int `json:"total"`
	Enabled        int `json:"enabled"`
	OpenIncidents  int `json:"open_incidents"`
	AckedIncidents int `json:"acked_incidents"`
}

type nodeDiagnosticsIncidents struct {
	Open          int        `json:"open"`
	Acked         int        `json:"acked"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	LastTitle     *string    `json:"last_title,omitempty"`
	LastSeverity  *string    `json:"last_severity,omitempty"`
	LastStatus    *string    `json:"last_status,omitempty"`
	LastAlertType *string    `json:"last_alert_type,omitempty"`
}

type nodeDiagnosticsResponse struct {
	Node      nodeResponse             `json:"node"`
	Metrics   *nodeDiagnosticsMetrics  `json:"metrics,omitempty"`
	LastCheck *nodeDiagnosticsCheck    `json:"last_check,omitempty"`
	Services  nodeDiagnosticsGroup     `json:"services"`
	Bots      nodeDiagnosticsGroup     `json:"bots"`
	Incidents nodeDiagnosticsIncidents `json:"incidents"`
}

func (h *Handler) GetNodeDiagnostics(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}

	resp := nodeDiagnosticsResponse{
		Node: toNodeResponse(node),
	}
	ctx := c.Request.Context()

	metrics, err := h.loadNodeDiagnosticsMetrics(ctx, node.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load diagnostics metrics")
		return
	}
	resp.Metrics = metrics

	lastCheck, err := h.loadNodeDiagnosticsCheck(ctx, node.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load diagnostics check")
		return
	}
	resp.LastCheck = lastCheck

	services, err := h.loadNodeDiagnosticsGroup(ctx, node.ID, "service")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load service diagnostics")
		return
	}
	resp.Services = services

	bots, err := h.loadNodeDiagnosticsGroup(ctx, node.ID, "bot")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load bot diagnostics")
		return
	}
	resp.Bots = bots

	incidents, err := h.loadNodeDiagnosticsIncidents(ctx, node.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load incident diagnostics")
		return
	}
	resp.Incidents = incidents

	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) loadNodeDiagnosticsMetrics(ctx context.Context, nodeID uuid.UUID) (*nodeDiagnosticsMetrics, error) {
	var (
		latest    db.NodeMetricsLatest
		rawMetric db.NodeMetric
	)
	latestErr := h.DB.WithContext(ctx).First(&latest, "node_id = ?", nodeID).Error
	if latestErr != nil && !hasNotFound(latestErr) {
		return nil, latestErr
	}
	rawErr := h.DB.WithContext(ctx).Where("node_id = ?", nodeID).Order("ts desc").First(&rawMetric).Error
	if rawErr != nil && !hasNotFound(rawErr) {
		return nil, rawErr
	}
	if hasNotFound(latestErr) && hasNotFound(rawErr) {
		return nil, nil
	}

	out := &nodeDiagnosticsMetrics{}
	if !hasNotFound(latestErr) {
		ts := latest.CollectedAt
		out.CollectedAt = &ts
		out.CPUPct = latest.CPUPct
		out.RAMUsedBytes = latest.RAMUsedBytes
		out.RAMTotalBytes = latest.RAMTotalBytes
		out.DiskUsedBytes = latest.DiskUsedBytes
		out.DiskTotalBytes = latest.DiskTotalBytes
		out.NetRxBps = latest.NetRxBps
		out.NetTxBps = latest.NetTxBps
		out.NetRxBytes = latest.NetRxBytes
		out.NetTxBytes = latest.NetTxBytes
		out.NetIface = latest.NetIface
		out.UptimeSec = latest.UptimeSec
		out.PingMS = latest.PingMs
		out.TCPConnections = latest.TCPConnections
		out.UDPConnections = latest.UDPConnections
	}
	if !hasNotFound(rawErr) {
		if out.CollectedAt == nil {
			ts := rawMetric.TS
			out.CollectedAt = &ts
		}
		out.Load1 = rawMetric.Load1
		out.Load5 = rawMetric.Load5
		out.Load15 = rawMetric.Load15
		out.LastError = rawMetric.Error
		if out.DiskTotalBytes == nil {
			out.DiskTotalBytes = rawMetric.DiskTotalBytes
		}
		if out.DiskUsedBytes == nil {
			out.DiskUsedBytes = rawMetric.DiskUsedBytes
		}
		if out.NetRxBytes == nil {
			out.NetRxBytes = rawMetric.NetRxBytes
		}
		if out.NetTxBytes == nil {
			out.NetTxBytes = rawMetric.NetTxBytes
		}
		if out.PingMS == nil {
			out.PingMS = rawMetric.PingMs
		}
	}
	return out, nil
}

func (h *Handler) loadNodeDiagnosticsCheck(ctx context.Context, nodeID uuid.UUID) (*nodeDiagnosticsCheck, error) {
	var row db.NodeCheck
	if err := h.DB.WithContext(ctx).Where("node_id = ?", nodeID).Order("ts desc").First(&row).Error; err != nil {
		if hasNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &nodeDiagnosticsCheck{
		TS:               row.TS,
		PanelOK:          row.PanelOK,
		SSHOK:            row.SSHOK,
		LatencyMS:        row.LatencyMS,
		Error:            row.Error,
		PanelErrorCode:   row.PanelErrorCode,
		PanelErrorDetail: row.PanelErrorDetail,
	}, nil
}

func (h *Handler) loadNodeDiagnosticsGroup(ctx context.Context, nodeID uuid.UUID, targetType string) (nodeDiagnosticsGroup, error) {
	out := nodeDiagnosticsGroup{}
	targetTable := "services"
	targetRefColumn := "service_id"
	if targetType == "bot" {
		targetTable = "bots"
		targetRefColumn = "bot_id"
	}
	var total int64
	if err := h.DB.WithContext(ctx).Table(targetTable).Where("node_id = ?", nodeID).Count(&total).Error; err != nil {
		return out, err
	}
	out.Total = int(total)
	var enabled int64
	if err := h.DB.WithContext(ctx).Table(targetTable).Where("node_id = ? AND is_enabled = true", nodeID).Count(&enabled).Error; err != nil {
		return out, err
	}
	out.Enabled = int(enabled)
	targetSub := h.DB.WithContext(ctx).Table(targetTable).Select("id").Where("node_id = ?", nodeID)
	var openIncidents int64
	if err := h.DB.WithContext(ctx).
		Model(&db.Incident{}).
		Where(targetRefColumn+" IN (?)", targetSub).
		Where("status = ?", "open").
		Count(&openIncidents).Error; err != nil {
		return out, err
	}
	out.OpenIncidents = int(openIncidents)
	var ackedIncidents int64
	if err := h.DB.WithContext(ctx).
		Model(&db.Incident{}).
		Where(targetRefColumn+" IN (?)", targetSub).
		Where("status = ?", "acked").
		Count(&ackedIncidents).Error; err != nil {
		return out, err
	}
	out.AckedIncidents = int(ackedIncidents)
	return out, nil
}

func (h *Handler) loadNodeDiagnosticsIncidents(ctx context.Context, nodeID uuid.UUID) (nodeDiagnosticsIncidents, error) {
	out := nodeDiagnosticsIncidents{}
	serviceSub := h.DB.WithContext(ctx).Table("services").Select("id").Where("node_id = ?", nodeID)
	botSub := h.DB.WithContext(ctx).Table("bots").Select("id").Where("node_id = ?", nodeID)
	scope := h.DB.WithContext(ctx).Model(&db.Incident{}).
		Where("node_id = ? OR service_id IN (?) OR bot_id IN (?)", nodeID, serviceSub, botSub)
	var openCount int64
	if err := scope.Where("status = ?", "open").Count(&openCount).Error; err != nil {
		return out, err
	}
	out.Open = int(openCount)
	var ackedCount int64
	if err := scope.Where("status = ?", "acked").Count(&ackedCount).Error; err != nil {
		return out, err
	}
	out.Acked = int(ackedCount)

	var last db.Incident
	if err := h.DB.WithContext(ctx).
		Where("node_id = ? OR service_id IN (?) OR bot_id IN (?)", nodeID, serviceSub, botSub).
		Order("last_seen desc").
		First(&last).Error; err != nil {
		if hasNotFound(err) {
			return out, nil
		}
		return out, err
	}
	seen := last.LastSeen
	out.LastSeenAt = &seen
	out.LastTitle = &last.Title
	out.LastSeverity = &last.Severity
	out.LastStatus = &last.Status
	out.LastAlertType = &last.AlertType
	return out, nil
}

func hasNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
