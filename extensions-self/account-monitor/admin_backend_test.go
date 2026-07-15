package accountmonitor

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAdminServiceOverviewUsesAttemptAndRequestFacts(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(overviewAttemptsSQL)).WithArgs(from, to).WillReturnRows(sqlmock.NewRows(overviewAttemptColumns()).AddRow(
		int64(10), int64(8), int64(2), int64(3), int64(2), int64(100), 1.2, 0.8, 450.0,
	))
	mock.ExpectQuery(regexp.QuoteMeta(overviewRequestsSQL)).WithArgs(from, to).WillReturnRows(sqlmock.NewRows([]string{"requests", "successes"}).AddRow(int64(9), int64(8)))
	mock.ExpectQuery(regexp.QuoteMeta(syncOverviewSQL)).WillReturnRows(sqlmock.NewRows([]string{"last_sync_at", "lag"}).AddRow(to, 12.0))

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{Resource: ResourceOverview, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	overview := result.(OverviewResponse)
	if overview.Attempts != 10 || overview.Requests != 9 || overview.RequestSuccesses != 8 || overview.P95DurationMS != 450 {
		t.Fatalf("overview = %+v", overview)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountSortClauseUsesWhitelist(t *testing.T) {
	if got := accountSortClause("success_rate", "asc"); got != "success_rate ASC, rollup_account_id ASC" {
		t.Fatalf("sort = %q", got)
	}
	if got := accountSortClause("attempts; DROP TABLE x", "asc"); got != "attempts DESC, rollup_account_id ASC" {
		t.Fatalf("unsafe sort = %q", got)
	}
}

func TestAdminServiceUpdatesGlobalThreshold(t *testing.T) {
	db, mock := newSourceMock(t)
	body, _ := json.Marshal(map[string]any{"scope": "global", "scope_id": 0, "success_rate": 0.87})
	mock.ExpectExec(regexp.QuoteMeta(upsertThresholdSQL)).WithArgs("global", int64(0), sqlmock.AnyArg(), int64(9)).WillReturnResult(sqlmock.NewResult(1, 1))

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{Resource: ResourceThresholds, Method: "PUT", ActorID: 9, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if result.(ThresholdResponse).SuccessRate != 0.87 {
		t.Fatalf("threshold = %+v", result)
	}
}

func TestAdminQueriesDoNotReadMainTables(t *testing.T) {
	joined := strings.ToLower(strings.Join([]string{overviewAttemptsSQL, overviewRequestsSQL, accountBaseSQL, modelsSQL, usersSQL, errorsSQL, attemptsSQL, dataQualitySQL}, "\n"))
	for _, forbidden := range []string{" usage_logs", " ops_error_logs", " accounts ", " api_keys ", " users "} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("admin query reads main table %q", forbidden)
		}
	}
}

func overviewAttemptColumns() []string {
	return []string{"attempts", "successes", "failures", "active_accounts", "users", "tokens", "user_cost", "account_cost", "p95"}
}
