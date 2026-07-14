package accountmonitor

import (
	"errors"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := LoadConfig(func(string) string { return "" })

	if cfg.Enabled {
		t.Fatal("monitor must default to disabled")
	}
	if cfg.PollInterval != time.Minute {
		t.Fatalf("poll interval = %s, want 1m", cfg.PollInterval)
	}
	if cfg.Lookback != 5*time.Minute {
		t.Fatalf("lookback = %s, want 5m", cfg.Lookback)
	}
	if cfg.DetailRetention != 90*24*time.Hour {
		t.Fatalf("detail retention = %s, want 90d", cfg.DetailRetention)
	}
	if cfg.DailyRetention != 365*24*time.Hour {
		t.Fatalf("daily retention = %s, want 365d", cfg.DailyRetention)
	}
	if cfg.BatchSize != 1000 {
		t.Fatalf("batch size = %d, want 1000", cfg.BatchSize)
	}
}

func TestConfigRequiresSourceDatabaseWhenEnabled(t *testing.T) {
	cfg := Config{Enabled: true}

	if err := cfg.Validate(); !errors.Is(err, ErrSourceDatabaseRequired) {
		t.Fatalf("Validate() error = %v, want %v", err, ErrSourceDatabaseRequired)
	}
}

func TestConfigAcceptsEnabledSourceDatabase(t *testing.T) {
	cfg := Config{Enabled: true, SourceDatabaseURL: "postgres://monitor@db/sub2api"}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
