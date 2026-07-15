package accountmonitor

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryCleanupAppliesDetailAndDailyRetention(t *testing.T) {
	db, mock := newSourceMock(t)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	for _, query := range cleanupDetailSQL {
		mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(now.Add(-90 * 24 * time.Hour)).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	for _, query := range cleanupDailySQL {
		mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(now.Add(-365 * 24 * time.Hour)).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	if err := NewRepository(db).Cleanup(context.Background(), now, 90*24*time.Hour, 365*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
