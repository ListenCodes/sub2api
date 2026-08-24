package main

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

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

func TestIdentityRulesRequireRecordedShadowDeadline(t *testing.T) {
	keyA := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	keyB := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF"))
	cfg := IdentityConfig{Enabled: true, RulesEnabled: true, HMACKey: keyA, EncryptionKey: keyB, EncryptionKeyID: "v1"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("identity rules accepted without a recorded Shadow deadline")
	}
	cfg.ShadowUntil = time.Now().UTC().Add(-time.Hour)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("recorded Shadow deadline is validated against persisted activation state: %v", err)
	}
}

func TestInitialIdentityShadowRequiresFourteenFullDays(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	if err := validateInitialShadowDeadline(now, now.Add(14*24*time.Hour-time.Second)); err == nil {
		t.Fatal("initial Shadow shorter than 14 days was accepted")
	}
	if err := validateInitialShadowDeadline(now, now.Add(14*24*time.Hour)); err != nil {
		t.Fatalf("14-day initial Shadow was rejected: %v", err)
	}
}

func TestCompositeEnforcementRequiresCompleteVerifiedRollout(t *testing.T) {
	keyA := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	keyB := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF"))
	cfg := IdentityConfig{
		Enabled: true, IPCollectionEnabled: true, DeviceCollectionEnabled: true, AdminEnabled: true,
		RulesEnabled: true, IPDomainEnabled: true, DeviceDomainEnabled: true, CompositeDomainEnabled: true,
		CompositeEnforcementEnabled: true, CurrentScoreEnabled: true, CasesEnabled: true, ExplainEnabled: true,
		DeliveryEnabled: true, GeoSource: "cloudflare_verified", ShadowUntil: time.Now().UTC().Add(24 * time.Hour),
		HMACKey: keyA, EncryptionKey: keyB, EncryptionKeyID: "v1",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("complete enforcement rollout rejected: %v", err)
	}
	cfg.DeliveryEnabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("composite enforcement accepted without delivery health coverage")
	}
	cfg.DeliveryEnabled = true
	cfg.GeoSource = "cloudflare_or_local"
	if err := cfg.Validate(); err == nil {
		t.Fatal("composite enforcement accepted without verified geo rollout")
	}
}

func TestIdentityPreviousEncryptionKeyRequiresCompleteDistinctPair(t *testing.T) {
	keyA := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	keyB := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF"))
	base := IdentityConfig{Enabled: true, HMACKey: keyA, EncryptionKey: keyB, EncryptionKeyID: "v2"}

	missingKey := base
	missingKey.PreviousEncryptionKeyID = "v1"
	if err := missingKey.Validate(); err == nil {
		t.Fatal("previous key id without a previous key was accepted")
	}

	reusedKey := base
	reusedKey.PreviousEncryptionKey = keyB
	reusedKey.PreviousEncryptionKeyID = "v1"
	if err := reusedKey.Validate(); err == nil {
		t.Fatal("current encryption key reused as previous key was accepted")
	}

	reusedHMAC := base
	reusedHMAC.PreviousEncryptionKey = keyA
	reusedHMAC.PreviousEncryptionKeyID = "v1"
	if err := reusedHMAC.Validate(); err == nil {
		t.Fatal("HMAC key reused as previous encryption key was accepted")
	}
}
