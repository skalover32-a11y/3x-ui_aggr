package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBDSN          string
	AdminUser      string
	AdminPass      string
	JWTSecret      string
	MasterKeyB64   string
	JWTExpiry      time.Duration
	SSHMaxSessions int
	SSHIdleTimeout time.Duration
	PublicBaseURL  string
}

func Load() (*Config, error) {
	cfg := &Config{
		DBDSN:        strings.TrimSpace(os.Getenv("DB_DSN")),
		AdminUser:    strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPass:    strings.TrimSpace(os.Getenv("ADMIN_PASS")),
		JWTSecret:    strings.TrimSpace(os.Getenv("JWT_SECRET")),
		MasterKeyB64: strings.TrimSpace(os.Getenv("AGG_MASTER_KEY_BASE64")),
		PublicBaseURL: strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
	}
	expHours := strings.TrimSpace(os.Getenv("JWT_EXP_HOURS"))
	if expHours == "" {
		cfg.JWTExpiry = 24 * time.Hour
	} else {
		hours, err := strconv.Atoi(expHours)
		if err != nil {
			return nil, fmt.Errorf("invalid JWT_EXP_HOURS: %w", err)
		}
		cfg.JWTExpiry = time.Duration(hours) * time.Hour
	}
	if cfg.DBDSN == "" || cfg.AdminUser == "" || cfg.AdminPass == "" || cfg.JWTSecret == "" || cfg.MasterKeyB64 == "" {
		return nil, fmt.Errorf("missing required env vars")
	}
	maxSessions := strings.TrimSpace(os.Getenv("GLOBAL_MAX_SSH_SESSIONS"))
	if maxSessions == "" {
		cfg.SSHMaxSessions = 10
	} else {
		val, err := strconv.Atoi(maxSessions)
		if err != nil {
			return nil, fmt.Errorf("invalid GLOBAL_MAX_SSH_SESSIONS: %w", err)
		}
		cfg.SSHMaxSessions = val
	}
	idleSeconds := strings.TrimSpace(os.Getenv("SSH_IDLE_TIMEOUT_SECONDS"))
	if idleSeconds == "" {
		cfg.SSHIdleTimeout = 600 * time.Second
	} else {
		val, err := strconv.Atoi(idleSeconds)
		if err != nil {
			return nil, fmt.Errorf("invalid SSH_IDLE_TIMEOUT_SECONDS: %w", err)
		}
		cfg.SSHIdleTimeout = time.Duration(val) * time.Second
	}
	return cfg, nil
}
