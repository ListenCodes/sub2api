package accountmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountMonitorWebContainsRequiredWorkflows(t *testing.T) {
	files := []string{"index.html", "app.js", "styles.css"}
	var combined strings.Builder
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join("web", name))
		if err != nil {
			t.Fatal(err)
		}
		combined.Write(raw)
	}
	content := combined.String()
	for _, marker := range []string{
		"account-overview", "account-filters", "accounts-table", "account-drawer",
		"models-tab", "users-tab", "errors-tab", "trends-tab", "attempts-tab", "media-tab",
		"data-quality", "thresholds-dialog", "rebuild-dialog",
		"setInterval", "page_size", "masked_prefix", "expand-api-keys",
		"custom-from", "custom-to", "account-id", "parent-account-id", "account-status",
		"user-id", "api-key-id", "request-type", "status-code", "data-sort",
		"account_cost", "average_duration_ms", "model_count", "user_count",
		"last_success_at", "last_failure_at", "health.reasons", "sort_by", "sort_order",
	} {
		if !strings.Contains(content, marker) {
			t.Fatalf("web assets missing %q", marker)
		}
	}
}
