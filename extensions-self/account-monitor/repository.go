package accountmonitor

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"strings"
	"time"
)

var ErrRebuildOverlap = errors.New("an active rebuild job overlaps the requested range")

//go:embed schema.sql
var schemaSQL string

const insertAttemptSQL = `
INSERT INTO account_monitor_attempt_facts (
    event_key, request_key, attempted_at, account_id, parent_account_id,
    platform, actual_model, model_attribution, user_id, api_key_id,
    request_type, result, recovered, error_category, status_code,
    upstream_status_code, provider_error_code, input_tokens, output_tokens,
    cache_creation_tokens, cache_read_tokens, user_cost, account_cost,
    duration_ms, image_count, image_size, video_count, video_resolution,
    video_duration_seconds, identity_quality, source_kind, source_id, updated_at
) VALUES (
    $1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,NULLIF($9,0),NULLIF($10,0),
    $11,$12,$13,$14,NULLIF($15,0),NULLIF($16,0),$17,$18,$19,$20,$21,$22,$23,
    NULLIF($24,0),$25,$26,$27,$28,$29,$30,$31,$32,NOW()
)
ON CONFLICT (event_key) DO UPDATE SET
    request_key=EXCLUDED.request_key, attempted_at=EXCLUDED.attempted_at,
    account_id=EXCLUDED.account_id, parent_account_id=EXCLUDED.parent_account_id,
    platform=EXCLUDED.platform, actual_model=EXCLUDED.actual_model,
    model_attribution=EXCLUDED.model_attribution, user_id=EXCLUDED.user_id,
    api_key_id=EXCLUDED.api_key_id, request_type=EXCLUDED.request_type,
    result=EXCLUDED.result, recovered=EXCLUDED.recovered,
    error_category=EXCLUDED.error_category, status_code=EXCLUDED.status_code,
    upstream_status_code=EXCLUDED.upstream_status_code,
    provider_error_code=EXCLUDED.provider_error_code,
    input_tokens=EXCLUDED.input_tokens, output_tokens=EXCLUDED.output_tokens,
    cache_creation_tokens=EXCLUDED.cache_creation_tokens,
    cache_read_tokens=EXCLUDED.cache_read_tokens, user_cost=EXCLUDED.user_cost,
    account_cost=EXCLUDED.account_cost, duration_ms=EXCLUDED.duration_ms,
    image_count=EXCLUDED.image_count, image_size=EXCLUDED.image_size,
    video_count=EXCLUDED.video_count, video_resolution=EXCLUDED.video_resolution,
    video_duration_seconds=EXCLUDED.video_duration_seconds,
    identity_quality=EXCLUDED.identity_quality, source_kind=EXCLUDED.source_kind,
    source_id=EXCLUDED.source_id, updated_at=NOW()`

const insertRequestSQL = `
INSERT INTO account_monitor_request_facts (
    request_key, occurred_at, user_id, api_key_id, account_id, group_id, platform,
    actual_model, model_attribution, request_type, result, error_category,
    status_code, input_tokens, output_tokens, cache_creation_tokens,
    cache_read_tokens, user_cost, account_cost, duration_ms, image_count,
    video_count, video_resolution, video_duration_seconds, identity_quality,
    source_kind, source_id, updated_at
) VALUES (
    $1,$2,NULLIF($3,0),NULLIF($4,0),NULLIF($5,0),$6,$7,$8,$9,$10,$11,$12,
    NULLIF($13,0),$14,$15,$16,$17,$18,$19,NULLIF($20,0),$21,$22,$23,$24,
    $25,$26,$27,NOW()
)
ON CONFLICT (request_key) DO UPDATE SET
    occurred_at=EXCLUDED.occurred_at, user_id=EXCLUDED.user_id,
    api_key_id=EXCLUDED.api_key_id, account_id=EXCLUDED.account_id,
    group_id=EXCLUDED.group_id,
    platform=EXCLUDED.platform, actual_model=EXCLUDED.actual_model,
    model_attribution=EXCLUDED.model_attribution, request_type=EXCLUDED.request_type,
    result=EXCLUDED.result, error_category=EXCLUDED.error_category,
    status_code=EXCLUDED.status_code, input_tokens=EXCLUDED.input_tokens,
    output_tokens=EXCLUDED.output_tokens,
    cache_creation_tokens=EXCLUDED.cache_creation_tokens,
    cache_read_tokens=EXCLUDED.cache_read_tokens, user_cost=EXCLUDED.user_cost,
    account_cost=EXCLUDED.account_cost, duration_ms=EXCLUDED.duration_ms,
    image_count=EXCLUDED.image_count, video_count=EXCLUDED.video_count,
    video_resolution=EXCLUDED.video_resolution,
    video_duration_seconds=EXCLUDED.video_duration_seconds,
    identity_quality=EXCLUDED.identity_quality, source_kind=EXCLUDED.source_kind,
    source_id=EXCLUDED.source_id, updated_at=NOW()`

