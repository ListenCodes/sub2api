package accountmonitor

import (
	"os"
	"path/filepath"
	"reflect"
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
		"groups":         groupDimensionQuery,
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
		"create or replace view extensions_self_ro.account_group_dimension",
		"create or replace view extensions_self_ro.user_dimension",
		"create or replace view extensions_self_ro.api_key_dimension",
		"create or replace view extensions_self_ro.group_dimension",
		"revoke all on schema extensions_self_ro from public",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("safe view SQL missing %q", required)
		}
	}
}

func TestSourceContractsIncludeAccountInventoryGroups(t *testing.T) {
	raw, err := os.ReadFile("source.go")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, required := range []string{
		"accountgroupdimension",
		"readaccountdimensions",
		"readaccountgroupdimensions",
		"extensions_self_ro.account_group_dimension",
		"account_id",
		"group_id",
		"group_name",
		"group_platform",
		"group_status",
		"group_rate_multiplier",
		"group_deleted_at",
	} {
		if !strings.Contains(lower, required) {
			t.Errorf("account inventory source contract missing %q", required)
		}
	}
}

func TestSourceContractsIncludeGroupIdentity(t *testing.T) {
	for name, query := range map[string]string{
		"usage":  usageSourceQuery,
		"errors": errorSourceQuery,
	} {
		if !strings.Contains(strings.ToLower(query), "group_id") {
			t.Errorf("%s source query does not select group_id", name)
		}
	}

	for _, tc := range []struct {
		typeName string
		typeOf   reflect.Type
		field    string
	}{
		{typeName: "UsageSourceRow", typeOf: reflect.TypeOf(UsageSourceRow{}), field: "GroupID"},
		{typeName: "ErrorSourceRow", typeOf: reflect.TypeOf(ErrorSourceRow{}), field: "GroupID"},
		{typeName: "DimensionIDs", typeOf: reflect.TypeOf(DimensionIDs{}), field: "GroupIDs"},
		{typeName: "Dimensions", typeOf: reflect.TypeOf(Dimensions{}), field: "Groups"},
	} {
		if _, ok := tc.typeOf.FieldByName(tc.field); !ok {
			t.Errorf("%s is missing %s", tc.typeName, tc.field)
		}
	}
	if _, ok := reflect.TypeOf(&PostgresSource{}).MethodByName("ReadGroupDimensions"); !ok {
		t.Error("PostgresSource is missing ReadGroupDimensions")
	}
}
