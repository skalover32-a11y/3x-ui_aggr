package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/audit"
	"agr_3x_ui/internal/config"
	"agr_3x_ui/internal/db"
	httpapi "agr_3x_ui/internal/http"
	"agr_3x_ui/internal/security"
	"agr_3x_ui/internal/services/alerts"
	"agr_3x_ui/internal/services/checks"
	"agr_3x_ui/internal/services/metrics"
	"agr_3x_ui/internal/services/nodecheck"
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
	handler := &httpapi.Handler{
		DB:             dbConn,
		Encryptor:      enc,
		Audit:          audit.New(dbConn),
		Alerts:         alertsSvc,
		AdminUser:      cfg.AdminUser,
		AdminPass:      cfg.AdminPass,
		JWTSecret:      []byte(cfg.JWTSecret),
		JWTExpiry:      cfg.JWTExpiry,
		SSHClient:      sshclient.New(15 * time.Second),
		SSHManager:     sshws.NewManager(cfg.SSHMaxSessions),
		SSHIdleTimeout: cfg.SSHIdleTimeout,
	}
	nodecheck.New(dbConn, alertsSvc, time.Minute).Start(context.Background())
	metrics.New(dbConn, handler.SSHClient, enc, alertsSvc, 5*time.Minute, 30*24*time.Hour).Start(context.Background())
	checksWorker := checks.New(dbConn, alertsSvc, handler.SSHClient, enc, 10*time.Second)
	handler.Checks = checksWorker
	checksWorker.Start(context.Background())
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
