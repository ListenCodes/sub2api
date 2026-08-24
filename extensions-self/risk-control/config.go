package main

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

var ErrWeakInternalSecret = errors.New("RISK_CONTROL_INTERNAL_SECRET must be at least 32 characters")

var (
	ErrIdentityKeyLength = errors.New("identity HMAC and encryption keys must each decode to exactly 32 bytes")
	ErrIdentityKeyReuse  = errors.New("identity HMAC and encryption keys must be different")
)

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
	if err := cfg.Identity.Validate(); err != nil {
		return err
	}
	return cfg.AccountMonitor.Validate()
}

type IdentityConfig struct {
	Enabled                     bool
	IPCollectionEnabled         bool
	DeviceCollectionEnabled     bool
	AdminEnabled                bool
	RulesEnabled                bool
	IPDomainEnabled             bool
	DeviceDomainEnabled         bool
	CompositeDomainEnabled      bool
	CompositeEnforcementEnabled bool
	CurrentScoreEnabled         bool
	CasesEnabled                bool
	ExplainEnabled              bool
	DeliveryEnabled             bool
	HMACKey                     string
	EncryptionKey               string
	EncryptionKeyID             string
	PreviousEncryptionKey       string
	PreviousEncryptionKeyID     string
	GeoSource                   string
	ShadowUntil                 time.Time
	MaxBodyBytes                int64
	QualityMinEvents            int64
	QualityMinCoverage          int64
	QualityMinUsers             int64
	QualityMaxIPShare           int64
}

func (c IdentityConfig) active() bool {
	return c.Enabled || c.IPCollectionEnabled || c.DeviceCollectionEnabled || c.AdminEnabled || c.RulesEnabled || c.IPDomainEnabled || c.DeviceDomainEnabled || c.CompositeDomainEnabled || c.CompositeEnforcementEnabled || c.CurrentScoreEnabled || c.CasesEnabled || c.ExplainEnabled || c.DeliveryEnabled
}

