package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func identityServiceForPrepareTest(t *testing.T) *IdentityService {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	hmacKey := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF"))
	service, err := NewIdentityService(IdentityConfig{
		Enabled:         true,
		HMACKey:         hmacKey,
		EncryptionKey:   key,
		EncryptionKeyID: "test-key",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestIdentityPrepareDoesNotCorrelateMissingEmail(t *testing.T) {
	service := identityServiceForPrepareTest(t)
	fact, err := service.prepare(IdentityEventReport{
		EventKey:   "anonymous-event",
		EventType:  "oauth_start",
		EventClass: "oauth",
		Outcome:    "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fact.EmailLookupKey != "" {
		t.Fatalf("missing email produced lookup key %q", fact.EmailLookupKey)
	}
}

func TestIdentityPrepareCorrelatesNormalizedEmail(t *testing.T) {
	service := identityServiceForPrepareTest(t)
	input := IdentityEventReport{
		EventKey:   "email-event",
		EventType:  "registration_success",
		EventClass: "registration",
		Outcome:    "success",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Email:      " User@Example.COM ",
	}
	fact, err := service.prepare(input)
	if err != nil {
		t.Fatal(err)
	}
	want := service.protector.LookupKey("email", "user@example.com")
	if fact.EmailLookupKey != want {
		t.Fatalf("email lookup key = %q, want %q", fact.EmailLookupKey, want)
	}
}

func TestIdentityEmailDisplayIsShortAndStable(t *testing.T) {
	if got := identityEmailDisplay("83a1abcdef"); got != "E-83A1" {
		t.Fatalf("identityEmailDisplay() = %q", got)
	}
}

func TestAPISuccessNeverWritesCrossAccountIdentityLinks(t *testing.T) {
	fact := IdentityFact{EventClass: identityEventAPI, Outcome: "success", UserID: 42}
	if err := upsertIdentityLinks(nil, nil, fact, 1, 2, 3, 4); err != nil {
		t.Fatalf("API success should be handled only by the daily aggregate: %v", err)
	}
}

func TestIdentityRuleDomainEnabledRequiresGlobalAndDomainSwitches(t *testing.T) {
	cfg := IdentityConfig{
		RulesEnabled:            true,
		IPCollectionEnabled:     true,
		DeviceCollectionEnabled: true,
		IPDomainEnabled:         true,
		DeviceDomainEnabled:     true,
		CompositeDomainEnabled:  true,
	}
	for _, domain := range []string{"account", "ip", "device", "composite"} {
		if !identityRuleDomainEnabled(cfg, domain) {
			t.Fatalf("domain %q should be enabled", domain)
		}
	}
	cfg.RulesEnabled = false
	if identityRuleDomainEnabled(cfg, "account") || identityRuleDomainEnabled(cfg, "ip") {
		t.Fatal("global rule switch must disable every identity rule")
	}
	cfg.RulesEnabled = true
	cfg.DeviceDomainEnabled = false
	if identityRuleDomainEnabled(cfg, "device") || identityRuleDomainEnabled(cfg, "composite") {
		t.Fatal("device domain switch must disable device and composite rules")
	}
}

func TestIdentityRuleEffectiveStateRequiresHealthyQuality(t *testing.T) {
	cfg := IdentityConfig{RulesEnabled: true, IPCollectionEnabled: true, IPDomainEnabled: true}
	if got := identityRuleEffectiveState(cfg, "ip", map[string]string{"ip": "paused"}); got != "paused" {
		t.Fatalf("IP effective state = %q", got)
	}
	if got := identityRuleEffectiveState(cfg, "account", nil); got != "healthy" {
		t.Fatalf("account effective state = %q", got)
	}
	cfg.RulesEnabled = false
	if got := identityRuleEffectiveState(cfg, "ip", map[string]string{"ip": "healthy"}); got != "disabled" {
		t.Fatalf("disabled global state = %q", got)
	}
}

func TestIdentityPrepareRejectsAPISuccessOlderThanDedupLedger(t *testing.T) {
	service := identityServiceForPrepareTest(t)
	_, err := service.prepare(IdentityEventReport{
		EventKey:   "stale-api-success",
		EventType:  "api_request",
		EventClass: identityEventAPI,
		Outcome:    "success",
		OccurredAt: time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano),
		UserID:     42,
	})
	if err == nil || !strings.Contains(err.Error(), "occurred_at") {
		t.Fatalf("stale API success error = %v", err)
	}
}
