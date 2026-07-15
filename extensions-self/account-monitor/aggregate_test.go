package accountmonitor

import (
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