const upsertGroupDimensionSQL = `
INSERT INTO account_monitor_group_dimensions (
    group_id, name, platform, status, deleted_at, synced_at
) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (group_id) DO UPDATE SET
    name=EXCLUDED.name, platform=EXCLUDED.platform, status=EXCLUDED.status,
    deleted_at=EXCLUDED.deleted_at, synced_at=EXCLUDED.synced_at`

const upsertSyncStateSQL = `
INSERT INTO account_monitor_sync_state(source, cursor_time, cursor_id, last_success_at, last_error, updated_at)
VALUES ($1,$2,$3,$4,'',NOW())
ON CONFLICT (source) DO UPDATE SET cursor_time=EXCLUDED.cursor_time,
cursor_id=EXCLUDED.cursor_id,last_success_at=EXCLUDED.last_success_at,last_error='',updated_at=NOW()`

const upsertSyncErrorSQL = `
INSERT INTO account_monitor_sync_state(source,cursor_time,cursor_id,last_success_at,last_error,updated_at)
VALUES ($1,to_timestamp(0),0,to_timestamp(0),$2,NOW())
ON CONFLICT (source) DO UPDATE SET last_error=EXCLUDED.last_error,updated_at=NOW()`

const clearSyncErrorSQL = `UPDATE account_monitor_sync_state SET last_error='',updated_at=NOW() WHERE source=$1 AND last_error<>''`

const upsertSyncSuccessSQL = `
INSERT INTO account_monitor_sync_state(source,cursor_time,cursor_id,last_success_at,last_error,updated_at)
VALUES ($1,to_timestamp(0),0,$2,'',NOW())
ON CONFLICT (source) DO UPDATE SET last_success_at=EXCLUDED.last_success_at,last_error='',updated_at=NOW()`

const rebuildLockSQL = `SELECT pg_advisory_xact_lock(87921345)`

const rebuildOverlapSQL = `
SELECT EXISTS(
    SELECT 1 FROM account_monitor_rebuild_jobs
    WHERE status IN ('pending','running')
      AND tstzrange(from_time,to_time,'[)') && tstzrange($1,$2,'[)')
)`

const insertRebuildJobSQL = `
INSERT INTO account_monitor_rebuild_jobs(from_time,to_time,status,requested_by)
VALUES ($1,$2,'pending',$3)
RETURNING id,created_at`

const claimRebuildJobSQL = `
WITH candidate AS (
    SELECT id FROM account_monitor_rebuild_jobs
    WHERE status='pending' ORDER BY created_at,id
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE account_monitor_rebuild_jobs AS jobs
SET status='running',started_at=NOW(),error=''
FROM candidate WHERE jobs.id=candidate.id
RETURNING jobs.id,jobs.from_time,jobs.to_time,jobs.status,jobs.processed_rows,
          jobs.error,jobs.requested_by,jobs.created_at,jobs.started_at,jobs.completed_at`

const finishRebuildJobSQL = `
UPDATE account_monitor_rebuild_jobs
SET status=$2,processed_rows=$3,error=$4,completed_at=NOW()
WHERE id=$1`

var cleanupDetailSQL = []string{
	`DELETE FROM account_monitor_attempt_facts WHERE attempted_at < $1`,
	`DELETE FROM account_monitor_request_facts WHERE occurred_at < $1`,
	`DELETE FROM account_monitor_account_minute WHERE bucket_at < $1`,
	`DELETE FROM account_monitor_account_model_minute WHERE bucket_at < $1`,
	`DELETE FROM account_monitor_group_model_10m WHERE bucket_at < $1`,
}

