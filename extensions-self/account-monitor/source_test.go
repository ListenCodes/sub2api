package accountmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceQueriesUseOnlySafeViews(t *testing.T) {
	queries := map[string]string{
		"usage":          usageSourceQuery,
		"errors":         errorSourceQuery,
		"accounts":       accountDimensionQuery,
		"account_status": accountIDsByStatusQuery,
		"users":          userDimensionQuery,
		"api_keys":       apiKeyDimensionQuery,
	}

	for name, query := range queries {
		lower := strings.ToLower(query)
		if !strings.Contains(lower, "extensions_self_ro.") {
			t.Fatalf("%s query does not use the safe schema: %s", name, query)
		}
		for _, forbidden := range []string{" from usage_logs", " from ops_error_logs", " from accounts", " from users", " from api_keys"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s query reads a raw table: %s", name, query)
			}
		}
	}
}

func TestSourceViewSQLDoesNotExposeSensitiveColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("sql", "main_source_views.sql"))
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))

	for _, forbidden := range []string{
		"a.credentials",
		"accounts.credentials",
		"k.key,",
		"api_keys.key,",
		"request_body",
		"request_headers",
		"error_body",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("safe view SQL exposes forbidden field %q", forbidden)
		}
	}
	for _, required := range []string{
		"create schema if not exists extensions_self_ro",
		"create or replace view extensions_self_ro.usage_source",
		"create or replace view extensions_self_ro.error_source",
		"create or replace view extensions_self_ro.account_dimension",
		"create or replace view extensions_self_ro.user_dimension",
		"create or replace view extensions_self_ro.api_key_dimension",
		"revoke all on schema extensions_self_ro from public",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("safe view SQL missing %q", required)
		}
	}
}
