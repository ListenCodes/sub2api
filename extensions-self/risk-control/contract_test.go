package main

import (
	"encoding/json"
	"os"
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

func TestSchemaMigratesLegacyRegistrationRulesToExplicitStrategies(t *testing.T) {
	for _, expected := range []string{
		"ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS count_strategy",
		"WHEN 'associated_events' THEN 'user_events'",
		"WHEN 'subject_device_events' THEN 'email_subject_events'",
		"WHEN 'ip_distinct_subjects' THEN 'ip_distinct_success_users'",
	} {
		if !strings.Contains(schemaSQL, expected) {
			t.Fatalf("schema is missing %q", expected)
		}
	}
}

func TestSchemaDisablesUnsafeLegacyEventRuleSemantics(t *testing.T) {
	for _, expected := range []string{
		"jsonb_array_elements_text(event_types)",
		"count_strategy='email_subject_events'",
		"count_strategy IN ('ip_distinct_success_users','browser_instance_distinct_success_users','ip_browser_cooccurrence')",
		"INSERT INTO risk_schema_migrations(version) VALUES (5)",
	} {
		if !strings.Contains(schemaSQL, expected) {
			t.Fatalf("schema is missing unsafe-rule migration fragment %q", expected)
		}
	}
}

func TestIdentitySchemaStageZeroDoesNotMutateExistingV1Rules(t *testing.T) {
	for _, forbidden := range []string{
		"WHERE code = 'registration_abuse'",
		"WHERE code IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')",
	} {
		if strings.Contains(schemaSQL, forbidden) {
			t.Fatalf("Stage 0 schema must not mutate existing V1 rules: found %q", forbidden)
		}
	}
	if !strings.Contains(schemaSQL, "AND code NOT IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')") {
		t.Fatal("legacy strategy migration must exclude activation-gated V1 rules")
	}
	if !strings.Contains(schemaSQL, "risk_identity_shadow_activation") {
		t.Fatal("schema does not persist the initial 14-day Shadow activation")
	}
}

func TestLegacyV1CleanupIsActivationGatedAndAudited(t *testing.T) {
	for _, expected := range []string{"legacyV1CleanupMigrationVersion = 3", "purge_legacy_v1", "DELETE FROM risk_events WHERE identity_version='legacy_v1'", "DELETE FROM risk_subjects"} {
		if !strings.Contains(identityDatabaseSourceForContract(t), expected) {
			t.Fatalf("identity cleanup is missing %q", expected)
		}
	}
	if !strings.Contains(schemaSQL, "risk_reject_retired_v1_rule_insert") {
		t.Fatal("schema is missing the retired V1 rule rollback guard")
	}
}

func TestSchemaReclassifiesOnlyPostCleanupV1Events(t *testing.T) {
	for _, expected := range []string{
		"SELECT pg_advisory_xact_lock(7357811167603551941)",
		"LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE",
		"risk_normalize_post_cleanup_event_version",
		"BEFORE INSERT OR UPDATE OF identity_version ON risk_events",
		"PERFORM 1 FROM risk_schema_migrations WHERE version=3 FOR SHARE",
		"EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=3)",
		"NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=6)",
		"SET identity_version='event_v2'",
		"WHERE identity_version='legacy_v1'",
		"reclassify_post_cleanup_v1_events",
		"jsonb_build_object('events_reclassified',corrected_events)",
		"INSERT INTO risk_schema_migrations(version) VALUES (6)",
	} {
		if !strings.Contains(schemaSQL, expected) {
			t.Fatalf("post-cleanup event reclassification is missing %q", expected)
		}
	}
	if strings.Index(schemaSQL, "SELECT pg_advisory_xact_lock(7357811167603551941)") > strings.Index(schemaSQL, "CREATE TABLE IF NOT EXISTS risk_rules") {
		t.Fatal("schema must take the shared schema/cleanup advisory lock before touching risk tables")
	}
	if strings.Index(schemaSQL, "LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE") > strings.Index(schemaSQL, "CREATE INDEX IF NOT EXISTS idx_risk_events_user_created") {
		t.Fatal("schema must lock legacy writers before altering or indexing risk events")
	}
}

func TestLegacyCleanupUsesSchemaAdvisoryLock(t *testing.T) {
	source := identityDatabaseSourceForContract(t)
	markerCheck := "SELECT EXISTS(SELECT 1 FROM risk_schema_migrations WHERE version=$1)"
	for _, expected := range []string{
		"riskSchemaAdvisoryLockID int64 = 7357811167603551941",
		"SELECT pg_advisory_xact_lock($1)",
		"LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE",
		"riskSchemaAdvisoryLockID",
		"LOCK TABLE risk_schema_migrations IN EXCLUSIVE MODE",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("legacy cleanup serialization is missing %q", expected)
		}
	}
	if strings.Index(source, "LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE") > strings.Index(source, "LOCK TABLE risk_schema_migrations IN EXCLUSIVE MODE") {
		t.Fatal("legacy cleanup must stop event writers before locking the migration marker")
	}
	if strings.Count(source, markerCheck) < 2 || strings.Index(source, markerCheck) > strings.Index(source, "LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE") {
		t.Fatal("legacy cleanup must fast-path an existing marker before locking events and recheck under both locks")
	}
}

func identityDatabaseSourceForContract(t *testing.T) string {
	t.Helper()
	payload, err := os.ReadFile("identity_db.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
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
	for _, expected := range []string{"WHERE user_id=$1 AND ", "WHERE subject.user_id=$1 AND "} {
		if !strings.Contains(riskSubjectProjectionByUserCTE, expected) {
			t.Fatalf("single-subject projection is missing scope %q", expected)
		}
	}
}
