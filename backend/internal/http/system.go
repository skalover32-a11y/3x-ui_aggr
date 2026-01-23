package httpapi

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetSystemStatus(c *gin.Context) {
	if h.DB == nil {
		respondError(c, http.StatusServiceUnavailable, "DB_DISABLED", "database not configured")
		return
	}
	version := strings.TrimSpace(os.Getenv("AGG_VERSION"))
	if version == "" {
		version = strings.TrimSpace(os.Getenv("APP_VERSION"))
	}
	if version == "" {
		version = "unknown"
	}
	var lastSync time.Time
	_ = h.DB.WithContext(c.Request.Context()).
		Table("node_metrics_latest").
		Select("MAX(collected_at)").
		Scan(&lastSync).Error
	resp := map[string]any{
		"status":    "running",
		"version":   version,
		"last_sync": nil,
		"now":       time.Now().UTC().Format(time.RFC3339),
	}
	if !lastSync.IsZero() {
		resp["last_sync"] = lastSync.UTC().Format(time.RFC3339)
	}
	respondStatus(c, http.StatusOK, resp)
}
