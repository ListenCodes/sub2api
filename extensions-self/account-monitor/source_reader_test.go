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

	source := NewPostgresSource(db, time.Second, 100)
	if _, err := source.ReadErrors(context.Background(), Cursor{}, from, 25); err != nil {
		t.Fatalf("ReadErrors() error = %v", err)
	}
	if _, err := source.ReadDimensions(context.Background(), DimensionIDs{AccountIDs: []int64{1}, UserIDs: []int64{2}, APIKeyIDs: []int64{3}}); err != nil {
		t.Fatalf("ReadDimensions() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
		"id", "created_at", "user_id", "api_key_id", "account_id", "parent_account_id",
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
		"account_id", "platform", "model", "requested_model", "upstream_model",
		"request_type", "stream", "error_phase", "error_type", "error_source",
		"error_owner", "status_code", "upstream_status_code", "provider_error_code",
		"provider_error_type", "network_error_type", "duration_ms", "error_message",
		"upstream_error_message", "upstream_errors",
	}
}
