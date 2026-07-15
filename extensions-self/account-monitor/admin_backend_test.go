package accountmonitor

import (
	"context"
	"encoding/json"
	"errors"
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
		int64(10), int64(8), int64(2), int64(3), int64(2), int64(100), 1.2, 0.8, 210.0, 450.0,
	))
	mock.ExpectQuery(regexp.QuoteMeta(overviewRequestsSQL)).WithArgs(from, to).WillReturnRows(sqlmock.NewRows([]string{"requests", "successes"}).AddRow(int64(9), int64(8)))
	mock.ExpectQuery(regexp.QuoteMeta(syncOverviewSQL)).WillReturnRows(sqlmock.NewRows([]string{"last_sync_at", "lag"}).AddRow(to, 12.0))
	mock.ExpectQuery(regexp.QuoteMeta(selectThresholdOverridesSQL)).WillReturnRows(sqlmock.NewRows([]string{"scope_type", "scope_id", "config"}))
	mock.ExpectQuery(regexp.QuoteMeta(healthMetricsSQL)).WithArgs(to.Add(-time.Hour), to, "physical", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(healthMetricColumns()).
			AddRow(int64(1), int64(0), "openai", int64(20), int64(10), int64(10), to.Add(-time.Minute), 0, 0, 0.0, int64(20), 0.0, int64(20), 20.0, int64(100), 100.0, "限流", int64(10)).
			AddRow(int64(2), int64(0), "anthropic", int64(30), int64(30), int64(0), to.Add(-time.Minute), 0, 0, 0.0, int64(30), 0.0, int64(30), 30.0, int64(100), 100.0, "", int64(0)))

	service := NewAdminService(NewRepository(db), nil, time.Second)
	service.now = func() time.Time { return to }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{Resource: ResourceOverview, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	overview := result.(OverviewResponse)
	if overview.Attempts != 10 || overview.Requests != 9 || overview.RequestSuccesses != 8 || overview.AverageDurationMS != 210 || overview.P95DurationMS != 450 || overview.AbnormalAccounts != 1 {
		t.Fatalf("overview = %+v", overview)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminServiceAccountsUsesSavedThresholdAndOneHourMetrics(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	now := from.Add(12 * time.Hour)
	mock.ExpectQuery(`SELECT stats\.\*`).
		WithArgs(from, now, "physical", 20, 0, "", "", "", "", int64(0), int64(0), int64(0), int64(0), 0, 0, "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(accountSummaryColumns()).
			AddRow(
				int64(9), int64(7), "anthropic", int64(1000), int64(950), int64(50), int64(4000),
				12.5, 8.5, 320.0, int64(700), now.Add(-time.Minute), now.Add(-2*time.Minute),
				int64(4), int64(12), int64(0), int64(0), int64(0), int64(2), 0.95,
			).
			AddRow(
				int64(10), int64(0), "openai", int64(40), int64(40), int64(0), int64(100),
				1.0, 0.5, 200.0, int64(300), now.Add(-2*time.Hour), now.Add(-3*time.Hour),
				int64(1), int64(1), int64(0), int64(0), int64(0), int64(2), 1.0,
			))
	mock.ExpectQuery(regexp.QuoteMeta(selectThresholdOverridesSQL)).WillReturnRows(
		sqlmock.NewRows([]string{"scope_type", "scope_id", "config"}).
			AddRow("global", int64(0), []byte(`{"success_rate":0.95}`)).
			AddRow("platform", PlatformScopeID("anthropic"), []byte(`{"success_rate":0.96}`)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(healthMetricsSQL)).WithArgs(now.Add(-time.Hour), now, "physical", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(healthMetricColumns()).AddRow(
			int64(9), int64(7), "anthropic", int64(20), int64(18), int64(2), now.Add(-time.Minute), 0, 3, 0.0, int64(20), 0.0, int64(20), 20.0, int64(300), 300.0, "限流", int64(2),
		))

	service := NewAdminService(NewRepository(db), nil, time.Second)
	service.now = func() time.Time { return now }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: now, Page: 1, PageSize: 20, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := result.(PageResponse).Items.([]AccountSummary)
	if len(items) != 2 || items[0].Health.Level != HealthCritical || items[1].Health.Level != HealthNormal {
		t.Fatalf("accounts = %+v", items)
	}
	reason := strings.Join(items[0].Health.Reasons, " ")
	for _, want := range []string{"认证失效或额度不足 3 次", "近 1 小时调用 20 次", "成功率 90.0%", "低于 96.0% 阈值"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("health reason %q missing %q", reason, want)
		}
	}
	if strings.Contains(reason, "1000") {
		t.Fatalf("health reason used all-range attempts: %q", reason)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountSortClauseUsesWhitelist(t *testing.T) {
	if got := accountSortClause("success_rate", "asc"); got != "success_rate ASC, rollup_account_id ASC" {
		t.Fatalf("sort = %q", got)
	}
	if got := accountSortClause("account_cost", "desc"); got != "account_cost DESC, rollup_account_id ASC" {
		t.Fatalf("account cost sort = %q", got)
	}
	if got := accountSortClause("attempts; DROP TABLE x", "asc"); got != "attempts DESC, rollup_account_id ASC" {
		t.Fatalf("unsafe sort = %q", got)
	}
}

func TestAdminServiceAccountsPassesFactFiltersToServerQuery(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	mock.ExpectQuery(`SELECT stats\.\*`).WithArgs(
		from, to, "physical", 20, 0, "anthropic", "claude", "failed", "限流",
		int64(9), int64(7), int64(11), int64(13), 2, 429, "", sqlmock.AnyArg(),
	).WillReturnRows(sqlmock.NewRows(accountSummaryColumns()))

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: to, Page: 1, PageSize: 20,
		Query: map[string]string{
			"platform": "anthropic", "model": "claude", "result": "failed", "error_category": "限流",
			"account_id": "9", "parent_account_id": "7", "user_id": "11", "api_key_id": "13",
			"request_type": "2", "status_code": "429",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.(PageResponse).Total != 0 {
		t.Fatalf("result = %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminServiceAccountsFiltersStatusThroughSafeDimensions(t *testing.T) {
	repoDB, repoMock := newSourceMock(t)
	sourceDB, sourceMock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	sourceMock.ExpectQuery(regexp.QuoteMeta(accountIDsByStatusQuery)).WithArgs("active").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)).AddRow(int64(10)))
	repoMock.ExpectQuery(`SELECT stats\.\*`).WithArgs(
		from, to, "physical", 20, 0, "", "", "", "", int64(0), int64(0), int64(0), int64(0), 0, 0, "active", sqlmock.AnyArg(),
	).WillReturnRows(sqlmock.NewRows(accountSummaryColumns()))

	result, err := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second).
		ExecuteAdmin(context.Background(), AdminRequest{
			Resource: ResourceAccounts, From: from, To: to, Page: 1, PageSize: 20,
			Query: map[string]string{"account_status": "active"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.(PageResponse).Total != 0 {
		t.Fatalf("result = %+v", result)
	}
	if err := repoMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := sourceMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDetailQueriesApplyServerSideFilters(t *testing.T) {
	for name, query := range map[string]string{
		"models": modelsSQL, "users": usersSQL, "errors": errorsSQL, "attempts": attemptsSQL,
	} {
		for _, marker := range []string{"platform=", "actual_model", "result=", "error_category=", "user_id=", "api_key_id=", "request_type=", "status_code="} {
			if !strings.Contains(query, marker) {
				t.Fatalf("%s query missing server filter %q", name, marker)
			}
		}
	}
}

func TestAdminServiceAttemptsPassesDetailFilters(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(attemptsSQL)).WithArgs(
		from, to, int64(9), 20, 0, "anthropic", "claude", "failed", "限流", int64(11), int64(13), 2, 429,
	).WillReturnRows(sqlmock.NewRows([]string{}))

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAttempts, From: from, To: to, Page: 1, PageSize: 20,
		Query: map[string]string{
			"account_id": "9", "platform": "anthropic", "model": "claude", "result": "failed",
			"error_category": "限流", "user_id": "11", "api_key_id": "13", "request_type": "2", "status_code": "429",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.(PageResponse).Total != 0 {
		t.Fatalf("result = %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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

func TestAdminServiceDataQualityChecksSourceConnectivity(t *testing.T) {
	for _, test := range []struct {
		name      string
		pingError error
		connected bool
	}{
		{name: "connected", connected: true},
		{name: "disconnected", pingError: errors.New("source unavailable"), connected: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repoDB, repoMock := newSourceMock(t)
			sourceDB, sourceMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sourceDB.Close() })
			if test.pingError != nil {
				sourceMock.ExpectPing().WillReturnError(test.pingError)
			} else {
				sourceMock.ExpectPing()
			}

			from := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
			to := from.Add(time.Hour)
			repoMock.ExpectQuery(regexp.QuoteMeta(dataQualitySQL)).WithArgs(from, to).
				WillReturnRows(sqlmock.NewRows([]string{"exact", "estimated", "fallback", "recovered"}).AddRow(int64(8), int64(2), int64(1), int64(3)))
			repoMock.ExpectQuery(regexp.QuoteMeta(requestQualitySQL)).WithArgs(from, to).
				WillReturnRows(sqlmock.NewRows([]string{"unattributed", "failed"}).AddRow(int64(1), int64(4)))

			result, err := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second).
				ExecuteAdmin(context.Background(), AdminRequest{Resource: ResourceDataQuality, From: from, To: to})
			if err != nil {
				t.Fatal(err)
			}
			quality := result.(DataQualityResponse)
			if quality.SourceConnected != test.connected || quality.ExactModels != 8 || quality.UnattributedErrors != 1 {
				t.Fatalf("quality = %+v", quality)
			}
			if err := repoMock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if err := sourceMock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAdminQueriesDoNotReadMainTables(t *testing.T) {
	joined := strings.ToLower(strings.Join([]string{overviewAttemptsSQL, overviewRequestsSQL, accountBaseSQL, modelsSQL, usersSQL, errorsSQL, trendsSQL, attemptsSQL, healthMetricsSQL, dataQualitySQL}, "\n"))
	for _, forbidden := range []string{" usage_logs", " ops_error_logs", " accounts ", " api_keys ", " users "} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("admin query reads main table %q", forbidden)
		}
	}
}

func TestHealthMetricsSQLPinsTimestampParameterTypes(t *testing.T) {
	for _, fragment := range []string{"$1::timestamptz", "$2::timestamptz"} {
		if !strings.Contains(healthMetricsSQL, fragment) {
			t.Fatalf("health metrics SQL must contain %q so PostgreSQL does not infer an interval parameter", fragment)
		}
	}
}

func overviewAttemptColumns() []string {
	return []string{"attempts", "successes", "failures", "active_accounts", "users", "tokens", "user_cost", "account_cost", "average", "p95"}
}

func accountSummaryColumns() []string {
	return []string{"account_id", "parent_account_id", "platform", "attempts", "successes", "failures", "tokens", "user_cost", "account_cost", "average_duration_ms", "p95_duration_ms", "last_success_at", "last_failure_at", "model_count", "user_count", "image_count", "video_count", "video_duration_seconds", "total", "success_rate"}
}

func healthMetricColumns() []string {
	return []string{"account_id", "parent_account_id", "platform", "attempts", "successes", "failures", "last_success_at", "consecutive_model_failures", "auth_quota_failures_15m", "rate_overload_ratio_15m", "attempts_24h", "top_user_ratio_24h", "current_hour_volume", "baseline_hour_volume", "p95_duration_ms", "baseline_p95_duration_ms", "top_error_category", "top_error_count"}
}
