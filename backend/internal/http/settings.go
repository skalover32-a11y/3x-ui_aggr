package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetAgentDeployDefaults(c *gin.Context) {
	allow := strings.TrimSpace(h.AllowCIDR)
	respondStatus(c, http.StatusOK, gin.H{
		"default_allow_cidr":           allow,
		"default_agent_port":           9191,
		"default_stats_mode":           "log",
		"default_xray_access_log_path": "/var/log/xray/access.log",
		"default_rate_limit_rps":       5,
		"default_health_check":         true,
		"default_enable_ufw":           false,
		"default_parallelism":          3,
	})
}
