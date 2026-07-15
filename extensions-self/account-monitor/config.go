package accountmonitor

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrSourceDatabaseRequired = errors.New("ACCOUNT_MONITOR_SOURCE_DATABASE_URL is required when account monitor is enabled")

type Config struct {
	Enabled           bool
	SourceDatabaseURL string
	PollInterval      time.Duration
	Lookback          time.Duration
	BatchSize         int
	DetailRetention   time.Duration
	DailyRetention    time.Duration
	QueryTimeout      time.Duration
}

func LoadConfig(getenv func(string) string) Config {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return Config{
		Enabled:           envBool(getenv("ACCOUNT_MONITOR_ENABLED"), false),
		SourceDatabaseURL: strings.TrimSpace(getenv("ACCOUNT_MONITOR_SOURCE_DATABASE_URL")),
		PollInterval:      envDurationSeconds(getenv("ACCOUNT_MONITOR_POLL_SECONDS"), time.Minute),
		Lookback:          envDurationSeconds(getenv("ACCOUNT_MONITOR_LOOKBACK_SECONDS"), 5*time.Minute),
		BatchSize:         envInt(getenv("ACCOUNT_MONITOR_BATCH_SIZE"), 1000),
		DetailRetention:   90 * 24 * time.Hour,
		DailyRetention:    365 * 24 * time.Hour,
		QueryTimeout:      envDurationMilliseconds(getenv("ACCOUNT_MONITOR_QUERY_TIMEOUT_MS"), 3*time.Second),
	}
}

func (c Config) Validate() error {
	if c.Enabled && strings.TrimSpace(c.SourceDatabaseURL) == "" {
		return ErrSourceDatabaseRequired
	}
	return nil
}

func envBool(value string, fallback bool) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDurationSeconds(value string, fallback time.Duration) time.Duration {
	return time.Duration(envInt64(value, int64(fallback/time.Second))) * time.Second
}

func envDurationMilliseconds(value string, fallback time.Duration) time.Duration {
	return time.Duration(envInt64(value, int64(fallback/time.Millisecond))) * time.Millisecond
}

func envInt64(value string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
