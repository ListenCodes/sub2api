package accountmonitor

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRebuildRange  = errors.New("rebuild range must have from before to")
	ErrRebuildRangeTooLarge = errors.New("rebuild range cannot exceed 31 days")
)

type CollectorSource interface {
	ReadUsage(ctx context.Context, after Cursor, from time.Time, limit int) ([]UsageSourceRow, error)
	ReadErrors(ctx context.Context, after Cursor, from time.Time, limit int) ([]ErrorSourceRow, error)
}

type CollectorStore interface {
	LoadCursors(ctx context.Context) (Cursor, Cursor, error)
	CommitBatch(ctx context.Context, batch Batch) error
	Cleanup(ctx context.Context, now time.Time, detailRetention, dailyRetention time.Duration) error
}

type syncErrorRecorder interface {
	RecordSyncError(ctx context.Context, source string, syncErr error) error
}

type syncErrorClearer interface {
	ClearSyncError(ctx context.Context, source string) error
}

type syncSuccessRecorder interface {
	RecordSyncSuccess(ctx context.Context, source string, at time.Time) error
}

type GroupDimensionSource interface {
	ReadGroupDimensions(ctx context.Context) ([]GroupDimension, error)
}

type RangeCollectorSource interface {
	ReadUsageRange(ctx context.Context, after Cursor, from, to time.Time, limit int) ([]UsageSourceRow, error)
	ReadErrorsRange(ctx context.Context, after Cursor, from, to time.Time, limit int) ([]ErrorSourceRow, error)
}

type RebuildBatchStore interface {
	CommitRebuildBatch(ctx context.Context, batch Batch) error
}

type RebuildQueueStore interface {
	ClaimNextRebuildJob(ctx context.Context) (RebuildJob, bool, error)
	FinishRebuildJob(ctx context.Context, id, processedRows int64, rebuildErr error) error
}

type Collector struct {
	source       CollectorSource
	store        CollectorStore
	cfg          Config
	now          func() time.Time
	failureCount int
}

func NewCollector(source CollectorSource, store CollectorStore, cfg Config, now func() time.Time) *Collector {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Minute
	}
	if cfg.Lookback <= 0 {
		cfg.Lookback = 5 * time.Minute
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.DetailRetention <= 0 {
		cfg.DetailRetention = 90 * 24 * time.Hour
	}
	if cfg.DailyRetention <= 0 {
		cfg.DailyRetention = 365 * 24 * time.Hour
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Collector{source: source, store: store, cfg: cfg, now: now}
}

func (c *Collector) Run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			err := c.SyncOnce(ctx)
			if err == nil {
				_, err = c.ProcessNextRebuild(ctx)
			}
			timer.Reset(c.nextDelay(err))
		}
	}
}

func (c *Collector) SyncOnce(ctx context.Context) error {
	if c == nil || c.source == nil || c.store == nil {
		return errors.New("account monitor collector is not configured")
	}
	usageCursor, errorCursor, err := c.store.LoadCursors(ctx)
	if err != nil {
		return err
	}
	usageFrom := lookbackStart(usageCursor, c.now(), c.cfg.Lookback)
	errorFrom := lookbackStart(errorCursor, c.now(), c.cfg.Lookback)
	usageRows, latestUsage, err := c.readAllUsage(ctx, usageCursor, usageFrom)
	if err != nil {
		c.recordSyncError(ctx, "usage", err)
		return err
	}
	errorRows, latestError, err := c.readAllErrors(ctx, errorCursor, errorFrom)
	if err != nil {
		c.recordSyncError(ctx, "errors", err)
		return err
	}
	batch, err := Normalize(usageRows, errorRows)
	if err != nil {
		return err
	}
	batch.UsageCursor = laterCursor(usageCursor, laterCursor(batch.UsageCursor, latestUsage))
	batch.ErrorCursor = laterCursor(errorCursor, laterCursor(batch.ErrorCursor, latestError))
	if groupSource, ok := c.source.(GroupDimensionSource); ok {
		groupDimensions, err := groupSource.ReadGroupDimensions(ctx)
		if err != nil {
			c.recordSyncError(ctx, "groups", err)
			return err
		}
		for i := range groupDimensions {
			groupDimensions[i].SyncedAt = c.now()
		}
		batch.GroupDimensions = groupDimensions
	}
	if len(usageRows) > 0 || len(errorRows) > 0 || len(batch.GroupDimensions) > 0 {
		if err := c.store.CommitBatch(ctx, batch); err != nil {
			c.recordSyncError(ctx, "collector", err)
			return err
		}
	}
	if err := c.store.Cleanup(ctx, c.now(), c.cfg.DetailRetention, c.cfg.DailyRetention); err != nil {
		c.recordSyncError(ctx, "collector", err)
		return err
	}
	if err := c.recordSyncSuccesses(ctx); err != nil {
		c.recordSyncError(ctx, "collector", err)
		return err
	}
	c.clearCollectorError(ctx)
	return nil
}

func (c *Collector) recordSyncError(ctx context.Context, source string, syncErr error) {
	if recorder, ok := c.store.(syncErrorRecorder); ok {
		_ = recorder.RecordSyncError(ctx, source, syncErr)
	}
}

