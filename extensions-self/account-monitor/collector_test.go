package accountmonitor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCollectorSource struct {
	usageAfter Cursor
	usageFrom  time.Time
	usageCalls []Cursor
	errorAfter Cursor
	errorFrom  time.Time
	errorCalls []Cursor
	usage      []UsageSourceRow
	errors     []ErrorSourceRow
}

func (f *fakeCollectorSource) ReadUsage(_ context.Context, after Cursor, from time.Time, _ int) ([]UsageSourceRow, error) {
	f.usageAfter, f.usageFrom = after, from
	f.usageCalls = append(f.usageCalls, after)
	rows := f.usage
	f.usage = nil
	return rows, nil
}

func (f *fakeCollectorSource) ReadErrors(_ context.Context, after Cursor, from time.Time, _ int) ([]ErrorSourceRow, error) {
	f.errorAfter, f.errorFrom = after, from
	f.errorCalls = append(f.errorCalls, after)
	rows := f.errors
	f.errors = nil
	return rows, nil
}

type fakeCollectorStore struct {
	usageCursor Cursor
	errorCursor Cursor
	batch       Batch
	commitErr   error
	cleanedAt   time.Time
}

func (f *fakeCollectorStore) LoadCursors(context.Context) (Cursor, Cursor, error) {
	return f.usageCursor, f.errorCursor, nil
}

func (f *fakeCollectorStore) CommitBatch(_ context.Context, batch Batch) error {
	f.batch = batch
	return f.commitErr
}

func (f *fakeCollectorStore) Cleanup(_ context.Context, now time.Time, _, _ time.Duration) error {
	f.cleanedAt = now
	return nil
}

func TestCollectorSyncOnceUsesSeparateLookbackCursors(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	usageCursor := Cursor{Time: now.Add(-time.Hour), ID: 10}
	errorCursor := Cursor{Time: now.Add(-2 * time.Hour), ID: 20}
	source := &fakeCollectorSource{
		usage:  []UsageSourceRow{{ID: 11, CreatedAt: now, ActualCost: 1, AccountID: 1}},
		errors: []ErrorSourceRow{{ID: 21, CreatedAt: now, StatusCode: 403, ErrorPhase: "security"}},
	}
	store := &fakeCollectorStore{usageCursor: usageCursor, errorCursor: errorCursor}
	collector := NewCollector(source, store, Config{Lookback: 5 * time.Minute, BatchSize: 100, DetailRetention: 90 * 24 * time.Hour, DailyRetention: 365 * 24 * time.Hour}, func() time.Time { return now })

	if err := collector.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	usageScanCursor := Cursor{Time: usageCursor.Time.Add(-5 * time.Minute)}
	errorScanCursor := Cursor{Time: errorCursor.Time.Add(-5 * time.Minute)}
	if source.usageAfter != usageScanCursor || !source.usageFrom.Equal(usageScanCursor.Time) {
		t.Fatalf("usage cursor=%+v from=%s", source.usageAfter, source.usageFrom)
	}
	if source.errorAfter != errorScanCursor || !source.errorFrom.Equal(errorScanCursor.Time) {
		t.Fatalf("error cursor=%+v from=%s", source.errorAfter, source.errorFrom)
	}
	if store.batch.UsageCursor.ID != 11 || store.batch.ErrorCursor.ID != 21 {
		t.Fatalf("committed cursors = %+v %+v", store.batch.UsageCursor, store.batch.ErrorCursor)
	}
	if !store.cleanedAt.Equal(now) {
		t.Fatalf("cleanup time = %s", store.cleanedAt)
	}
}

func TestCollectorSyncOnceCollectsLateRowsWithoutRegressingCursor(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	persisted := Cursor{Time: now.Add(-time.Minute), ID: 100}
	lateAt := persisted.Time.Add(-2 * time.Minute)
	source := &fakeCollectorSource{
		usage: []UsageSourceRow{{ID: 90, CreatedAt: lateAt, ActualCost: 1, AccountID: 1}},
	}
	store := &fakeCollectorStore{usageCursor: persisted, errorCursor: persisted}
	collector := NewCollector(source, store, Config{Lookback: 5 * time.Minute, BatchSize: 100}, func() time.Time { return now })

	if err := collector.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(source.usageCalls) != 1 || source.usageCalls[0] != (Cursor{Time: persisted.Time.Add(-5 * time.Minute)}) {
		t.Fatalf("usage scan cursors = %+v", source.usageCalls)
	}
	if len(store.batch.Attempts) != 1 {
		t.Fatalf("committed attempts = %d, want late row", len(store.batch.Attempts))
	}
	if store.batch.UsageCursor != persisted {
		t.Fatalf("committed cursor = %+v, want persisted %+v", store.batch.UsageCursor, persisted)
	}
}

func TestCollectorReturnsCommitFailure(t *testing.T) {
	source := &fakeCollectorSource{usage: []UsageSourceRow{{ID: 1, ActualCost: 1}}}
	store := &fakeCollectorStore{commitErr: errors.New("commit failed")}
	collector := NewCollector(source, store, Config{BatchSize: 100}, time.Now)

	if err := collector.SyncOnce(context.Background()); err == nil {
		t.Fatal("SyncOnce() error = nil")
	}
}

func TestCollectorBackoffIsCappedAndResets(t *testing.T) {
	collector := NewCollector(nil, nil, Config{PollInterval: time.Minute}, time.Now)
	var delay time.Duration
	for range 10 {
		delay = collector.nextDelay(errors.New("failed"))
	}
	if delay != 15*time.Minute {
		t.Fatalf("capped delay = %s, want 15m", delay)
	}
	if delay = collector.nextDelay(nil); delay != time.Minute {
		t.Fatalf("reset delay = %s, want 1m", delay)
	}
}

func TestValidateRebuildRange(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateRebuildRange(from, from.Add(31*24*time.Hour)); err != nil {
		t.Fatalf("31 day range error = %v", err)
	}
	if err := ValidateRebuildRange(from, from.Add(31*24*time.Hour+time.Second)); !errors.Is(err, ErrRebuildRangeTooLarge) {
		t.Fatalf("large range error = %v", err)
	}
	if err := ValidateRebuildRange(from, from); !errors.Is(err, ErrInvalidRebuildRange) {
		t.Fatalf("empty range error = %v", err)
	}
}