var cleanupDailySQL = []string{
	`DELETE FROM account_monitor_account_daily WHERE bucket_date < $1::date`,
	`DELETE FROM account_monitor_account_model_daily WHERE bucket_date < $1::date`,
	`DELETE FROM account_monitor_account_user_daily WHERE bucket_date < $1::date`,
	`DELETE FROM account_monitor_account_error_daily WHERE bucket_date < $1::date`,
}

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func ApplySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("account monitor database is nil")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CommitBatch(ctx context.Context, batch Batch) error {
	if r == nil || r.db == nil {
		return errors.New("account monitor repository database is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, dimension := range batch.GroupDimensions {
		if _, err := tx.ExecContext(ctx, upsertGroupDimensionSQL,
			dimension.ID, dimension.Name, dimension.Platform, dimension.Status,
			dimension.DeletedAt, dimension.SyncedAt,
		); err != nil {
			return err
		}
	}
	for _, fact := range batch.Attempts {
		if _, err := tx.ExecContext(ctx, insertAttemptSQL,
			fact.EventKey, fact.RequestKey, fact.AttemptedAt, fact.AccountID, fact.ParentAccountID,
			fact.Platform, fact.ActualModel, fact.ModelAttribution, fact.UserID, fact.APIKeyID,
			fact.RequestType, fact.Result, fact.Recovered, fact.ErrorCategory, fact.StatusCode,
			fact.UpstreamStatusCode, fact.ProviderErrorCode, fact.InputTokens, fact.OutputTokens,
			fact.CacheCreationTokens, fact.CacheReadTokens, fact.UserCost, fact.AccountCost,
			fact.DurationMS, fact.ImageCount, fact.ImageSize, fact.VideoCount, fact.VideoResolution,
			fact.VideoDurationSeconds, fact.IdentityQuality, fact.SourceKind, fact.SourceID,
		); err != nil {
			return err
		}
	}
	for _, fact := range batch.Requests {
		if _, err := tx.ExecContext(ctx, insertRequestSQL,
			fact.RequestKey, fact.OccurredAt, fact.UserID, fact.APIKeyID, fact.AccountID,
			fact.GroupID, fact.Platform, fact.ActualModel, fact.ModelAttribution, fact.RequestType,
			fact.Result, fact.ErrorCategory, fact.StatusCode, fact.InputTokens,
			fact.OutputTokens, fact.CacheCreationTokens, fact.CacheReadTokens,
			fact.UserCost, fact.AccountCost, fact.DurationMS, fact.ImageCount,
			fact.VideoCount, fact.VideoResolution, fact.VideoDurationSeconds,
			fact.IdentityQuality, fact.SourceKind, fact.SourceID,
		); err != nil {
			return err
		}
	}
	if err := refreshAggregatesTx(ctx, tx, batch); err != nil {
		return err
	}
	now := r.now()
	if !batch.UsageCursor.Time.IsZero() || batch.UsageCursor.ID > 0 {
		if _, err := tx.ExecContext(ctx, upsertSyncStateSQL, "usage", batch.UsageCursor.Time, batch.UsageCursor.ID, now); err != nil {
			return err
		}
	}
	if !batch.ErrorCursor.Time.IsZero() || batch.ErrorCursor.ID > 0 {
		if _, err := tx.ExecContext(ctx, upsertSyncStateSQL, "errors", batch.ErrorCursor.Time, batch.ErrorCursor.ID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) CommitRebuildBatch(ctx context.Context, batch Batch) error {
	if r == nil || r.db == nil {
		return errors.New("account monitor repository database is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, dimension := range batch.GroupDimensions {
		if _, err := tx.ExecContext(ctx, upsertGroupDimensionSQL,
			dimension.ID, dimension.Name, dimension.Platform, dimension.Status,
			dimension.DeletedAt, dimension.SyncedAt,
		); err != nil {
			return err
		}
	}
	for _, fact := range batch.Attempts {
		if _, err := tx.ExecContext(ctx, insertAttemptSQL,
			fact.EventKey, fact.RequestKey, fact.AttemptedAt, fact.AccountID, fact.ParentAccountID,
			fact.Platform, fact.ActualModel, fact.ModelAttribution, fact.UserID, fact.APIKeyID,
			fact.RequestType, fact.Result, fact.Recovered, fact.ErrorCategory, fact.StatusCode,
			fact.UpstreamStatusCode, fact.ProviderErrorCode, fact.InputTokens, fact.OutputTokens,
			fact.CacheCreationTokens, fact.CacheReadTokens, fact.UserCost, fact.AccountCost,
			fact.DurationMS, fact.ImageCount, fact.ImageSize, fact.VideoCount, fact.VideoResolution,
			fact.VideoDurationSeconds, fact.IdentityQuality, fact.SourceKind, fact.SourceID,
		); err != nil {
			return err
		}
	}
	for _, fact := range batch.Requests {
		if _, err := tx.ExecContext(ctx, insertRequestSQL,
			fact.RequestKey, fact.OccurredAt, fact.UserID, fact.APIKeyID, fact.AccountID,
			fact.GroupID, fact.Platform, fact.ActualModel, fact.ModelAttribution, fact.RequestType,
			fact.Result, fact.ErrorCategory, fact.StatusCode, fact.InputTokens,
			fact.OutputTokens, fact.CacheCreationTokens, fact.CacheReadTokens,
			fact.UserCost, fact.AccountCost, fact.DurationMS, fact.ImageCount,
			fact.VideoCount, fact.VideoResolution, fact.VideoDurationSeconds,
			fact.IdentityQuality, fact.SourceKind, fact.SourceID,
		); err != nil {
			return err
		}
	}
	if err := refreshAggregatesTx(ctx, tx, batch); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CreateRebuildJob(ctx context.Context, from, to time.Time, actorID int64) (RebuildJob, error) {
	if err := ValidateRebuildRange(from, to); err != nil {
		return RebuildJob{}, err
	}
	if r == nil || r.db == nil {
		return RebuildJob{}, errors.New("account monitor repository database is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RebuildJob{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, rebuildLockSQL); err != nil {
		return RebuildJob{}, err
	}
	var overlaps bool
	if err := tx.QueryRowContext(ctx, rebuildOverlapSQL, from, to).Scan(&overlaps); err != nil {
		return RebuildJob{}, err
	}
	if overlaps {
		return RebuildJob{}, ErrRebuildOverlap
	}
	job := RebuildJob{From: from, To: to, Status: RebuildPending, RequestedBy: actorID}
	if err := tx.QueryRowContext(ctx, insertRebuildJobSQL, from, to, actorID).Scan(&job.ID, &job.CreatedAt); err != nil {
		return RebuildJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return RebuildJob{}, err
	}
	return job, nil
}

func (r *Repository) ClaimNextRebuildJob(ctx context.Context) (RebuildJob, bool, error) {
	if r == nil || r.db == nil {
		return RebuildJob{}, false, errors.New("account monitor repository database is nil")
	}
	var job RebuildJob
	var startedAt, completedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, claimRebuildJobSQL).Scan(
		&job.ID, &job.From, &job.To, &job.Status, &job.ProcessedRows,
		&job.Error, &job.RequestedBy, &job.CreatedAt, &startedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebuildJob{}, false, nil
	}
	if err != nil {
		return RebuildJob{}, false, err
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, true, nil
}

func (r *Repository) FinishRebuildJob(ctx context.Context, id, processedRows int64, rebuildErr error) error {
	status := RebuildCompleted
	errorText := ""
	if rebuildErr != nil {
		status = RebuildFailed
		errorText = rebuildErr.Error()
	}
	_, err := r.db.ExecContext(ctx, finishRebuildJobSQL, id, status, processedRows, errorText)
	return err
}

func (r *Repository) LoadCursors(ctx context.Context) (Cursor, Cursor, error) {
	if r == nil || r.db == nil {
		return Cursor{}, Cursor{}, errors.New("account monitor repository database is nil")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT source,cursor_time,cursor_id FROM account_monitor_sync_state WHERE source IN ('usage','errors')`)
	if err != nil {
		return Cursor{}, Cursor{}, err
	}
	defer rows.Close()
	var usage, errorCursor Cursor
	for rows.Next() {
		var source string
		var cursor Cursor
		if err := rows.Scan(&source, &cursor.Time, &cursor.ID); err != nil {
			return Cursor{}, Cursor{}, err
		}
		if cursor.Time.Unix() <= 0 {
			cursor = Cursor{}
		}
		if source == "usage" {
			usage = cursor
		} else if source == "errors" {
			errorCursor = cursor
		}
	}
	return usage, errorCursor, rows.Err()
}

func (r *Repository) RecordSyncError(ctx context.Context, source string, syncErr error) error {
	if r == nil || r.db == nil || syncErr == nil {
		return nil
	}
	message := strings.TrimSpace(syncErr.Error())
	if len(message) > 2048 {
		message = message[:2048]
	}
	_, err := r.db.ExecContext(ctx, upsertSyncErrorSQL, source, message)
	return err
}

func (r *Repository) ClearSyncError(ctx context.Context, source string) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, clearSyncErrorSQL, source)
	return err
}

func (r *Repository) RecordSyncSuccess(ctx context.Context, source string, at time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, upsertSyncSuccessSQL, source, at)
	return err
}

func (r *Repository) Cleanup(ctx context.Context, now time.Time, detailRetention, dailyRetention time.Duration) error {
	if r == nil || r.db == nil {
		return errors.New("account monitor repository database is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	detailCutoff := now.Add(-detailRetention)
	for _, query := range cleanupDetailSQL {
		if _, err := tx.ExecContext(ctx, query, detailCutoff); err != nil {
			return err
		}
	}
	dailyCutoff := now.Add(-dailyRetention)
	for _, query := range cleanupDailySQL {
		if _, err := tx.ExecContext(ctx, query, dailyCutoff); err != nil {
			return err
		}
	}
	return tx.Commit()
}
