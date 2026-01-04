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
	api.POST("/auth/2fa/recovery", h.SendRecoveryCode)
	api.GET("/nodes/:id/ssh", h.SSHWebsocket)
	api.POST("/telegram/webhook", h.TelegramWebhook)

	auth := api.Group("")
	auth.Use(middleware.JWTAuth(h.JWTSecret))

	readRoles := []string{middleware.RoleAdmin, middleware.RoleOperator, middleware.RoleViewer}
	writeRoles := []string{middleware.RoleAdmin, middleware.RoleOperator}

	auth.GET("/nodes", middleware.RequireRoles(readRoles...), h.ListNodes)
	auth.GET("/nodes/:id", middleware.RequireRoles(readRoles...), h.GetNode)
	auth.GET("/nodes/:id/status", middleware.RequireRoles(readRoles...), h.GetNodeStatus)
	auth.GET("/nodes/:id/uptime", middleware.RequireRoles(readRoles...), h.GetNodeUptime)
	auth.GET("/nodes/:id/metrics", middleware.RequireRoles(readRoles...), h.GetNodeMetrics)


	auth.GET("/nodes/:id/services", middleware.RequireRoles(readRoles...), h.ListServices)
	auth.POST("/nodes/:id/services", middleware.RequireRoles(writeRoles...), h.CreateService)
	auth.PATCH("/nodes/:id/services/:serviceId", middleware.RequireRoles(writeRoles...), h.UpdateService)
	auth.DELETE("/nodes/:id/services/:serviceId", middleware.RequireRoles(writeRoles...), h.DeleteService)
	auth.PUT("/services/:service_id", middleware.RequireRoles(writeRoles...), h.UpdateService)
	auth.DELETE("/services/:service_id", middleware.RequireRoles(writeRoles...), h.DeleteService)
	auth.POST("/services/:service_id/run", middleware.RequireRoles(writeRoles...), h.RunServiceCheck)
	auth.GET("/services/:service_id/results", middleware.RequireRoles(readRoles...), h.ListServiceResults)

	auth.GET("/nodes/:id/checks", middleware.RequireRoles(readRoles...), h.ListNodeChecks)
	auth.POST("/nodes/:id/checks", middleware.RequireRoles(writeRoles...), h.CreateNodeCheck)
	auth.GET("/services/:service_id/checks", middleware.RequireRoles(readRoles...), h.ListServiceChecks)
	auth.POST("/services/:service_id/checks", middleware.RequireRoles(writeRoles...), h.CreateServiceCheck)
	auth.PATCH("/checks/:id", middleware.RequireRoles(writeRoles...), h.UpdateCheck)
	auth.DELETE("/checks/:id", middleware.RequireRoles(writeRoles...), h.DeleteCheck)
	auth.GET("/checks/:id/results", middleware.RequireRoles(readRoles...), h.ListCheckResults)

	auth.POST("/nodes", middleware.RequireRoles(writeRoles...), h.CreateNode)
	auth.PATCH("/nodes/:id", middleware.RequireRoles(writeRoles...), h.UpdateNode)
	auth.POST("/nodes/:id/test", middleware.RequireRoles(writeRoles...), h.TestNode)
	auth.POST("/validate/node", middleware.RequireRoles(writeRoles...), h.ValidateNode)
	auth.POST("/utils/convert-ssh-key", middleware.RequireRoles(writeRoles...), h.ConvertSSHKey)

	auth.DELETE("/nodes/:id", middleware.RequireRoles(middleware.RoleAdmin), h.DeleteNode)

	auth.GET("/nodes/:id/inbounds", middleware.RequireRoles(writeRoles...), h.ListInbounds)
	auth.POST("/nodes/:id/inbounds", middleware.RequireRoles(writeRoles...), h.AddInbound)
	auth.PATCH("/nodes/:id/inbounds/:inboundId", middleware.RequireRoles(writeRoles...), h.UpdateInbound)
	auth.DELETE("/nodes/:id/inbounds/:inboundId", middleware.RequireRoles(writeRoles...), h.DeleteInbound)

	auth.POST("/nodes/:id/actions/restart-xray", middleware.RequireRoles(writeRoles...), h.RestartXray)
	auth.POST("/nodes/:id/actions/reboot", middleware.RequireRoles(writeRoles...), h.RebootServer)
	auth.POST("/nodes/:id/actions/:action/plan", middleware.RequireRoles(writeRoles...), h.PlanNodeAction)
	auth.POST("/nodes/:id/actions/:action/run", middleware.RequireRoles(writeRoles...), h.RunNodeAction)

	auth.GET("/audit", middleware.RequireRoles(middleware.RoleAdmin), h.ListAuditLogs)
	auth.GET("/alerts", middleware.RequireRoles(readRoles...), h.ListAlerts)
	auth.GET("/telegram/settings", middleware.RequireRoles(middleware.RoleAdmin), h.GetTelegramSettings)
	auth.PUT("/telegram/settings", middleware.RequireRoles(middleware.RoleAdmin), h.UpdateTelegramSettings)
	auth.POST("/telegram/test", middleware.RequireRoles(middleware.RoleAdmin), h.SendTelegramTest)
	auth.POST("/alerts/:fingerprint/mute", middleware.RequireRoles(writeRoles...), h.MuteAlert)
	auth.POST("/alerts/:fingerprint/retry", middleware.RequireRoles(writeRoles...), h.RetryAlert)

	auth.GET("/users", middleware.RequireRoles(middleware.RoleAdmin), h.ListUsers)
	auth.POST("/users", middleware.RequireRoles(middleware.RoleAdmin), h.CreateUser)
	auth.PATCH("/users/:id", middleware.RequireRoles(middleware.RoleAdmin), h.UpdateUser)
	auth.DELETE("/users/:id", middleware.RequireRoles(middleware.RoleAdmin), h.DeleteUser)

	auth.GET("/auth/2fa/status", middleware.RequireRoles(readRoles...), h.GetTOTPStatus)
	auth.POST("/auth/2fa/setup", middleware.RequireRoles(writeRoles...), h.SetupTOTP)
	auth.POST("/auth/2fa/verify", middleware.RequireRoles(writeRoles...), h.VerifyTOTP)
	auth.POST("/auth/2fa/disable", middleware.RequireRoles(writeRoles...), h.DisableTOTP)

	return r
}
