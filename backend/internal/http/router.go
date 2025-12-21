package httpapi

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/http/middleware"
)

func NewRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	api := r.Group("/api")
	api.POST("/auth/login", h.Login)

	auth := api.Group("")
	auth.Use(middleware.JWTAuth(h.JWTSecret))

	auth.GET("/nodes", h.ListNodes)
	auth.POST("/nodes", h.CreateNode)
	auth.GET("/nodes/:id", h.GetNode)
	auth.PATCH("/nodes/:id", h.UpdateNode)
	auth.DELETE("/nodes/:id", h.DeleteNode)
	auth.POST("/nodes/:id/test", h.TestNode)
	auth.GET("/nodes/:id/status", h.GetNodeStatus)
	auth.GET("/nodes/:id/uptime", h.GetNodeUptime)
	auth.GET("/nodes/:id/metrics", h.GetNodeMetrics)

	auth.GET("/nodes/:id/inbounds", h.ListInbounds)
	auth.POST("/nodes/:id/inbounds", h.AddInbound)
	auth.PATCH("/nodes/:id/inbounds/:inboundId", h.UpdateInbound)
	auth.DELETE("/nodes/:id/inbounds/:inboundId", h.DeleteInbound)

	auth.POST("/nodes/:id/actions/restart-xray", h.RestartXray)
	auth.POST("/nodes/:id/actions/reboot", h.RebootServer)
	auth.POST("/nodes/:id/actions/:action/plan", h.PlanNodeAction)
	auth.POST("/nodes/:id/actions/:action/run", h.RunNodeAction)
	auth.POST("/utils/convert-ssh-key", h.ConvertSSHKey)
	auth.POST("/validate/node", h.ValidateNode)

	return r
}
