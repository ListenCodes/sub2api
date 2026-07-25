package accountmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGroupMonitorQueriesUseOnlyDimensionsAndRequestFacts(t *testing.T) {
	raw, err := os.ReadFile("admin_backend.go")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.ToLower(string(raw))
	for _, required := range []string{
		"account_monitor_group_dimensions",
		"account_monitor_request_facts",
		"lower(platform),lower(name),group_id",
		"group_id=any",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("group monitor query missing %q", required)
		}
	}
	for name, query := range map[string]string{"list": groupTimelineSQL, "detail": groupModelTimelineSQL} {
		lower := strings.ToLower(query)
		if !strings.Contains(lower, "account_monitor_request_facts") {
			t.Fatalf("%s query does not use request facts: %s", name, query)
		}
		if strings.Contains(lower, "account_monitor_group_model_10m") {
			t.Fatalf("%s query still uses incompatible ten-minute aggregates: %s", name, query)
		}
	}
	if !strings.Contains(groupModelTimelineSQL, "COALESCE(NULLIF(actual_model,''),'未知实际模型')") {
		t.Fatalf("group detail query does not normalize blank actual models: %s", groupModelTimelineSQL)
	}
}

func TestGroupMonitorQueriesUseAdaptiveDisplayBuckets(t *testing.T) {
	for name, query := range map[string]string{"list": groupTimelineSQL, "detail": groupModelTimelineSQL} {
		lower := strings.ToLower(query)
		for _, marker := range []string{"date_bin", "make_interval", "secs => $4", "1970-01-01 00:00:00+00"} {
			if !strings.Contains(lower, marker) {
				t.Fatalf("%s query missing %q: %s", name, marker, query)
			}
		}
	}
}

func TestAccountTrendQueryReturnsNewestBucketFirst(t *testing.T) {
	if !strings.Contains(strings.ToLower(trendsSQL), "order by 1 desc") {
		t.Fatalf("trend query is not newest first: %s", trendsSQL)
	}
}

