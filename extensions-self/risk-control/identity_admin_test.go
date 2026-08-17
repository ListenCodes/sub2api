package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestMaskIdentityIPReturnsNetworkOnly(t *testing.T) {
	tests := map[string]string{
		"203.0.113.99":     "203.0.113.0/24",
		"2001:db8:1:2::99": "2001:db8:1:2::/64",
		"invalid":          "",
	}
	for input, want := range tests {
		if got := maskIdentityIP(input); got != want {
			t.Fatalf("maskIdentityIP(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIdentityDeviceDisplaySeparatesIdentityKinds(t *testing.T) {
	tests := []struct {
		kind       string
		wantCode   string
		confidence string
	}{
		{kind: "browser_instance", wantCode: "B-ABCDEF", confidence: "medium_high"},
		{kind: "browser_profile", wantCode: "F-ABCDEF", confidence: "low"},
		{kind: "api_client", wantCode: "K-ABCDEF", confidence: "high"},
	}
	for _, test := range tests {
		code, confidence := identityDeviceDisplay(test.kind, "abcdef012345")
		if code != test.wantCode || confidence != test.confidence {
			t.Fatalf("identityDeviceDisplay(%q) = %q/%q", test.kind, code, confidence)
		}
	}
}

func TestCombinedIdentityStateUsesMostRestrictiveDomain(t *testing.T) {
	if got := combinedIdentityState(map[string]string{"ip": "healthy", "device": "degraded", "composite": "healthy"}); got != "degraded" {
		t.Fatalf("degraded state = %q", got)
	}
	if got := combinedIdentityState(map[string]string{"ip": "healthy", "device": "paused", "composite": "degraded"}); got != "paused" {
		t.Fatalf("paused state = %q", got)
	}
}

func TestUniqueIdentityUserIDsDeduplicatesAndBoundsInput(t *testing.T) {
	input := []int64{0, -1, 7, 7}
	for id := int64(1); id <= 120; id++ {
		input = append(input, id)
	}
	got := uniqueIdentityUserIDs(input)
	if len(got) != 100 || got[0] != 7 {
		t.Fatalf("bounded ids = len %d first %d", len(got), got[0])
	}
	seen := make(map[int64]bool, len(got))
	for _, id := range got {
		if id <= 0 || seen[id] {
			t.Fatalf("invalid bounded ids: %#v", got)
		}
		seen[id] = true
	}
}

func TestParseOptionalScoreRejectsMalformedFilters(t *testing.T) {
	if score, err := parseOptionalScore(""); err != nil || score != -1 {
		t.Fatalf("empty score = %d, error=%v", score, err)
	}
	if score, err := parseOptionalScore("60"); err != nil || score != 60 {
		t.Fatalf("valid score = %d, error=%v", score, err)
	}
	for _, input := range []string{"abc", "-1", "101"} {
		if _, err := parseOptionalScore(input); err == nil {
			t.Fatalf("parseOptionalScore(%q) accepted malformed input", input)
		}
	}
}

func TestIdentityRiskLevelRangeMatchesDisplayedRiskLevels(t *testing.T) {
	tests := []struct {
		level    string
		minimum  int
		maximum  int
		accepted bool
	}{
		{level: "none", minimum: 0, maximum: 0, accepted: true},
		{level: "low", minimum: 1, maximum: 29, accepted: true},
		{level: "medium", minimum: 30, maximum: 59, accepted: true},
		{level: "high", minimum: 60, maximum: 79, accepted: true},
		{level: "critical", minimum: 80, maximum: 100, accepted: true},
		{level: "unknown", accepted: false},
	}
	for _, test := range tests {
		minimum, maximum, accepted := identityRiskLevelRange(test.level)
		if minimum != test.minimum || maximum != test.maximum || accepted != test.accepted {
			t.Fatalf("identityRiskLevelRange(%q) = (%d,%d,%v), want (%d,%d,%v)", test.level, minimum, maximum, accepted, test.minimum, test.maximum, test.accepted)
		}
	}
}

func TestIdentityAdminHealthReportsDisabledWithoutServiceFailure(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret}, NewMemoryRepository(nil))
	request := signedRequest(http.MethodGet, "/api/v1/admin/identity-health", nil, testSecret, "nonce-disabled-health", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var health IdentityHealth
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.AdminEnabled || health.Enabled || health.Schema != "v2" {
		t.Fatalf("unexpected disabled health: %+v", health)
	}
}

func TestIdentityAdminHealthReportsUnavailableWhenCollectionCannotInitialize(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Identity: IdentityConfig{Enabled: true}}, NewMemoryRepository(nil))
	request := signedRequest(http.MethodGet, "/api/v1/admin/identity-health", nil, testSecret, "nonce-unavailable-health", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestIdentityRebuildStatusRemainsUnavailableWhenAdminDisabled(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret}, NewMemoryRepository(nil))
	request := signedRequest(http.MethodGet, "/api/v1/admin/risk-rebuilds/1", nil, testSecret, "nonce-disabled-rebuild", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
