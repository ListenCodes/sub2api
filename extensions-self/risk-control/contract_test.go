package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRiskEventContractDoesNotSerializeSensitiveFields(t *testing.T) {
	payload, err := json.Marshal(EventReport{
		EventKey:    "registration-1",
		EventType:   "registration_attempt",
		Password:    "should-not-appear",
		RequestBody: `{"password":"should-not-appear"}`,
		RawDeviceID: "raw-device-id",
		EmailHash:   "email-hash",
		DeviceHash:  "device-hash",
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"password", "request_body", "raw_device_id", "should-not-appear", "raw-device-id"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("serialized event contains forbidden value %q: %s", forbidden, serialized)
		}
	}
}

func TestRiskDecisionAcceptsSupportedActions(t *testing.T) {
	for _, action := range []string{"allow", "review", "ban", "reject_candidate"} {
		if !validRiskAction(action) {
			t.Fatalf("validRiskAction(%q) = false", action)
		}
	}
	if validRiskAction("delete_everything") {
		t.Fatal("unsupported action accepted")
	}
}

func TestEventRecordUsesStableJSONFieldNames(t *testing.T) {
	payload, err := json.Marshal(EventRecord{
		ID: 7, EventKey: "login-7-1", EventType: "login_failure", RiskType: "login_failure",
		Reason: "invalid credentials", OccurredAt: "2026-07-12T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal event record: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode event record: %v", err)
	}
	for _, key := range []string{"id", "event_key", "event_type", "risk_type", "reason", "occurred_at"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("event record missing JSON field %q: %s", key, payload)
		}
	}
	if _, ok := decoded["EventType"]; ok {
		t.Fatalf("event record must not expose Go field names: %s", payload)
	}
}

func TestSchemaMigratesLegacyRegistrationRuleToSplitStrategies(t *testing.T) {
	for _, expected := range []string{
		"ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS count_strategy",
		"code = 'registration_abuse'",
		"'registration_identity_abuse'",
		"'subject_device_events'",
		"'registration_ip_multi_account'",
		"'ip_distinct_subjects'",
	} {
		if !strings.Contains(schemaSQL, expected) {
			t.Fatalf("schema is missing %q", expected)
		}
	}
}

func TestIdentitySchemaStageZeroDoesNotMutateExistingV1Rules(t *testing.T) {
	if strings.Contains(schemaSQL, "WHERE code = 'api_request_observation'") {
		t.Fatal("Stage 0 schema must not mutate an existing V1 API observation rule")
	}
	if !strings.Contains(schemaSQL, "risk_identity_shadow_activation") {
		t.Fatal("schema does not persist the initial 14-day Shadow activation")
	}
}

func TestLegacyRiskProjectionUsesSetBasedAggregation(t *testing.T) {
	for _, expected := range []string{"legacy_api_subjects AS MATERIALIZED", "non_api_counts AS MATERIALIZED", "non_api_best AS MATERIALIZED"} {
		if !strings.Contains(riskSubjectProjectionCTE, expected) {
			t.Fatalf("risk subject projection is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"JOIN LATERAL", "SELECT COUNT(*)::int FROM risk_events x"} {
		if strings.Contains(riskSubjectProjectionCTE, forbidden) {
			t.Fatalf("risk subject projection contains per-subject scan %q", forbidden)
		}
	}
}