func TestGroupMonitorListReturnsTwentyFourSevenHourBuckets(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	to := from.Add(7 * 24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(groupDimensionsSQL)).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "name", "platform", "status", "deleted_at"}).
			AddRow(int64(7), "Primary", "openai", "active", nil),
	)
	mock.ExpectQuery(regexp.QuoteMeta(groupTimelineSQL)).WithArgs(from, to, sqlmock.AnyArg(), 25200).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "bucket_at", "total", "successes", "failures"}).
			AddRow(int64(7), from.In(time.FixedZone("Asia/Shanghai", 8*60*60)), int64(6), int64(4), int64(2)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(syncQualitySQL)).WillReturnRows(
		sqlmock.NewRows([]string{"source", "cursor_time", "cursor_id", "last_success_at", "last_error", "updated_at"}),
	)
	mock.ExpectQuery(regexp.QuoteMeta(sharedQualityFactsSQL)).WithArgs(from, to).WillReturnRows(
		sqlmock.NewRows([]string{"available_from", "available_to", "missing_group", "exact", "estimated"}).
			AddRow(from, to, int64(0), int64(6), int64(0)),
	)

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceGroupMonitorGroups, From: from, To: to, Page: 1, PageSize: 12,
		BucketSeconds: 25200, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(GroupMonitorGroupsResponse)
	if groupResponseBucketSeconds(t, page) != 25200 || len(page.Items) != 1 || len(page.Items[0].Timeline) != 24 {
		t.Fatalf("seven-hour page = %+v", page)
	}
	if page.Items[0].TotalRequests != 6 || page.Items[0].Successes != 4 || page.Items[0].Failures != 2 || page.Items[0].Timeline[0].Total != 6 {
		t.Fatalf("hourly card = %+v", page.Items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupMonitorDetailReturnsTwentyFourThirtyHourBuckets(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	to := from.Add(30 * 24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(groupDimensionByIDSQL)).WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "name", "platform", "status", "deleted_at"}).
			AddRow(int64(7), "Primary", "openai", "active", nil),
	)
	mock.ExpectQuery(regexp.QuoteMeta(groupModelTimelineSQL)).WithArgs(int64(7), from, to, 108000).WillReturnRows(
		sqlmock.NewRows([]string{"model", "bucket", "total", "successes", "failures", "exact", "estimated"}).
			AddRow("gpt-5", from.In(time.FixedZone("Asia/Shanghai", 8*60*60)), int64(36), int64(30), int64(6), int64(30), int64(6)),
	)

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceGroupMonitorGroup, GroupID: 7, From: from, To: to,
		BucketSeconds: 108000, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail := result.(GroupMonitorDetailResponse)
	if groupResponseBucketSeconds(t, detail) != 108000 || len(detail.Models) != 1 || len(detail.Models[0].Timeline) != 24 {
		t.Fatalf("thirty-hour detail = %+v", detail)
	}
	if detail.Group.TotalRequests != 36 || detail.Models[0].TotalRequests != 36 || detail.Group.Failures != 6 || detail.Models[0].Failures != 6 {
		t.Fatalf("six-hour totals = group:%+v model:%+v", detail.Group, detail.Models[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGroupMonitorCardDistinguishesBucketAndIdleStates(t *testing.T) {
	from := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	to := from.Add(30 * time.Minute)
	dimension := GroupDimension{ID: 7, Name: "Primary", Platform: "openai", Status: "active"}
	tests := []struct {
		name    string
		buckets map[time.Time]GroupMonitorBucket
		status  string
	}{
		{name: "no data", buckets: nil, status: "no_data"},
		{name: "recently idle", buckets: map[time.Time]GroupMonitorBucket{from: {Total: 1, Successes: 1}}, status: "recently_idle"},
		{name: "normal", buckets: map[time.Time]GroupMonitorBucket{to.Add(-15 * time.Minute): {Total: 2, Successes: 2}}, status: "normal"},
		{name: "partial failure", buckets: map[time.Time]GroupMonitorBucket{to.Add(-15 * time.Minute): {Total: 2, Successes: 1, Failures: 1}}, status: "partial_failure"},
		{name: "all failed", buckets: map[time.Time]GroupMonitorBucket{to.Add(-15 * time.Minute): {Total: 2, Failures: 2}}, status: "all_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			card := buildGroupCard(dimension, from, to, test.buckets, 900)
			if len(card.Timeline) != 2 || card.CallStatus != test.status {
				t.Fatalf("card = %+v", card)
			}
		})
	}
}

func TestBuildGroupCardMatchesBucketInstantsAcrossTimeZones(t *testing.T) {
	from := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	bucketAt := from.In(shanghai)

	card := buildGroupCard(
		GroupDimension{ID: 7, Name: "Primary", Platform: "openai", Status: "active"},
		from,
		to,
		map[time.Time]GroupMonitorBucket{
			bucketAt: {BucketAt: bucketAt, Total: 3, Successes: 2, Failures: 1},
		},
	)

	if card.TotalRequests != 3 || card.Successes != 2 || card.Failures != 1 {
		t.Fatalf("card totals = %d/%d/%d, want 3/2/1", card.TotalRequests, card.Successes, card.Failures)
	}
	if len(card.Timeline) != 2 || card.Timeline[0].Total != 3 {
		t.Fatalf("timeline = %+v, want first cross-zone bucket populated", card.Timeline)
	}
}

func TestAdminServiceGroupMonitorDefaultsToActiveGroups(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(groupDimensionsSQL)).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "name", "platform", "status", "deleted_at"}).
			AddRow(int64(1), "Alpha", "openai", "active", nil).
			AddRow(int64(2), "Beta", "openai", "inactive", nil),
	)
	mock.ExpectQuery(regexp.QuoteMeta(groupTimelineSQL)).WithArgs(from, to, sqlmock.AnyArg(), 900).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "bucket_at", "total", "successes", "failures"}).
			AddRow(int64(1), to.Add(-15*time.Minute), int64(4), int64(3), int64(1)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(syncQualitySQL)).WillReturnRows(
		sqlmock.NewRows([]string{"source", "cursor_time", "cursor_id", "last_success_at", "last_error", "updated_at"}),
	)
	mock.ExpectQuery(regexp.QuoteMeta(sharedQualityFactsSQL)).WithArgs(from, to).WillReturnRows(
		sqlmock.NewRows([]string{"available_from", "available_to", "missing_group", "exact", "estimated"}).
			AddRow(from, to.Add(-time.Minute), int64(2), int64(8), int64(1)),
	)

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceGroupMonitorGroups, From: from, To: to, Page: 1, PageSize: 12, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(GroupMonitorGroupsResponse)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GroupID != 1 || page.Items[0].CallStatus != "partial_failure" {
		t.Fatalf("group page = %+v", page)
	}
	if page.Quality.MissingGroupRequests != 2 || page.Quality.ExactModelRequests != 8 {
		t.Fatalf("quality = %+v", page.Quality)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFilterAndPrioritizeGroupCardsPutsCallsFirstAndSupportsHasCalls(t *testing.T) {
	cards := []GroupMonitorCard{
		{GroupID: 1, Name: "A Zero", CallStatus: "no_data"},
		{GroupID: 2, Name: "B Normal", CallStatus: "normal", TotalRequests: 3},
		{GroupID: 3, Name: "C Failed", CallStatus: "all_failed", TotalRequests: 2},
		{GroupID: 4, Name: "D Zero", CallStatus: "no_data"},
	}

	prioritized := filterAndPrioritizeGroupCards(cards, "")
	if got := groupCardIDs(prioritized); !reflect.DeepEqual(got, []int64{2, 3, 1, 4}) {
		t.Fatalf("calls-first IDs = %v", got)
	}
	hasCalls := filterAndPrioritizeGroupCards(cards, "has_calls")
	if got := groupCardIDs(hasCalls); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("has-calls IDs = %v", got)
	}
	failed := filterAndPrioritizeGroupCards(cards, "all_failed")
	if got := groupCardIDs(failed); !reflect.DeepEqual(got, []int64{3}) {
		t.Fatalf("failed IDs = %v", got)
	}
}

