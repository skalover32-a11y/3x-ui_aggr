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
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	api := r.Group("/api")
	api.POST("/auth/login", h.Login)
	api.POST("/auth/refresh", h.Refresh)
	api.POST("/auth/logout", h.Logout)
	api.POST("/auth/2fa/recovery", h.SendRecoveryCode)
	api.POST("/auth/webauthn/login/options", h.WebAuthnLoginOptions)
	api.POST("/auth/webauthn/login/verify", h.WebAuthnLoginVerify)
	api.GET("/nodes/:id/ssh", h.SSHWebsocket)
	api.POST("/telegram/webhook", h.TelegramWebhook)
	api.GET("/healthz", h.Healthz)

	auth := api.Group("")
	auth.Use(middleware.JWTAuth(h.JWTSecret))

	readRoles := []string{middleware.RoleAdmin, middleware.RoleOperator, middleware.RoleViewer}
	writeRoles := []string{middleware.RoleAdmin, middleware.RoleOperator}

	auth.GET("/nodes", middleware.RequireRoles(readRoles...), h.ListNodes)
	auth.GET("/nodes/:id", middleware.RequireRoles(readRoles...), h.GetNode)
	auth.GET("/nodes/:id/status", middleware.RequireRoles(readRoles...), h.GetNodeStatus)
	auth.GET("/nodes/:id/uptime", middleware.RequireRoles(readRoles...), h.GetNodeUptime)
	auth.GET("/nodes/:id/metrics", middleware.RequireRoles(readRoles...), h.GetNodeMetrics)
	auth.GET("/nodes/:id/files/roots", middleware.RequireRoles(readRoles...), h.ListFileRoots)
	auth.GET("/nodes/:id/files/list", middleware.RequireRoles(readRoles...), h.ListFiles)
	auth.GET("/nodes/:id/files/read", middleware.RequireRoles(readRoles...), h.ReadFileChunk)
	auth.GET("/nodes/:id/files/tail", middleware.RequireRoles(readRoles...), h.TailFile)
	auth.GET("/nodes/:id/files/download", middleware.RequireRoles(readRoles...), h.DownloadFile)
	auth.POST("/nodes/:id/files/upload", middleware.RequireRoles(writeRoles...), h.UploadFile)
	auth.POST("/nodes/:id/files/mkdir", middleware.RequireRoles(writeRoles...), h.Mkdir)
	auth.POST("/nodes/:id/files/rename", middleware.RequireRoles(writeRoles...), h.RenamePath)
	auth.POST("/nodes/:id/files/delete", middleware.RequireRoles(writeRoles...), h.DeletePath)

	auth.GET("/nodes/:id/services", middleware.RequireRoles(readRoles...), h.ListServices)
	auth.POST("/nodes/:id/services", middleware.RequireRoles(writeRoles...), h.CreateService)
	auth.GET("/nodes/:id/bots", middleware.RequireRoles(readRoles...), h.ListBots)
	auth.POST("/nodes/:id/bots", middleware.RequireRoles(writeRoles...), h.CreateBot)
	auth.GET("/services", middleware.RequireRoles(readRoles...), h.ListAllServices)
	auth.POST("/services", middleware.RequireRoles(writeRoles...), h.CreateServiceGlobal)
	auth.PATCH("/nodes/:id/services/:serviceId", middleware.RequireRoles(writeRoles...), h.UpdateService)
	auth.DELETE("/nodes/:id/services/:serviceId", middleware.RequireRoles(writeRoles...), h.DeleteService)
	auth.PUT("/services/:service_id", middleware.RequireRoles(writeRoles...), h.UpdateService)
	auth.DELETE("/services/:service_id", middleware.RequireRoles(writeRoles...), h.DeleteService)
	auth.POST("/services/:service_id/run", middleware.RequireRoles(writeRoles...), h.RunServiceCheck)
	auth.GET("/services/:service_id/results", middleware.RequireRoles(readRoles...), h.ListServiceResults)
	auth.GET("/bots/:bot_id", middleware.RequireRoles(readRoles...), h.GetBot)
	auth.PUT("/bots/:bot_id", middleware.RequireRoles(writeRoles...), h.UpdateBot)
	auth.DELETE("/bots/:bot_id", middleware.RequireRoles(writeRoles...), h.DeleteBot)
	auth.POST("/bots/:bot_id/run-now", middleware.RequireRoles(writeRoles...), h.RunBotCheck)
	auth.GET("/bots/:bot_id/results", middleware.RequireRoles(readRoles...), h.ListBotResults)

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

	auth.POST("/ops/jobs", middleware.RequireRoles(writeRoles...), h.CreateOpsJob)
	auth.GET("/ops/jobs/:id", middleware.RequireRoles(readRoles...), h.GetOpsJob)
	auth.GET("/ops/jobs/:id/items", middleware.RequireRoles(readRoles...), h.GetOpsJobItems)
	auth.GET("/ops/jobs/:id/stream", middleware.RequireRoles(readRoles...), h.OpsJobStream)

	auth.GET("/users", middleware.RequireRoles(middleware.RoleAdmin), h.ListUsers)
	auth.POST("/users", middleware.RequireRoles(middleware.RoleAdmin), h.CreateUser)
	auth.PATCH("/users/:id", middleware.RequireRoles(middleware.RoleAdmin), h.UpdateUser)
	auth.DELETE("/users/:id", middleware.RequireRoles(middleware.RoleAdmin), h.DeleteUser)

	auth.GET("/auth/2fa/status", middleware.RequireRoles(readRoles...), h.GetTOTPStatus)
	auth.POST("/auth/2fa/setup", middleware.RequireRoles(writeRoles...), h.SetupTOTP)
	auth.POST("/auth/2fa/verify", middleware.RequireRoles(writeRoles...), h.VerifyTOTP)
	auth.POST("/auth/2fa/disable", middleware.RequireRoles(writeRoles...), h.DisableTOTP)
	auth.POST("/auth/webauthn/register/options", middleware.RequireRoles(writeRoles...), h.WebAuthnRegisterOptions)
	auth.POST("/auth/webauthn/register/verify", middleware.RequireRoles(writeRoles...), h.WebAuthnRegisterVerify)
	auth.GET("/auth/webauthn/credentials", middleware.RequireRoles(readRoles...), h.ListWebAuthnCredentials)
	auth.DELETE("/auth/webauthn/credentials/:id", middleware.RequireRoles(writeRoles...), h.DeleteWebAuthnCredential)

	return r
}
