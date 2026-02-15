package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBDSN                        string
	AdminUser                    string
	AdminPass                    string
	JWTSecret                    string
	MasterKeyB64                 string
	TokenSalt                    string
	TelegramWebhookSecret        string
	JWTExpiry                    time.Duration
	AccessTokenTTL               time.Duration
	RefreshTokenTTL              time.Duration
	AuthRPID                     string
	AuthRPOrigin                 string
	WebAuthnRegisterChallengeTTL time.Duration
	WebAuthnLoginChallengeTTL    time.Duration
	FileAllowedRoots             []string
	FilePreviewMaxBytes          int64
	FileTailMaxBytes             int64
	SSHMaxSessions               int
	SSHIdleTimeout               time.Duration
	PublicBaseURL                string
	DashboardCollectInterval     time.Duration
	DashboardCollectParallelism  int
	DashboardCollectTimeout      time.Duration
	DashboardPanelActiveUsers    bool
	DashboardPanelSessionTTL     time.Duration
	DashboardAgentTimeout        time.Duration
	DashboardAgentPrefer         bool
	AlertCPUThreshold            float64
	AlertMemoryThreshold         float64
	AlertDiskFreeThreshold       float64
	AlertOfflineDelay            time.Duration
	SudoPasswords                []string
	AllowCIDR                    string
	RepoPath                     string
}

func Load() (*Config, error) {
	cfg := &Config{
		DBDSN:                 strings.TrimSpace(os.Getenv("DB_DSN")),
		AdminUser:             strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPass:             strings.TrimSpace(os.Getenv("ADMIN_PASS")),
		JWTSecret:             strings.TrimSpace(os.Getenv("JWT_SECRET")),
		MasterKeyB64:          strings.TrimSpace(os.Getenv("AGG_MASTER_KEY_BASE64")),
		PublicBaseURL:         strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		TokenSalt:             strings.TrimSpace(os.Getenv("TOKEN_SALT")),
		TelegramWebhookSecret: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
		AuthRPID:              strings.TrimSpace(os.Getenv("AUTH_RP_ID")),
		AuthRPOrigin:          strings.TrimSpace(os.Getenv("AUTH_RP_ORIGIN")),
	}
	cfg.SudoPasswords = parseCSVEnv("SUDO_PASSWORDS", nil)
	cfg.AllowCIDR = strings.TrimSpace(os.Getenv("AGG_ALLOW_CIDR"))
	cfg.RepoPath = strings.TrimSpace(os.Getenv("AGG_REPO_PATH"))
	accessTTL := strings.TrimSpace(os.Getenv("ACCESS_TOKEN_TTL"))
	if accessTTL != "" {
		val, err := time.ParseDuration(accessTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid ACCESS_TOKEN_TTL: %w", err)
		}
		cfg.AccessTokenTTL = val
	} else {
		expHours := strings.TrimSpace(os.Getenv("JWT_EXP_HOURS"))
		if expHours == "" {
			cfg.AccessTokenTTL = 24 * time.Hour
		} else {
			hours, err := strconv.Atoi(expHours)
			if err != nil {
				return nil, fmt.Errorf("invalid JWT_EXP_HOURS: %w", err)
			}
			cfg.AccessTokenTTL = time.Duration(hours) * time.Hour
		}
	}
	cfg.JWTExpiry = cfg.AccessTokenTTL
	refreshTTL := strings.TrimSpace(os.Getenv("REFRESH_TOKEN_TTL"))
	if refreshTTL == "" {
		cfg.RefreshTokenTTL = 720 * time.Hour
	} else {
		val, err := time.ParseDuration(refreshTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid REFRESH_TOKEN_TTL: %w", err)
		}
		cfg.RefreshTokenTTL = val
	}
	cfg.WebAuthnRegisterChallengeTTL = parseDurationEnv("WEBAUTHN_REGISTER_CHALLENGE_TTL", 5*time.Minute)
	cfg.WebAuthnLoginChallengeTTL = parseDurationEnv("WEBAUTHN_LOGIN_CHALLENGE_TTL", 3*time.Minute)
	cfg.FileAllowedRoots = parseCSVEnv("FILE_ALLOWED_ROOTS", []string{"/opt", "/var/log", "/home/*/backups"})
	cfg.FilePreviewMaxBytes = parseInt64Env("FILE_PREVIEW_MAX_BYTES", 2*1024*1024)
	cfg.FileTailMaxBytes = parseInt64Env("FILE_TAIL_MAX_BYTES", 128*1024)
	cfg.DashboardCollectInterval = parseDurationEnv("DASHBOARD_COLLECT_INTERVAL", 10*time.Second)
	cfg.DashboardCollectTimeout = parseDurationEnv("DASHBOARD_COLLECT_TIMEOUT", 8*time.Second)
	cfg.DashboardCollectParallelism = parseIntEnv("DASHBOARD_COLLECT_PARALLELISM", 5)
	cfg.DashboardPanelActiveUsers = parseBoolEnv("DASHBOARD_PANEL_ACTIVE_USERS_ENABLED", true)
	cfg.DashboardPanelSessionTTL = parseDurationAllowZeroEnv("DASHBOARD_PANEL_SESSION_TTL", 12*time.Hour)
	cfg.DashboardAgentTimeout = parseDurationEnv("DASHBOARD_AGENT_TIMEOUT", 5*time.Second)
	cfg.DashboardAgentPrefer = parseBoolEnv("DASHBOARD_AGENT_PREFER", true)
	cfg.AlertCPUThreshold = parseFloat64Env("ALERT_CPU_THRESHOLD", 2.0)
	cfg.AlertMemoryThreshold = parseFloat64Env("ALERT_MEMORY_THRESHOLD", 90.0)
	cfg.AlertDiskFreeThreshold = parseFloat64Env("ALERT_DISK_FREE_THRESHOLD", 10.0)
	cfg.AlertOfflineDelay = parseDurationAllowZeroEnv("ALERT_OFFLINE_DELAY", 5*time.Minute)
	if cfg.DBDSN == "" || cfg.AdminUser == "" || cfg.AdminPass == "" || cfg.JWTSecret == "" || cfg.MasterKeyB64 == "" || cfg.TokenSalt == "" {
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

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := time.ParseDuration(raw)
	if err != nil || val <= 0 {
		return fallback
	}
	return val
}

func parseDurationAllowZeroEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := time.ParseDuration(raw)
	if err != nil || val < 0 {
		return fallback
	}
	return val
}

func parseIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return fallback
	}
	return val
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return val
}

func parseCSVEnv(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func parseInt64Env(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || val <= 0 {
		return fallback
	}
	return val
}

func parseFloat64Env(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil || val < 0 {
		return fallback
	}
	return val
}