func (c IdentityConfig) Validate() error {
	if !c.active() {
		return nil
	}
	hmacKey, hmacErr := base64.StdEncoding.DecodeString(strings.TrimSpace(c.HMACKey))
	encryptionKey, encryptionErr := base64.StdEncoding.DecodeString(strings.TrimSpace(c.EncryptionKey))
	if hmacErr != nil || encryptionErr != nil || len(hmacKey) != 32 || len(encryptionKey) != 32 {
		return ErrIdentityKeyLength
	}
	if subtle.ConstantTimeCompare(hmacKey, encryptionKey) == 1 {
		return ErrIdentityKeyReuse
	}
	currentKeyID := strings.TrimSpace(c.EncryptionKeyID)
	if currentKeyID == "" || len(currentKeyID) > 40 {
		return errors.New("RISK_IDENTITY_ENCRYPTION_KEY_ID is required and must not exceed 40 characters")
	}
	previousKey := strings.TrimSpace(c.PreviousEncryptionKey)
	previousKeyID := strings.TrimSpace(c.PreviousEncryptionKeyID)
	if previousKey != "" || previousKeyID != "" {
		previous, err := base64.StdEncoding.DecodeString(previousKey)
		if err != nil || len(previous) != 32 || previousKeyID == "" || len(previousKeyID) > 40 || previousKeyID == currentKeyID || subtle.ConstantTimeCompare(previous, encryptionKey) == 1 || subtle.ConstantTimeCompare(previous, hmacKey) == 1 {
			return errors.New("previous identity encryption key and a distinct key id are required together")
		}
	}
	if c.RulesEnabled && c.ShadowUntil.IsZero() {
		return errors.New("RISK_IDENTITY_SHADOW_UNTIL is required while identity rules are enabled")
	}
	if c.CompositeEnforcementEnabled && (!c.Enabled || !c.IPCollectionEnabled || !c.DeviceCollectionEnabled || !c.AdminEnabled || !c.RulesEnabled || !c.IPDomainEnabled || !c.DeviceDomainEnabled || !c.CompositeDomainEnabled || !c.CurrentScoreEnabled || !c.CasesEnabled || !c.ExplainEnabled || !c.DeliveryEnabled || c.GeoSource != "cloudflare_verified") {
		return errors.New("RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED requires the complete healthy V2 rollout and verified geo")
	}
	return nil
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
	Identity          IdentityConfig
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
		Identity: IdentityConfig{
			Enabled:                     envBool("RISK_IDENTITY_V2_ENABLED", false),
			IPCollectionEnabled:         envBool("RISK_IDENTITY_IP_COLLECTION_ENABLED", false),
			DeviceCollectionEnabled:     envBool("RISK_IDENTITY_DEVICE_COLLECTION_ENABLED", false),
			AdminEnabled:                envBool("RISK_IDENTITY_ADMIN_ENABLED", false),
			RulesEnabled:                envBool("RISK_IDENTITY_RULES_ENABLED", false),
			IPDomainEnabled:             envBoolFallback("RISK_IDENTITY_IP_RULES_ENABLED", "RISK_IDENTITY_IP_DOMAIN_ENABLED", false),
			DeviceDomainEnabled:         envBoolFallback("RISK_IDENTITY_DEVICE_RULES_ENABLED", "RISK_IDENTITY_DEVICE_DOMAIN_ENABLED", false),
			CompositeDomainEnabled:      envBoolFallback("RISK_IDENTITY_COMPOSITE_RULES_ENABLED", "RISK_IDENTITY_COMPOSITE_DOMAIN_ENABLED", false),
			CompositeEnforcementEnabled: envBool("RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED", false),
			CurrentScoreEnabled:         envBool("RISK_IDENTITY_CURRENT_SCORE_ENABLED", false),
			CasesEnabled:                envBool("RISK_IDENTITY_CASES_ENABLED", false),
			ExplainEnabled:              envBool("RISK_IDENTITY_EXPLAIN_ENABLED", false),
			DeliveryEnabled:             envBool("RISK_IDENTITY_DELIVERY_ENABLED", false),
			HMACKey:                     strings.TrimSpace(os.Getenv("RISK_IDENTITY_HMAC_KEY")),
			EncryptionKey:               strings.TrimSpace(os.Getenv("RISK_IDENTITY_ENCRYPTION_KEY")),
			EncryptionKeyID:             strings.TrimSpace(os.Getenv("RISK_IDENTITY_ENCRYPTION_KEY_ID")),
			PreviousEncryptionKey:       strings.TrimSpace(os.Getenv("RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY")),
			PreviousEncryptionKeyID:     strings.TrimSpace(os.Getenv("RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY_ID")),
			GeoSource:                   envOr("RISK_IDENTITY_GEO_SOURCE", "cloudflare_or_local"),
			ShadowUntil:                 envTime("RISK_IDENTITY_SHADOW_UNTIL"),
			MaxBodyBytes:                envInt64("RISK_IDENTITY_MAX_BODY_BYTES", 32*1024),
			QualityMinEvents:            envInt64("RISK_IDENTITY_QUALITY_MIN_EVENTS", 50),
			QualityMinCoverage:          envInt64("RISK_IDENTITY_QUALITY_MIN_COVERAGE_PERCENT", 80),
			QualityMinUsers:             envInt64("RISK_IDENTITY_QUALITY_MIN_USERS", 50),
			QualityMaxIPShare:           envInt64("RISK_IDENTITY_QUALITY_MAX_IP_SHARE_PERCENT", 20),
		},
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

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBoolFallback(primary, legacy string, fallback bool) bool {
	if _, ok := os.LookupEnv(primary); ok {
		return envBool(primary, fallback)
	}
	return envBool(legacy, fallback)
}

func envTime(key string) time.Time {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed.UTC()
}
