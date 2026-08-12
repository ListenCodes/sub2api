package main

import (
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

func TestIdentityAdminHealthAndRebuildStatusAreUnavailableWhenDisabled(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret}, NewMemoryRepository(nil))
	for _, path := range []string{"/api/v1/admin/identity-health", "/api/v1/admin/risk-rebuilds/1"} {
		request := signedRequest(http.MethodGet, path, nil, testSecret, "nonce-"+path, time.Now())
		request.Header.Set("X-Risk-Actor-ID", "7")
		response := serveJSON(server, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, body = %s", path, response.Code, response.Body.String())
		}
	}
}
