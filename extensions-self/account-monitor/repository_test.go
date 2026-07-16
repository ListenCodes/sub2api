package accountmonitor

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryCommitBatchAdvancesCursorsAfterFacts(t *testing.T) {
	db, mock := newSourceMock(t)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertAttemptSQL)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertRequestSQL)).WillReturnResult(sqlmock.NewResult(1, 1))
	for _, query := range refreshAggregateSQL {
		mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	for _, query := range refreshGroupAggregateSQL {
		mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(regexp.QuoteMeta(upsertSyncStateSQL)).WithArgs("usage", now, int64(10), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(upsertSyncStateSQL)).WithArgs("errors", now, int64(11), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := NewRepository(db)
	err := repo.CommitBatch(context.Background(), Batch{
		Attempts:    []AttemptFact{{EventKey: "usage:10", RequestKey: "request:1:r", AttemptedAt: now, AccountID: 7, Result: ResultSucceeded}},
		Requests:    []RequestFact{{RequestKey: "request:1:r", OccurredAt: now, Result: ResultSucceeded}},
		UsageCursor: Cursor{Time: now, ID: 10},
		ErrorCursor: Cursor{Time: now, ID: 11},
	})
	if err != nil {
		t.Fatalf("CommitBatch() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCommitBatchRollsBackWithoutCursorOnFactFailure(t *testing.T) {
	db, mock := newSourceMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertAttemptSQL)).WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()

	repo := NewRepository(db)
	err := repo.CommitBatch(context.Background(), Batch{Attempts: []AttemptFact{{EventKey: "usage:10"}}})
	if err == nil {
		t.Fatal("CommitBatch() error = nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
