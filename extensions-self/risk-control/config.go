package main

import (
	"errors"
	"os"
	"strconv"
	"strings"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

var ErrWeakInternalSecret = errors.New("RISK_CONTROL_INTERNAL_SECRET must be at least 32 characters")

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.InternalSecret) == "" {
		return errors.New("RISK_CONTROL_INTERNAL_SECRET is required")
	}
	if len([]byte(cfg.InternalSecret)) < 32 {
		return ErrWeakInternalSecret
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return errors.New("RISK_CONTROL_DATABASE_URL is required")
	}
	return cfg.AccountMonitor.Validate()
}

type Config struct {
	DatabaseURL       string
	InternalSecret    string
	HomepageDir       string
	Listen            string
	Mode              string
	DecisionFailMode  string
	MaxBodyBytes      int64
	AdminProxyTimeout int
	AccountMonitor    accountmonitor.Config
}

func loadConfig() Config {
	return Config{
		DatabaseURL:       strings.TrimSpace(os.Getenv("RISK_CONTROL_DATABASE_URL")),
		InternalSecret:    strings.TrimSpace(os.Getenv("RISK_CONTROL_INTERNAL_SECRET")),
		HomepageDir:       envOr("EXTENSIONS_SELF_HOMEPAGE_DIR", "/app/homepage"),
		Listen:            envOr("RISK_CONTROL_LISTEN", ":8090"),
		Mode:              normalizeMode(envOr("RISK_CONTROL_MODE", "shadow")),
		DecisionFailMode:  normalizeFailMode(envOr("RISK_CONTROL_DECISION_FAIL_MODE", "open")),
		MaxBodyBytes:      envInt64("RISK_CONTROL_MAX_BODY_BYTES", 256*1024),
		AdminProxyTimeout: int(envInt64("RISK_CONTROL_ADMIN_TIMEOUT_MS", 3000)),
		AccountMonitor:    accountmonitor.LoadConfig(os.Getenv),
	}
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "review", "enforce":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "shadow"
	}
}

func normalizeFailMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "closed") {
		return "closed"
	}
	return "open"
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
