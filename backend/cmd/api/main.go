package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"
	"net/http"
	"net/url"

	"agr_3x_ui/internal/audit"
	"agr_3x_ui/internal/config"
	"agr_3x_ui/internal/db"
	httpapi "agr_3x_ui/internal/http"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/alerts"
	"agr_3x_ui/internal/services/checks"
	"agr_3x_ui/internal/services/dashboard"
	"agr_3x_ui/internal/services/metrics"
	"agr_3x_ui/internal/services/nodecheck"
	"agr_3x_ui/internal/services/ops"
	"agr_3x_ui/internal/services/sshclient"
	"agr_3x_ui/internal/services/sshws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	dbConn, err := db.Open(cfg.DBDSN)
	if err != nil {
		log.Fatalf("db error: %v", err)
	}
	enc, err := security.NewEncryptor(cfg.MasterKeyB64)
	if err != nil {
		log.Fatalf("encryptor error: %v", err)
	}
	alertsSvc := alerts.New(dbConn, enc, cfg.PublicBaseURL)
	rpID := strings.TrimSpace(cfg.AuthRPID)
	rpOrigin := strings.TrimSpace(cfg.AuthRPOrigin)
	if rpID == "" && cfg.PublicBaseURL != "" {
		if parsed, err := url.Parse(cfg.PublicBaseURL); err == nil && parsed.Host != "" {
			rpID = parsed.Host
		}
	}
	if rpOrigin == "" && cfg.PublicBaseURL != "" {
		rpOrigin = cfg.PublicBaseURL
	}
	if rpID == "" {
		rpID = "localhost"
	}
	if rpOrigin == "" {
		rpOrigin = "http://localhost"
	}
	webAuthn, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "3x-ui Aggregator",
		RPID:          rpID,
		RPOrigins:     []string{rpOrigin},
	})
	if err != nil {
		log.Fatalf("webauthn init error: %v", err)
	}
	handler := &httpapi.Handler{
		DB:                  dbConn,
		Encryptor:           enc,
		Audit:               audit.New(dbConn),
		Alerts:              alertsSvc,
		AdminUser:           cfg.AdminUser,
		AdminPass:           cfg.AdminPass,
		JWTSecret:           []byte(cfg.JWTSecret),
		JWTExpiry:           cfg.JWTExpiry,
		RefreshTTL:          cfg.RefreshTokenTTL,
		WebAuthnRegisterTTL: cfg.WebAuthnRegisterChallengeTTL,
		WebAuthnLoginTTL:    cfg.WebAuthnLoginChallengeTTL,
		FileAllowedRoots:    cfg.FileAllowedRoots,
		FilePreviewMaxBytes: cfg.FilePreviewMaxBytes,
		FileTailMaxBytes:    cfg.FileTailMaxBytes,
		WebAuthn:            webAuthn,
		SSHClient:           sshclient.New(15 * time.Second),
		SSHManager:          sshws.NewManager(cfg.SSHMaxSessions),
		SSHIdleTimeout:      cfg.SSHIdleTimeout,
		MasterKey:           cfg.MasterKeyB64,
		AllowCIDR:           cfg.AllowCIDR,
		TokenSalt:           cfg.TokenSalt,
		PublicBaseURL:       cfg.PublicBaseURL,
	}
	if _, err := handler.EnsureRootOrg(context.Background()); err != nil {
		log.Printf("ensure root org failed: %v", err)
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			handler.CleanupExpiredWebAuthnChallenges(context.Background())
		}
	}()
	nodecheck.New(dbConn, alertsSvc, time.Minute).Start(context.Background())
	metrics.New(dbConn, handler.SSHClient, enc, alertsSvc, 5*time.Minute, 30*24*time.Hour).Start(context.Background())
	checksWorker := checks.New(dbConn, alertsSvc, handler.SSHClient, enc, 10*time.Second)
	handler.Checks = checksWorker
	checksWorker.Start(context.Background())
	agentExec := ops.NewAgentExecutor(enc, 10*time.Second)
	opsSvc := ops.New(dbConn, ops.NewSSHExecutor(enc, 20*time.Second), agentExec, enc, cfg.SudoPasswords, cfg.AllowCIDR, cfg.RepoPath)
	handler.Ops = opsSvc
	opsSvc.Start(context.Background())
	agentProvider := dashboard.NewAgentProvider(enc, cfg.DashboardAgentTimeout)
	panelProvider := dashboard.NewPanelActiveUsersProvider(enc, cfg.DashboardCollectTimeout, cfg.DashboardPanelSessionTTL)
	metricsProvider := &dashboard.CompositeMetricsProvider{
		Agent:       agentProvider,
		PreferAgent: cfg.DashboardAgentPrefer,
	}
	usersProvider := &dashboard.CompositeActiveUsersProvider{
		Agent:        agentProvider,
		Panel:        panelProvider,
		PreferAgent:  cfg.DashboardAgentPrefer,
		PanelEnabled: cfg.DashboardPanelActiveUsers,
	}
	dashboardSvc := dashboard.New(dbConn, metricsProvider, usersProvider, cfg.DashboardCollectInterval, cfg.DashboardCollectParallelism)
	handler.Dashboard = dashboardSvc
	dashboardSvc.Start(context.Background())
	router := httpapi.NewRouter(handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatalf("invalid PORT: %v", err)
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("api listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