func groupCardIDs(cards []GroupMonitorCard) []int64 {
	result := make([]int64, len(cards))
	for index, card := range cards {
		result[index] = card.GroupID
	}
	return result
}

func TestAdminServiceGroupDetailBuildsModelTimelines(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	to := from.Add(30 * time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta(groupDimensionByIDSQL)).WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"group_id", "name", "platform", "status", "deleted_at"}).AddRow(int64(7), "Primary", "openai", "active", nil),
	)
	mock.ExpectQuery(regexp.QuoteMeta(groupModelTimelineSQL)).WithArgs(int64(7), from, to, 900).WillReturnRows(
		sqlmock.NewRows([]string{"model", "bucket", "total", "successes", "failures", "exact", "estimated"}).
			AddRow("gpt-5", from, int64(2), int64(2), int64(0), int64(2), int64(0)).
			AddRow("gpt-5", from.Add(15*time.Minute), int64(1), int64(0), int64(1), int64(0), int64(1)),
	)

	result, err := NewAdminService(NewRepository(db), nil, time.Second).ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceGroupMonitorGroup, GroupID: 7, From: from, To: to, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail := result.(GroupMonitorDetailResponse)
	if detail.Group.TotalRequests != 3 || len(detail.Models) != 1 || len(detail.Models[0].Timeline) != 2 || detail.Models[0].EstimatedModelRequests != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

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
			AddRow(int64(1), int64(0), "openai", int64(20), int64(10), int64(10), to.Add(-time.Minute), 0, 3, 0.0, int64(20), 0.0, int64(20), 20.0, int64(100), 100.0, "限流", int64(10)).
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
	raw, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["average_risk_score"] != 50.0 || payload["high_risk_accounts"] != float64(1) {
		t.Fatalf("overview risk payload = %s", raw)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRiskFilterAndSortKeepUnavailableAccountsLast(t *testing.T) {
	items := []AccountSummary{
		{AccountID: 3, Health: Health{RiskScore: 0, RiskScoreAvailable: false}},
		{AccountID: 2, Health: Health{RiskScore: 20, RiskScoreAvailable: true}},
		{AccountID: 1, Health: Health{RiskScore: 20, RiskScoreAvailable: true}},
		{AccountID: 4, Health: Health{RiskScore: 70, RiskScoreAvailable: true}},
	}
	sortAccountsByRisk(items, "asc")
	got := []int64{items[0].AccountID, items[1].AccountID, items[2].AccountID, items[3].AccountID}
	want := []int64{1, 2, 4, 3}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ascending account IDs = %v, want %v", got, want)
		}
	}

	filtered := filterAccountsByRisk(items, map[string]string{"min_risk_score": "20", "max_risk_score": "20"})
	if len(filtered) != 2 || filtered[0].AccountID != 1 || filtered[1].AccountID != 2 {
		t.Fatalf("filtered accounts = %+v", filtered)
	}
}

