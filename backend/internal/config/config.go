package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBDSN        string
	AdminUser    string
	AdminPass    string
	JWTSecret    string
	MasterKeyB64 string
	JWTExpiry    time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{
		DBDSN:        strings.TrimSpace(os.Getenv("DB_DSN")),
		AdminUser:    strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPass:    strings.TrimSpace(os.Getenv("ADMIN_PASS")),
		JWTSecret:    strings.TrimSpace(os.Getenv("JWT_SECRET")),
		MasterKeyB64: strings.TrimSpace(os.Getenv("AGG_MASTER_KEY_BASE64")),
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
	return cfg, nil
}