func (c *Collector) recordSyncSuccesses(ctx context.Context) error {
	recorder, ok := c.store.(syncSuccessRecorder)
	if !ok {
		return nil
	}
	sources := []string{"usage", "errors"}
	if _, ok := c.source.(GroupDimensionSource); ok {
		sources = append(sources, "groups")
	}
	at := c.now()
	for _, source := range sources {
		if err := recorder.RecordSyncSuccess(ctx, source, at); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) clearCollectorError(ctx context.Context) {
	clearer, ok := c.store.(syncErrorClearer)
	if !ok {
		return
	}
	_ = clearer.ClearSyncError(ctx, "collector")
}

func (c *Collector) readAllUsage(ctx context.Context, cursor Cursor, from time.Time) ([]UsageSourceRow, Cursor, error) {
	all := make([]UsageSourceRow, 0)
	scan := Cursor{Time: from}
	latest := cursor
	for {
		rows, err := c.source.ReadUsage(ctx, scan, from, c.cfg.BatchSize)
		if err != nil {
			return nil, latest, err
		}
		all = append(all, rows...)
		for _, row := range rows {
			rowCursor := Cursor{Time: row.CreatedAt, ID: row.ID}
			scan = laterCursor(scan, rowCursor)
			latest = laterCursor(latest, rowCursor)
		}
		if len(rows) < c.cfg.BatchSize {
			return all, latest, nil
		}
	}
}

func (c *Collector) readAllErrors(ctx context.Context, cursor Cursor, from time.Time) ([]ErrorSourceRow, Cursor, error) {
	all := make([]ErrorSourceRow, 0)
	scan := Cursor{Time: from}
	latest := cursor
	for {
		rows, err := c.source.ReadErrors(ctx, scan, from, c.cfg.BatchSize)
		if err != nil {
			return nil, latest, err
		}
		all = append(all, rows...)
		for _, row := range rows {
			rowCursor := Cursor{Time: row.CreatedAt, ID: row.ID}
			scan = laterCursor(scan, rowCursor)
			latest = laterCursor(latest, rowCursor)
		}
		if len(rows) < c.cfg.BatchSize {
			return all, latest, nil
		}
	}
}

func (c *Collector) nextDelay(err error) time.Duration {
	if err == nil {
		c.failureCount = 0
		return c.cfg.PollInterval
	}
	c.failureCount++
	shift := c.failureCount
	if shift > 8 {
		shift = 8
	}
	delay := c.cfg.PollInterval * time.Duration(1<<shift)
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func ValidateRebuildRange(from, to time.Time) error {
	if !from.Before(to) {
		return ErrInvalidRebuildRange
	}
	if to.Sub(from) > 31*24*time.Hour {
		return ErrRebuildRangeTooLarge
	}
	return nil
}

func (c *Collector) ProcessRebuild(ctx context.Context, job RebuildJob) (int64, error) {
	if err := ValidateRebuildRange(job.From, job.To); err != nil {
		return 0, err
	}
	source, ok := c.source.(RangeCollectorSource)
	if !ok {
		return 0, errors.New("account monitor source does not support rebuild ranges")
	}
	store, ok := c.store.(RebuildBatchStore)
	if !ok {
		return 0, errors.New("account monitor store does not support rebuild batches")
	}
	usageRows := make([]UsageSourceRow, 0)
	errorRows := make([]ErrorSourceRow, 0)
	usageCursor := Cursor{}
	errorCursor := Cursor{}
	for {
		rows, err := source.ReadUsageRange(ctx, usageCursor, job.From, job.To, c.cfg.BatchSize)
		if err != nil {
			return int64(len(usageRows) + len(errorRows)), err
		}
		usageRows = append(usageRows, rows...)
		for _, row := range rows {
			usageCursor = laterCursor(usageCursor, Cursor{Time: row.CreatedAt, ID: row.ID})
		}
		if len(rows) < c.cfg.BatchSize {
			break
		}
	}
	for {
		rows, err := source.ReadErrorsRange(ctx, errorCursor, job.From, job.To, c.cfg.BatchSize)
		if err != nil {
			return int64(len(usageRows) + len(errorRows)), err
		}
		errorRows = append(errorRows, rows...)
		for _, row := range rows {
			errorCursor = laterCursor(errorCursor, Cursor{Time: row.CreatedAt, ID: row.ID})
		}
		if len(rows) < c.cfg.BatchSize {
			break
		}
	}
	batch, err := Normalize(usageRows, errorRows)
	if err != nil {
		return int64(len(usageRows) + len(errorRows)), err
	}
	batch.UsageCursor = Cursor{}
	batch.ErrorCursor = Cursor{}
	if err := store.CommitRebuildBatch(ctx, batch); err != nil {
		return int64(len(usageRows) + len(errorRows)), err
	}
	return int64(len(usageRows) + len(errorRows)), nil
}

func (c *Collector) ProcessNextRebuild(ctx context.Context) (bool, error) {
	queue, ok := c.store.(RebuildQueueStore)
	if !ok {
		return false, nil
	}
	job, found, err := queue.ClaimNextRebuildJob(ctx)
	if err != nil || !found {
		return found, err
	}
	processed, rebuildErr := c.ProcessRebuild(ctx, job)
	if finishErr := queue.FinishRebuildJob(ctx, job.ID, processed, rebuildErr); finishErr != nil {
		return true, finishErr
	}
	return true, rebuildErr
}

func lookbackStart(cursor Cursor, now time.Time, lookback time.Duration) time.Time {
	if cursor.Time.IsZero() {
		return now.Add(-lookback)
	}
	return cursor.Time.Add(-lookback)
}
