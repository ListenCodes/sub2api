package main

import (
	"errors"
	"testing"
)

func TestValidateConfigRejectsWeakInternalSecret(t *testing.T) {
	err := validateConfig(Config{DatabaseURL: "postgres://risk", InternalSecret: "too-short"})
	if !errors.Is(err, ErrWeakInternalSecret) {
		t.Fatalf("validateConfig() error = %v, want weak secret error", err)
	}
}

func TestValidateConfigAcceptsProductionLengthSecret(t *testing.T) {
	if err := validateConfig(Config{DatabaseURL: "postgres://risk", InternalSecret: "01234567890123456789012345678901"}); err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
}
