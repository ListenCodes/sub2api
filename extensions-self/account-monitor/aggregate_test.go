package accountmonitor

import (
	"os"
	"strings"
	"testing"
)

func TestAggregateRefreshCoversAllTablesAndMetrics(t *testing.T) {
	joined := strings.ToLower(strings.Join(refreshAggregateSQL, "\n"))
	for _, table := range []string{
		"account_monitor_account_minute",
		"account_monitor_account_model_minute",
		"account_monitor_account_daily",
		"account_monitor_account_model_daily",
		"account_monitor_account_user_daily",
		"account_monitor_account_error_daily",
	} {
		if !strings.Contains(joined, table) {
			t.Fatalf("aggregate SQL missing %s", table)
		}
	}
	for _, metric := range []string{
		"percentile_disc(0.95)",
		"cache_creation_tokens",
		"cache_read_tokens",
		"image_count",
		"video_count",
		"video_duration_seconds",
		"recovered",
	} {
		if !strings.Contains(joined, metric) {
			t.Fatalf("aggregate SQL missing metric %s", metric)
		}
	}
}

func TestGroupAggregateUsesFinalRequestsAndCompleteTenMinuteBuckets(t *testing.T) {
	raw, err := os.ReadFile("aggregate.go")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, required := range []string{
		"account_monitor_group_model_10m",
		"account_monitor_request_facts",
		"date_bin('10 minutes'",
		"current_timestamp",
		"exact_model_requests",
		"estimated_model_requests",
		"group_id is not null",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("group aggregate SQL missing %q", required)
		}
	}
}

func TestAggregateRefreshDeletesBeforeReinsertInSeparateStatements(t *testing.T) {
	if len(refreshAggregateSQL) != 12 {
		t.Fatalf("aggregate refresh statements = %d, want 12 delete/insert statements", len(refreshAggregateSQL))
	}
	for index, query := range refreshAggregateSQL {
		prefix := "delete from"
		if index%2 == 1 {
			prefix = "insert into"
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(query)), prefix) {
			t.Fatalf("aggregate refresh statement %d must start with %q", index, prefix)
		}
	}
}

func TestDailyAggregateRefreshUsesUTCBoundaries(t *testing.T) {
	utcFrom := "(($1::timestamptz at time zone 'utc')::date::timestamp at time zone 'utc')"
	utcTo := "(((($2::timestamptz at time zone 'utc')::date + 1)::timestamp) at time zone 'utc')"
	for _, index := range []int{5, 7, 9, 11} {
		query := strings.ToLower(refreshAggregateSQL[index])
		if !strings.Contains(query, utcFrom) || !strings.Contains(query, utcTo) {
			t.Fatalf("daily aggregate insert %d must use UTC fact boundaries", index)
		}
		if strings.Contains(query, "date_trunc('day'") {
			t.Fatalf("daily aggregate insert %d uses the database session timezone", index)
		}
	}
}
