package accountmonitor

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresSourceClampsUsagePageAndUsesCursor(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	after := Cursor{Time: from.Add(time.Minute), ID: 42}
	mock.ExpectQuery(regexp.QuoteMeta(usageSourceQuery)).
		WithArgs(from, after.Time, after.ID, 100).
		WillReturnRows(sqlmock.NewRows(usageSourceColumns()))

	source := NewPostgresSource(db, 2*time.Second, 100)
	rows, err := source.ReadUsage(context.Background(), after, from, 500)
	if err != nil {
		t.Fatalf("ReadUsage() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ReadUsage() rows = %d, want 0", len(rows))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSourceReadsErrorsAndDimensionsFromSafeViews(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(errorSourceQuery)).
		WithArgs(from, time.Time{}, int64(0), 25).
		WillReturnRows(sqlmock.NewRows(errorSourceColumns()))
	mock.ExpectQuery(regexp.QuoteMeta(accountDimensionQuery)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_account_id", "name", "platform", "status", "schedulable", "deleted_at"}))
	mock.ExpectQuery(regexp.QuoteMeta(userDimensionQuery)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "username", "status", "deleted_at"}))
	mock.ExpectQuery(regexp.QuoteMeta(apiKeyDimensionQuery)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "masked_prefix", "status", "deleted_at"}))
	mock.ExpectQuery(regexp.QuoteMeta(groupDimensionQuery)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "status", "deleted_at"}))

	source := NewPostgresSource(db, time.Second, 100)
	if _, err := source.ReadErrors(context.Background(), Cursor{}, from, 25); err != nil {
		t.Fatalf("ReadErrors() error = %v", err)
	}
	if _, err := source.ReadDimensions(context.Background(), DimensionIDs{AccountIDs: []int64{1}, UserIDs: []int64{2}, APIKeyIDs: []int64{3}, GroupIDs: []int64{4}}); err != nil {
		t.Fatalf("ReadDimensions() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSourceRangeUsesExclusiveUpperBound(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(usageRangeSourceQuery)).
		WithArgs(from, to, time.Time{}, int64(0), 50).
		WillReturnRows(sqlmock.NewRows(usageSourceColumns()))

	if _, err := NewPostgresSource(db, time.Second, 100).ReadUsageRange(context.Background(), Cursor{}, from, to, 50); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSourceReadsAllGroupDimensions(t *testing.T) {
	db, mock := newSourceMock(t)
	deletedAt := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(allGroupDimensionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "platform", "status", "deleted_at"}).
			AddRow(int64(7), "Primary", "openai", "active", nil).
			AddRow(int64(8), "Retired", "anthropic", "inactive", deletedAt),
	)

	groups, err := NewPostgresSource(db, time.Second, 100).ReadGroupDimensions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].ID != 7 || groups[1].DeletedAt == nil {
		t.Fatalf("groups = %+v", groups)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSourceReadsUsageWithNullOptionalPayloadFields(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(usageSourceQuery)).
		WithArgs(from, time.Time{}, int64(0), 10).
		WillReturnRows(sqlmock.NewRows(usageSourceColumns()).AddRow(
			int64(1), from, int64(2), int64(3), int64(4), int64(5), nil,
			"request-1", "openai", "gpt-5", nil, nil,
			int64(10), int64(20), int64(0), int64(0),
			0.2, 0.1, nil, nil, 1, false, 0, nil, nil, nil, nil, 0, nil, nil,
		))

	rows, err := NewPostgresSource(db, time.Second, 100).ReadUsage(context.Background(), Cursor{}, from, 10)
	if err != nil {
		t.Fatalf("ReadUsage() error = %v", err)
	}
	if len(rows) != 1 || rows[0].ImageSizeBreakdown != nil {
		t.Fatalf("ReadUsage() rows = %+v", rows)
	}
}

func TestPostgresSourceReadsLegacyErrorsWithNullModelAndRequestType(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(errorSourceQuery)).
		WithArgs(from, time.Time{}, int64(0), 10).
		WillReturnRows(sqlmock.NewRows(errorSourceColumns()).AddRow(
			int64(1), from, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, false, "upstream", "provider", nil, nil, nil, nil, nil, nil, nil, nil,
			"provider error", nil, []byte("[]"),
		))

	rows, err := NewPostgresSource(db, time.Second, 100).ReadErrors(context.Background(), Cursor{}, from, 10)
	if err != nil {
		t.Fatalf("ReadErrors() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Model != "" || rows[0].RequestType != 0 {
		t.Fatalf("ReadErrors() rows = %+v", rows)
	}
}

func newSourceMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func usageSourceColumns() []string {
	return []string{
		"id", "created_at", "user_id", "api_key_id", "account_id", "group_id", "parent_account_id",
		"request_id", "platform", "model", "requested_model", "upstream_model",
		"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_cost", "actual_cost", "account_rate_multiplier", "duration_ms",
		"request_type", "stream", "image_count", "image_size", "image_input_size",
		"image_output_size", "image_size_breakdown", "video_count", "video_resolution",
		"video_duration_seconds",
	}
}

func errorSourceColumns() []string {
	return []string{
		"id", "created_at", "request_id", "client_request_id", "user_id", "api_key_id",
		"account_id", "group_id", "platform", "model", "requested_model", "upstream_model",
		"request_type", "stream", "error_phase", "error_type", "error_source",
		"error_owner", "status_code", "upstream_status_code", "provider_error_code",
		"provider_error_type", "network_error_type", "duration_ms", "error_message",
		"upstream_error_message", "upstream_errors",
	}
}
