package accountmonitor

import (
	"strings"
	"testing"
)

func TestSchemaContainsFactsAggregatesAndControls(t *testing.T) {
	lower := strings.ToLower(schemaSQL)
	for _, table := range []string{
		"account_monitor_attempt_facts",
		"account_monitor_request_facts",
		"account_monitor_account_minute",
		"account_monitor_account_model_minute",
		"account_monitor_account_daily",
		"account_monitor_account_model_daily",
		"account_monitor_account_user_daily",
		"account_monitor_account_error_daily",
		"account_monitor_sync_state",
		"account_monitor_rebuild_jobs",
		"account_monitor_thresholds",
		"account_monitor_group_dimensions",
		"account_monitor_group_model_10m",
	} {
		if !strings.Contains(lower, "create table if not exists "+table) {
			t.Fatalf("schema missing table %s", table)
		}
	}
	for _, required := range []string{
		"event_key text not null unique",
		"request_key text not null unique",
		"model_attribution",
		"identity_quality",
		"recovered boolean",
		"group_id bigint",
		"idx_account_monitor_request_group_time",
		"insert into account_monitor_schema_migrations(version) values (2)",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("schema missing %q", required)
		}
	}
}

func TestSchemaDefinesGroupModelTenMinutePrimaryKey(t *testing.T) {
	lower := strings.ToLower(schemaSQL)
	for _, required := range []string{
		"primary key (bucket_at, group_id, actual_model)",
		"exact_model_requests bigint",
		"estimated_model_requests bigint",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("group aggregate schema missing %q", required)
		}
	}
}

func TestRequestFactUpsertUpdatesGroupIdentity(t *testing.T) {
	lower := strings.ToLower(insertRequestSQL)
	for _, required := range []string{
		"account_id, group_id, platform",
		"group_id=excluded.group_id",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("request fact upsert missing %q", required)
		}
	}
}

func TestSchemaDoesNotStoreSecretsOrRequestPayloads(t *testing.T) {
	lower := strings.ToLower(schemaSQL)
	for _, forbidden := range []string{
		"credentials json",
		"full_api_key",
		"request_body",
		"request_headers",
		"access_token",
		"refresh_token",
		"cookie",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("schema contains forbidden field %q", forbidden)
		}
	}
}
