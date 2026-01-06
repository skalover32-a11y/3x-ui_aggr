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
	JWTExpiry                    time.Duration
	AccessTokenTTL               time.Duration
	RefreshTokenTTL              time.Duration
	AuthRPID                     string
	AuthRPOrigin                 string
	WebAuthnRegisterChallengeTTL time.Duration
	WebAuthnLoginChallengeTTL    time.Duration
	SSHMaxSessions               int
	SSHIdleTimeout               time.Duration
	PublicBaseURL                string
}

func Load() (*Config, error) {
	cfg := &Config{
		DBDSN:         strings.TrimSpace(os.Getenv("DB_DSN")),
		AdminUser:     strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPass:     strings.TrimSpace(os.Getenv("ADMIN_PASS")),
		JWTSecret:     strings.TrimSpace(os.Getenv("JWT_SECRET")),
		MasterKeyB64:  strings.TrimSpace(os.Getenv("AGG_MASTER_KEY_BASE64")),
		PublicBaseURL: strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		AuthRPID:      strings.TrimSpace(os.Getenv("AUTH_RP_ID")),
		AuthRPOrigin:  strings.TrimSpace(os.Getenv("AUTH_RP_ORIGIN")),
	}
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