func TestAdminServiceAccountsUsesFullInventory(t *testing.T) {
	repoDB, repoMock := newSourceMock(t)
	sourceDB, sourceMock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	expectAccountInventory(sourceMock)
	repoMock.ExpectQuery(`SELECT stats\.\*`).WillReturnRows(
		sqlmock.NewRows(accountSummaryColumns()).AddRow(
			int64(2), nil, "openai", int64(8), int64(7), int64(1), int64(120),
			1.5, 0.8, 200.0, int64(350), to.Add(-time.Minute), to.Add(-2*time.Minute),
			int64(1), int64(2), int64(0), int64(0), int64(0), int64(1), 0.875,
		),
	)
	expectAccountHealth(repoMock, to, int64(2))

	service := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second)
	service.now = func() time.Time { return to }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: to, Page: 1, PageSize: 20, Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(PageResponse)
	items := page.Items.([]AccountSummary)
	if page.Total != 3 || len(items) != 3 {
		t.Fatalf("full inventory page = %+v items=%+v", page, items)
	}
	if len(page.Groups) != 3 || page.Groups[0].GroupID != 11 || page.Groups[1].GroupID != 12 || page.Groups[2].GroupID != 10 {
		t.Fatalf("full inventory group options = %+v", page.Groups)
	}
	idle := accountSummaryByID(t, items, 1)
	if idle.Attempts != 0 || idle.Successes != 0 || idle.Failures != 0 || idle.Health.RiskScoreAvailable {
		t.Fatalf("idle account = %+v, want zero metrics and unavailable risk", idle)
	}
	multi := accountSummaryByID(t, items, 3)
	groups := accountSummaryGroups(t, multi)
	if len(groups) != 2 || groups[0].GroupID != 11 || groups[1].GroupID != 12 {
		t.Fatalf("multi-account groups = %+v", groups)
	}
	if groups[0].RateMultiplier != 1.5 || groups[1].RateMultiplier != 2 {
		t.Fatalf("multi-account group rates = %+v, want 1.5 and 2", groups)
	}
	if err := repoMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := sourceMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminServiceAccountsFiltersMultipleGroups(t *testing.T) {
	tests := []struct {
		name       string
		query      map[string]string
		factRows   *sqlmock.Rows
		wantIDs    []int64
		wantGroups []int64
	}{
		{
			name:  "concrete group keeps every membership",
			query: map[string]string{"group_id": "11"},
			factRows: sqlmock.NewRows(accountSummaryColumns()).AddRow(
				int64(3), nil, "anthropic", int64(4), int64(4), int64(0), int64(20),
				0.4, 0.2, 150.0, int64(220), time.Date(2026, 7, 15, 0, 50, 0, 0, time.UTC), time.Time{},
				int64(1), int64(1), int64(0), int64(0), int64(0), int64(1), 1.0,
			),
			wantIDs: []int64{3}, wantGroups: []int64{11, 12},
		},
		{
			name:     "deleted membership counts as ungrouped",
			query:    map[string]string{"group_id": "ungrouped"},
			factRows: sqlmock.NewRows(accountSummaryColumns()),
			wantIDs:  []int64{1},
		},
		{
			name:  "fact filter excludes zero matches",
			query: map[string]string{"model": "gpt"},
			factRows: sqlmock.NewRows(accountSummaryColumns()).AddRow(
				int64(2), nil, "openai", int64(2), int64(2), int64(0), int64(10),
				0.2, 0.1, 100.0, int64(120), time.Date(2026, 7, 15, 0, 55, 0, 0, time.UTC), time.Time{},
				int64(1), int64(1), int64(0), int64(0), int64(0), int64(1), 1.0,
			),
			wantIDs: []int64{2}, wantGroups: []int64{10},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repoDB, repoMock := newSourceMock(t)
			sourceDB, sourceMock := newSourceMock(t)
			from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
			to := from.Add(time.Hour)
			expectAccountInventory(sourceMock)
			repoMock.ExpectQuery(`SELECT stats\.\*`).WillReturnRows(test.factRows)
			if test.name != "deleted membership counts as ungrouped" {
				expectAccountHealth(repoMock, to, test.wantIDs...)
			}

			service := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second)
			service.now = func() time.Time { return to }
			result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
				Resource: ResourceAccounts, From: from, To: to, Page: 1, PageSize: 20, Query: test.query,
			})
			if err != nil {
				t.Fatal(err)
			}
			page := result.(PageResponse)
			items := page.Items.([]AccountSummary)
			if page.Total != int64(len(test.wantIDs)) || len(items) != len(test.wantIDs) {
				t.Fatalf("page = %+v items=%+v", page, items)
			}
			for index, wantID := range test.wantIDs {
				if items[index].AccountID != wantID {
					t.Fatalf("account IDs = %+v, want %v", items, test.wantIDs)
				}
			}
			if len(test.wantGroups) > 0 {
				groups := accountSummaryGroups(t, items[0])
				for index, wantID := range test.wantGroups {
					if index >= len(groups) || groups[index].GroupID != wantID {
						t.Fatalf("groups = %+v, want %v", groups, test.wantGroups)
					}
				}
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

func TestAdminServiceRiskSortScoresAllCandidatesBeforePaging(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	now := from.Add(12 * time.Hour)
	mock.ExpectQuery(`SELECT stats\.\*`).
		WithArgs(from, now, "physical", 5001, 0, "", "", "", "", int64(0), int64(0), int64(0), int64(0), 0, 0, "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(accountSummaryColumns()).
			AddRow(int64(1), nil, "openai", int64(20), int64(10), int64(10), int64(0), 0.0, 0.0, 0.0, int64(0), now, now, int64(1), int64(1), int64(0), int64(0), int64(0), int64(3), 0.5).
			AddRow(int64(2), nil, "openai", int64(20), int64(20), int64(0), int64(0), 0.0, 0.0, 0.0, int64(0), now, now, int64(1), int64(1), int64(0), int64(0), int64(0), int64(3), 1.0).
			AddRow(int64(3), nil, "openai", int64(20), int64(20), int64(0), int64(0), 0.0, 0.0, 0.0, int64(0), now, now, int64(1), int64(1), int64(0), int64(0), int64(0), int64(3), 1.0))
	mock.ExpectQuery(regexp.QuoteMeta(selectThresholdOverridesSQL)).
		WillReturnRows(sqlmock.NewRows([]string{"scope_type", "scope_id", "config"}))
	mock.ExpectQuery(regexp.QuoteMeta(healthMetricsSQL)).
		WithArgs(now.Add(-time.Hour), now, "physical", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(healthMetricColumns()).
			AddRow(int64(1), int64(0), "openai", int64(0), int64(0), int64(0), nil, 0, 3, 0.0, int64(0), 0.0, int64(0), 0.0, int64(0), 0.0, "", int64(0)).
			AddRow(int64(2), int64(0), "openai", int64(20), int64(20), int64(0), now, 0, 0, 0.0, int64(20), 0.0, int64(31), 10.0, int64(0), 0.0, "", int64(0)))

	service := NewAdminService(NewRepository(db), nil, time.Second)
	service.now = func() time.Time { return now }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: now, Page: 1, PageSize: 1,
		SortBy: "risk_score", SortOrder: "desc", Query: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(PageResponse)
	items := page.Items.([]AccountSummary)
	if page.Total != 3 || len(items) != 1 || items[0].AccountID != 1 || items[0].Health.RiskScore != 70 {
		t.Fatalf("risk page = %+v items=%+v", page, items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminServiceRiskSortRejectsTooManyCandidates(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	now := from.Add(12 * time.Hour)
	mock.ExpectQuery(`SELECT stats\.\*`).
		WithArgs(from, now, "physical", 5001, 0, "", "", "", "", int64(0), int64(0), int64(0), int64(0), 0, 0, "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(accountSummaryColumns()).AddRow(
			int64(1), nil, "openai", int64(20), int64(20), int64(0), int64(0),
			0.0, 0.0, 0.0, int64(0), now, now, int64(1), int64(1), int64(0), int64(0), int64(0), int64(5001), 1.0,
		))

	service := NewAdminService(NewRepository(db), nil, time.Second)
	_, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: now, Page: 1, PageSize: 20,
		SortBy: "risk_score", SortOrder: "desc", Query: map[string]string{},
	})
	if !errors.Is(err, ErrAccountCandidateLimit) {
		t.Fatalf("error = %v, want %v", err, ErrAccountCandidateLimit)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminServiceAccountsAcceptsNullParentAccount(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	now := from.Add(time.Hour)
	mock.ExpectQuery(`SELECT stats\.\*`).
		WillReturnRows(sqlmock.NewRows(accountSummaryColumns()).AddRow(
			int64(10), nil, "openai", int64(4), int64(4), int64(0), int64(100),
			1.0, 0.5, 200.0, int64(300), now, time.Time{},
			int64(1), int64(1), int64(0), int64(0), int64(0), int64(1), 1.0,
		))
	mock.ExpectQuery(regexp.QuoteMeta(selectThresholdOverridesSQL)).
		WillReturnRows(sqlmock.NewRows([]string{"scope_type", "scope_id", "config"}))
	mock.ExpectQuery(regexp.QuoteMeta(healthMetricsSQL)).
		WillReturnRows(sqlmock.NewRows(healthMetricColumns()))

	service := NewAdminService(NewRepository(db), nil, time.Second)
	service.now = func() time.Time { return now }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: now, Page: 1, PageSize: 20, Query: map[string]string{},
	})
	if err != nil {
		t.Fatalf("accounts error = %v", err)
	}
	items := result.(PageResponse).Items.([]AccountSummary)
	if len(items) != 1 || items[0].ParentAccountID != 0 {
		t.Fatalf("accounts = %+v", items)
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

func TestParseOptionalRiskScoreRejectsInvalidOrOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"-1", "101", "not-a-number"} {
		if value, ok := parseOptionalRiskScore(raw); ok {
			t.Fatalf("parseOptionalRiskScore(%q) = (%d,true), want unavailable", raw, value)
		}
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
	sourceMock.ExpectQuery(regexp.QuoteMeta(allAccountDimensionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "parent_account_id", "name", "platform", "status", "schedulable", "deleted_at", "account_identity"}).
			AddRow(int64(9), nil, "active account", "openai", "active", true, nil, "").
			AddRow(int64(10), nil, "inactive account", "openai", "inactive", false, nil, ""),
	)
	sourceMock.ExpectQuery(regexp.QuoteMeta(accountGroupDimensionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"account_id", "group_id", "group_name", "group_platform", "group_status", "group_rate_multiplier", "group_deleted_at"}),
	)
	repoMock.ExpectQuery(`SELECT stats\.\*`).WillReturnRows(sqlmock.NewRows(accountSummaryColumns()).
		AddRow(int64(9), nil, "openai", int64(2), int64(2), int64(0), int64(10), 0.2, 0.1, 100.0, int64(120), to, time.Time{}, int64(1), int64(1), int64(0), int64(0), int64(0), int64(2), 1.0).
		AddRow(int64(10), nil, "openai", int64(3), int64(3), int64(0), int64(20), 0.3, 0.2, 110.0, int64(130), to, time.Time{}, int64(1), int64(1), int64(0), int64(0), int64(0), int64(2), 1.0))
	expectAccountHealth(repoMock, to, int64(9))

	service := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second)
	service.now = func() time.Time { return to }
	result, err := service.ExecuteAdmin(context.Background(), AdminRequest{
		Resource: ResourceAccounts, From: from, To: to, Page: 1, PageSize: 20,
		Query: map[string]string{"account_status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(PageResponse)
	items := page.Items.([]AccountSummary)
	if page.Total != 1 || len(items) != 1 || items[0].AccountID != 9 || items[0].Status != "active" {
		t.Fatalf("result = %+v", result)
	}
	if err := repoMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := sourceMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFilterAccountInventoryMatchesNameOrIdentity(t *testing.T) {
	inventory := accountInventory{
		Accounts: map[int64]AccountDimension{
			1: {ID: 1, Name: "Primary OpenAI", AccountIdentity: "owner@example.com"},
			2: {ID: 2, Name: "Backup Claude", AccountIdentity: "backup@example.com"},
		},
		Members: map[int64][]AccountDimension{
			1: {{ID: 1, Name: "Primary OpenAI", AccountIdentity: "owner@example.com"}},
			2: {{ID: 2, Name: "Backup Claude", AccountIdentity: "backup@example.com"}},
		},
		Groups: map[int64][]AccountGroupSummary{},
	}

	byName := filterAccountInventory(inventory, map[string]string{"query": "PRIMARY"})
	if len(byName.Accounts) != 1 || byName.Accounts[1].Name != "Primary OpenAI" {
		t.Fatalf("name-filtered inventory = %+v", byName.Accounts)
	}
	byIdentity := filterAccountInventory(inventory, map[string]string{"query": "OWNER@EXAMPLE"})
	if len(byIdentity.Accounts) != 1 || byIdentity.Accounts[1].AccountIdentity != "owner@example.com" {
		t.Fatalf("identity-filtered inventory = %+v", byIdentity.Accounts)
	}
}

func TestFilterAccountInventoryKeepsParentWhenChildIdentityMatches(t *testing.T) {
	parent := AccountDimension{ID: 10, Name: "Parent"}
	child := AccountDimension{ID: 11, ParentAccountID: 10, Name: "Child", AccountIdentity: "child@example.com"}
	inventory := accountInventory{
		Accounts: map[int64]AccountDimension{10: parent},
		Members:  map[int64][]AccountDimension{10: {parent, child}},
		Groups:   map[int64][]AccountGroupSummary{},
	}

	filtered := filterAccountInventory(inventory, map[string]string{"query": "CHILD@EXAMPLE"})
	if len(filtered.Accounts) != 1 || filtered.Accounts[10].Name != "Parent" {
		t.Fatalf("parent-rollup inventory = %+v", filtered.Accounts)
	}
}

func TestMergeAccountStatsReturnsAccountIdentity(t *testing.T) {
	inventory := accountInventory{
		Accounts: map[int64]AccountDimension{
			1: {ID: 1, Name: "Primary", AccountIdentity: "owner@example.com"},
		},
		Groups: map[int64][]AccountGroupSummary{},
	}

	items := mergeAccountStats(inventory, nil, false)
	if len(items) != 1 || items[0].AccountIdentity != "owner@example.com" {
		t.Fatalf("account summaries = %+v", items)
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

func TestUsersDetailResponseFields(t *testing.T) {
	repoDB, repoMock := newSourceMock(t)
	sourceDB, sourceMock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	lastAttemptedAt := to.Add(-time.Minute)
	repoMock.ExpectQuery(regexp.QuoteMeta(usersSQL)).WithArgs(
		from, to, int64(9), 20, 0, "", "", "", "", int64(0), int64(0), 0, 0,
	).WillReturnRows(sqlmock.NewRows([]string{
		"user_id", "api_key_id", "attempts", "successes", "failures", "tokens", "user_cost", "last_attempted_at", "total",
	}).AddRow(int64(7), int64(13), int64(4), int64(3), int64(1), int64(120), 1.25, lastAttemptedAt, int64(1)))
	sourceMock.ExpectQuery(regexp.QuoteMeta(userDimensionQuery)).WithArgs(sqlmock.AnyArg()).WillReturnRows(
		sqlmock.NewRows([]string{"id", "email", "username", "status", "deleted_at"}).
			AddRow(int64(7), "alice@example.test", "alice", "active", nil),
	)
	sourceMock.ExpectQuery(regexp.QuoteMeta(apiKeyDimensionQuery)).WithArgs(sqlmock.AnyArg()).WillReturnRows(
		sqlmock.NewRows([]string{"id", "user_id", "name", "masked_prefix", "status", "deleted_at"}).
			AddRow(int64(13), int64(7), "Production Key", "sk-old***", "active", nil),
	)

	result, err := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second).
		ExecuteAdmin(context.Background(), AdminRequest{
			Resource: ResourceUsers, AccountID: 9, From: from, To: to, Page: 1, PageSize: 20, Query: map[string]string{},
		})
	if err != nil {
		t.Fatal(err)
	}
	items := result.(PageResponse).Items.([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("users = %+v", items)
	}
	item := items[0]
	for _, key := range []string{"email", "api_key_name", "attempts", "successes", "failures", "success_rate", "tokens", "user_cost", "last_attempted_at"} {
		if _, ok := item[key]; !ok {
			t.Errorf("user detail missing %q: %+v", key, item)
		}
	}
	for _, key := range []string{"username", "masked_prefix"} {
		if _, ok := item[key]; ok {
			t.Errorf("user detail unexpectedly exposes %q: %+v", key, item)
		}
	}
	if item["email"] != "alice@example.test" || item["api_key_name"] != "Production Key" || item["success_rate"] != 0.75 || item["last_attempted_at"] != lastAttemptedAt {
		t.Fatalf("user detail = %+v", item)
	}
	if err := repoMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := sourceMock.ExpectationsWereMet(); err != nil {
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
			now := to.Add(5 * time.Minute)
			usageCursor := from.Add(45 * time.Minute)
			errorCursor := from.Add(40 * time.Minute)
			availableFrom := from.Add(-24 * time.Hour)
			availableTo := from.Add(50 * time.Minute)
			repoMock.ExpectQuery(regexp.QuoteMeta(dataQualitySQL)).WithArgs(from, to).
				WillReturnRows(sqlmock.NewRows([]string{"exact", "estimated", "fallback", "recovered"}).AddRow(int64(8), int64(2), int64(1), int64(3)))
			repoMock.ExpectQuery(regexp.QuoteMeta(requestQualitySQL)).WithArgs(from, to).
				WillReturnRows(sqlmock.NewRows([]string{"unattributed", "failed"}).AddRow(int64(1), int64(4)))
			repoMock.ExpectQuery(regexp.QuoteMeta(syncQualitySQL)).WillReturnRows(
				sqlmock.NewRows([]string{"source", "cursor_time", "cursor_id", "last_success_at", "last_error", "updated_at"}).
					AddRow("errors", errorCursor, int64(22), errorCursor, "error source timeout", now).
					AddRow("usage", usageCursor, int64(11), usageCursor, "", now.Add(-time.Minute)),
			)
			repoMock.ExpectQuery(regexp.QuoteMeta(sharedQualityFactsSQL)).WithArgs(from, to).WillReturnRows(
				sqlmock.NewRows([]string{"available_from", "available_to", "missing_group", "exact", "estimated"}).
					AddRow(availableFrom, availableTo, int64(2), int64(7), int64(3)),
			)

			service := NewAdminService(NewRepository(repoDB), NewPostgresSource(sourceDB, time.Second, 100), time.Second)
			service.now = func() time.Time { return now }
			result, err := service.ExecuteAdmin(context.Background(), AdminRequest{Resource: ResourceDataQuality, From: from, To: to})
			if err != nil {
				t.Fatal(err)
			}
			quality := result.(DataQualityResponse)
			if quality.SourceConnected != test.connected || quality.ExactModels != 8 || quality.UnattributedErrors != 1 {
				t.Fatalf("quality = %+v", quality)
			}
			if quality.DataAsOf == nil || !quality.DataAsOf.Equal(errorCursor) || quality.CollectionLagSeconds == nil || *quality.CollectionLagSeconds != 25*60 {
				t.Fatalf("quality collection snapshot = %+v", quality.DataQualitySnapshot)
			}
			if quality.UsageCursor.CursorID != 11 || quality.ErrorCursor.CursorID != 22 || quality.RecentSourceError != "error source timeout" {
				t.Fatalf("quality cursors = %+v", quality.DataQualitySnapshot)
			}
			if quality.AvailableFrom == nil || !quality.AvailableFrom.Equal(availableFrom) || quality.AvailableTo == nil || !quality.AvailableTo.Equal(availableTo) {
				t.Fatalf("quality history = %+v", quality.DataQualitySnapshot)
			}
			if quality.MissingGroupRequests != 2 || quality.ExactModelRequests != 7 || quality.EstimatedModelRequests != 3 || quality.StaleDataWarning == "" {
				t.Fatalf("quality attribution/stale state = %+v", quality.DataQualitySnapshot)
			}
			raw, err := json.Marshal(quality)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"missing_group_requests":2`) {
				t.Fatalf("quality JSON = %s", raw)
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

func expectAccountInventory(mock sqlmock.Sqlmock) {
	deletedAt := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(allAccountDimensionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "parent_account_id", "name", "platform", "status", "schedulable", "deleted_at", "account_identity"}).
			AddRow(int64(1), nil, "idle", "grok", "active", true, nil, "").
			AddRow(int64(2), nil, "busy", "openai", "active", true, nil, "").
			AddRow(int64(3), nil, "multi", "anthropic", "active", true, nil, ""),
	)
	mock.ExpectQuery(regexp.QuoteMeta(accountGroupDimensionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"account_id", "group_id", "group_name", "group_platform", "group_status", "group_rate_multiplier", "group_deleted_at"}).
			AddRow(int64(1), int64(99), "Retired", "grok", "inactive", 1.0, deletedAt).
			AddRow(int64(2), int64(10), "GPT", "openai", "active", 1.0, nil).
			AddRow(int64(3), int64(11), "Claude", "anthropic", "active", 1.5, nil).
			AddRow(int64(3), int64(12), "Shared", "anthropic", "active", 2.0, nil),
	)
}

func expectAccountHealth(mock sqlmock.Sqlmock, now time.Time, accountIDs ...int64) {
	mock.ExpectQuery(regexp.QuoteMeta(selectThresholdOverridesSQL)).WillReturnRows(
		sqlmock.NewRows([]string{"scope_type", "scope_id", "config"}),
	)
	rows := sqlmock.NewRows(healthMetricColumns())
	for _, accountID := range accountIDs {
		rows.AddRow(
			accountID, int64(0), "openai", int64(1), int64(1), int64(0), now.Add(-time.Minute),
			0, 0, 0.0, int64(1), 0.0, int64(1), 1.0, int64(100), 100.0, "", int64(0),
		)
	}
	mock.ExpectQuery(regexp.QuoteMeta(healthMetricsSQL)).WithArgs(now.Add(-time.Hour), now, "physical", sqlmock.AnyArg()).WillReturnRows(rows)
}

func accountSummaryByID(t *testing.T, items []AccountSummary, accountID int64) AccountSummary {
	t.Helper()
	for _, item := range items {
		if item.AccountID == accountID {
			return item
		}
	}
	t.Fatalf("account %d not found in %+v", accountID, items)
	return AccountSummary{}
}

type accountGroupContract struct {
	GroupID        int64   `json:"group_id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	Status         string  `json:"status"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

func accountSummaryGroups(t *testing.T, item AccountSummary) []accountGroupContract {
	t.Helper()
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Groups []accountGroupContract `json:"groups"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Groups
}

func groupResponseBucketSeconds(t *testing.T, response any) int64 {
	t.Helper()
	field := reflect.ValueOf(response).FieldByName("BucketSeconds")
	if !field.IsValid() {
		t.Fatalf("%T is missing BucketSeconds", response)
	}
	return field.Int()
}
