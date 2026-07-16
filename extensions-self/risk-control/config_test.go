package main

import (
	"errors"
	"testing"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

func TestValidateConfigRejectsWeakInternalSecret(t *testing.T) {
	err := validateConfig(Config{DatabaseURL: "postgres://risk", InternalSecret: "too-short"})
	if !errors.Is(err, ErrWeakInternalSecret) {
		t.Fatalf("validateConfig() error = %v, want weak secret error", err)
	}
}

func TestValidateConfigRequiresMonitorSourceWhenEnabled(t *testing.T) {
	err := validateConfig(Config{
		DatabaseURL: "postgres://risk", InternalSecret: "01234567890123456789012345678901",
		AccountMonitor: accountmonitor.Config{Enabled: true},
	})
	if !errors.Is(err, accountmonitor.ErrSourceDatabaseRequired) {
		t.Fatalf("validateConfig() error = %v", err)
	}
}

func TestValidateConfigAcceptsProductionLengthSecret(t *testing.T) {
	if err := validateConfig(Config{DatabaseURL: "postgres://risk", InternalSecret: "01234567890123456789012345678901"}); err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
}
