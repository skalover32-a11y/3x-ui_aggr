package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type nodeStatusResponse struct {
	NodeID           string     `json:"node_id"`
	PanelOK          bool       `json:"panel_ok"`
	SSHOK            bool       `json:"ssh_ok"`
	LatencyMS        int        `json:"latency_ms"`
	Error            *string    `json:"error"`
	PanelErrorCode   *string    `json:"panel_error_code"`
	PanelErrorDetail *string    `json:"panel_error_detail"`
	TS               *time.Time `json:"ts"`
	Status           string     `json:"status"`
}

type nodeUptimePoint struct {
	TS               time.Time `json:"ts"`
	PanelOK          bool      `json:"panel_ok"`
	SSHOK            bool      `json:"ssh_ok"`
	LatencyMS        int       `json:"latency_ms"`
	Error            *string   `json:"error"`
	PanelErrorCode   *string   `json:"panel_error_code"`
	PanelErrorDetail *string   `json:"panel_error_detail"`
}

type nodeMetricPoint struct {
	TS                time.Time `json:"ts"`
	Load1             *float64  `json:"load1"`
	Load5             *float64  `json:"load5"`
	Load15            *float64  `json:"load15"`
	MemTotalBytes     *int64    `json:"mem_total_bytes"`
	MemAvailableBytes *int64    `json:"mem_available_bytes"`
	DiskTotalBytes    *int64    `json:"disk_total_bytes"`
	DiskUsedBytes     *int64    `json:"disk_used_bytes"`
	Error             *string   `json:"error"`
}

func (h *Handler) GetNodeStatus(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var check db.NodeCheck
	err = h.DB.WithContext(c.Request.Context()).Where("node_id = ?", node.ID).Order("ts desc").First(&check).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondStatus(c, http.StatusOK, nodeStatusResponse{
				NodeID: node.ID.String(),
				Status: "unknown",
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load status")
		return
	}
	status := deriveStatus(check.PanelOK, check.SSHOK)
	ts := check.TS
	respondStatus(c, http.StatusOK, nodeStatusResponse{
		NodeID:           node.ID.String(),
		PanelOK:          check.PanelOK,
		SSHOK:            check.SSHOK,
		LatencyMS:        check.LatencyMS,
		Error:            check.Error,
		PanelErrorCode:   check.PanelErrorCode,
		PanelErrorDetail: check.PanelErrorDetail,
		TS:               &ts,
		Status:           status,
	})
}

func (h *Handler) GetNodeUptime(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	minutes := 60
	if raw := c.Query("minutes"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			minutes = val
		}
	}
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 1440 {
		minutes = 1440
	}
	from := time.Now().Add(-time.Duration(minutes) * time.Minute)
	var rows []db.NodeCheck
	if err := h.DB.WithContext(c.Request.Context()).Where("node_id = ? AND ts >= ?", node.ID, from).Order("ts asc").Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load uptime")
		return
	}
	resp := make([]nodeUptimePoint, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, nodeUptimePoint{
			TS:               row.TS,
			PanelOK:          row.PanelOK,
			SSHOK:            row.SSHOK,
			LatencyMS:        row.LatencyMS,
			Error:            row.Error,
			PanelErrorCode:   row.PanelErrorCode,
			PanelErrorDetail: row.PanelErrorDetail,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) GetNodeMetrics(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	minutes := 1440
	if raw := c.Query("minutes"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			minutes = val
		}
	}
	if minutes < 5 {
		minutes = 5
	}
	maxMinutes := 60 * 24 * 30
	if minutes > maxMinutes {
		minutes = maxMinutes
	}
	from := time.Now().Add(-time.Duration(minutes) * time.Minute)
	var rows []db.NodeMetric
	if err := h.DB.WithContext(c.Request.Context()).Where("node_id = ? AND ts >= ?", node.ID, from).Order("ts asc").Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to load metrics")
		return
	}
	resp := make([]nodeMetricPoint, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, nodeMetricPoint{
			TS:                row.TS,
			Load1:             row.Load1,
			Load5:             row.Load5,
			Load15:            row.Load15,
			MemTotalBytes:     row.MemTotalBytes,
			MemAvailableBytes: row.MemAvailableBytes,
			DiskTotalBytes:    row.DiskTotalBytes,
			DiskUsedBytes:     row.DiskUsedBytes,
			Error:             row.Error,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

func deriveStatus(panelOK, sshOK bool) string {
	if panelOK && sshOK {
		return "online"
	}
	if panelOK || sshOK {
		return "degraded"
	}
	return "offline"
}
