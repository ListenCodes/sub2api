package accountmonitor

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryCreateRebuildJobRejectsOverlap(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(rebuildLockSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(rebuildOverlapSQL)).WithArgs(from, to).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	_, err := NewRepository(db).CreateRebuildJob(context.Background(), from, to, 9)
	if !errors.Is(err, ErrRebuildOverlap) {
		t.Fatalf("CreateRebuildJob() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCreateRebuildJobReturnsPendingJob(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(rebuildLockSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(rebuildOverlapSQL)).WithArgs(from, to).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta(insertRebuildJobSQL)).WithArgs(from, to, int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(4), from))
	mock.ExpectCommit()

	job, err := NewRepository(db).CreateRebuildJob(context.Background(), from, to, 9)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != 4 || job.Status != RebuildPending || job.RequestedBy != 9 {
		t.Fatalf("job = %+v", job)
	}
}

type fakeRangeSource struct {
	fakeCollectorSource
	usageRange []UsageSourceRow
	errorRange []ErrorSourceRow
}

func (f *fakeRangeSource) ReadUsageRange(context.Context, Cursor, time.Time, time.Time, int) ([]UsageSourceRow, error) {
	rows := f.usageRange
	f.usageRange = nil
	return rows, nil
}

func (f *fakeRangeSource) ReadErrorsRange(context.Context, Cursor, time.Time, time.Time, int) ([]ErrorSourceRow, error) {
	rows := f.errorRange
	f.errorRange = nil
	return rows, nil
}

type fakeRebuildStore struct {
	fakeCollectorStore
	rebuildBatch Batch
	claimedJob   RebuildJob
	finishedID   int64
	finishedRows int64
	finishedErr  error
}

func (f *fakeRebuildStore) CommitRebuildBatch(_ context.Context, batch Batch) error {
	f.rebuildBatch = batch
	return nil
}

func (f *fakeRebuildStore) ClaimNextRebuildJob(context.Context) (RebuildJob, bool, error) {
	if f.claimedJob.ID == 0 {
		return RebuildJob{}, false, nil
	}
	job := f.claimedJob
	f.claimedJob = RebuildJob{}
	return job, true, nil
}

func (f *fakeRebuildStore) FinishRebuildJob(_ context.Context, id, processedRows int64, err error) error {
	f.finishedID, f.finishedRows, f.finishedErr = id, processedRows, err
	return nil
}

func TestCollectorProcessRebuildDoesNotAdvanceLiveCursors(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	source := &fakeRangeSource{usageRange: []UsageSourceRow{{ID: 3, CreatedAt: from.Add(time.Hour), AccountID: 1, ActualCost: 1}}}
	store := &fakeRebuildStore{}
	collector := NewCollector(source, store, Config{BatchSize: 100}, time.Now)

	processed, err := collector.ProcessRebuild(context.Background(), RebuildJob{ID: 4, From: from, To: from.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || len(store.rebuildBatch.Attempts) != 1 {
		t.Fatalf("processed=%d batch=%+v", processed, store.rebuildBatch)
	}
	if !store.rebuildBatch.UsageCursor.Time.IsZero() || !store.rebuildBatch.ErrorCursor.Time.IsZero() {
		t.Fatalf("rebuild advanced live cursors: %+v", store.rebuildBatch)
	}
}

func TestCollectorProcessNextRebuildFinishesClaimedJob(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	source := &fakeRangeSource{usageRange: []UsageSourceRow{{ID: 3, CreatedAt: from.Add(time.Hour), AccountID: 1, ActualCost: 1}}}
	store := &fakeRebuildStore{claimedJob: RebuildJob{ID: 4, From: from, To: from.Add(24 * time.Hour), Status: RebuildRunning}}
	collector := NewCollector(source, store, Config{BatchSize: 100}, time.Now)

	found, err := collector.ProcessNextRebuild(context.Background())
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if store.finishedID != 4 || store.finishedRows != 1 || store.finishedErr != nil {
		t.Fatalf("finish id=%d rows=%d err=%v", store.finishedID, store.finishedRows, store.finishedErr)
	}
}

func TestRepositoryClaimAndFinishRebuildJob(t *testing.T) {
	db, mock := newSourceMock(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(claimRebuildJobSQL)).WillReturnRows(sqlmock.NewRows(rebuildJobColumns()).AddRow(
		int64(4), from, to, "running", int64(0), "", int64(9), from, from, nil,
	))
	mock.ExpectExec(regexp.QuoteMeta(finishRebuildJobSQL)).WithArgs(int64(4), "completed", int64(18), "").WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewRepository(db)
	job, found, err := repo.ClaimNextRebuildJob(context.Background())
	if err != nil || !found || job.Status != RebuildRunning {
		t.Fatalf("claim job=%+v found=%v err=%v", job, found, err)
	}
	if err := repo.FinishRebuildJob(context.Background(), job.ID, 18, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func rebuildJobColumns() []string {
	return []string{"id", "from_time", "to_time", "status", "processed_rows", "error", "requested_by", "created_at", "started_at", "completed_at"}
}
