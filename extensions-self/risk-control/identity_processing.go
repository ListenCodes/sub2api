package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrIdentitySignalProcessing = errors.New("identity signal processing failed")
var ErrIdentityNotEvaluable = errors.New("identity signal is not currently evaluable")
var ErrInvalidIdentity = errors.New("invalid identity input")

func (r *SQLIdentityRepository) ProcessSignalJob(ctx context.Context, eventKey string, cfg IdentityConfig) error {
	if r == nil || r.db == nil {
		return errors.New("identity repository unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var jobID, eventID int64
	var lockToken string
	err = tx.QueryRowContext(ctx, `WITH candidate AS (
 SELECT job.id,job.event_id FROM risk_signal_processing_jobs job
 JOIN risk_identity_events event ON event.id=job.event_id
 WHERE event.event_key=$1 AND (
   (job.status IN ('pending','retry') AND job.next_attempt_at<=NOW()) OR
   (job.status='processing' AND job.locked_at<NOW()-interval '2 minutes')
 )
 FOR UPDATE OF job SKIP LOCKED
), claimed AS (
 UPDATE risk_signal_processing_jobs job SET status='processing',attempts=attempts+1,locked_at=NOW(),lock_token=md5(random()::text||clock_timestamp()::text||job.id::text),updated_at=NOW()
 FROM candidate WHERE job.id=candidate.id RETURNING job.id,job.event_id,job.lock_token
) SELECT id,event_id,lock_token FROM claimed`, eventKey).Scan(&jobID, &eventID, &lockToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	event, err := r.loadPersistedIdentityEvent(ctx, eventID)
	if err == nil {
		err = r.EvaluateAndStoreSignals(ctx, event, cfg)
	}
	if err == nil {
		result, completeErr := r.db.ExecContext(ctx, `UPDATE risk_signal_processing_jobs SET status='completed',completed_at=NOW(),locked_at=NULL,lock_token=NULL,last_error='',updated_at=NOW() WHERE id=$1 AND status='processing' AND lock_token=$2`, jobID, lockToken)
		if completeErr != nil {
			return completeErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows != 1 {
			return fmt.Errorf("%w: processing lease lost", ErrIdentitySignalProcessing)
		}
		return nil
	}

	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	result, markErr := r.db.ExecContext(ctx, `UPDATE risk_signal_processing_jobs SET
 status=CASE WHEN attempts>=8 THEN 'failed' ELSE 'retry' END,
 next_attempt_at=NOW()+(LEAST(300,POWER(2,LEAST(attempts,8)))::text||' seconds')::interval,
 locked_at=NULL,lock_token=NULL,last_error=$3,updated_at=NOW()
 WHERE id=$1 AND status='processing' AND lock_token=$2`, jobID, lockToken, message)
	if markErr != nil {
		return fmt.Errorf("%w: %v; persist retry state: %v", ErrIdentitySignalProcessing, err, markErr)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("%w: %v; inspect retry state: %v", ErrIdentitySignalProcessing, err, rowsErr)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %v; processing lease lost", ErrIdentitySignalProcessing, err)
	}
	return fmt.Errorf("%w: %v", ErrIdentitySignalProcessing, err)
}

func (r *SQLIdentityRepository) loadPersistedIdentityEvent(ctx context.Context, eventID int64) (PersistedIdentityEvent, error) {
	var event PersistedIdentityEvent
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,email_lookup_key,COALESCE(network_identity_id,0),COALESCE(browser_identity_id,0),COALESCE(profile_identity_id,0),COALESCE(api_client_identity_id,0),event_type,event_class,outcome,occurred_at,ip_quality_valid,device_quality_valid FROM risk_identity_events WHERE id=$1`, eventID).Scan(&event.ID, &event.UserID, &event.EmailLookupKey, &event.NetworkID, &event.BrowserID, &event.ProfileID, &event.APIClientID, &event.EventType, &event.EventClass, &event.Outcome, &event.OccurredAt, &event.IPQualityValid, &event.DeviceQualityValid)
	return event, err
}

func (r *SQLIdentityRepository) ProcessPendingSignalJobs(ctx context.Context, cfg IdentityConfig, limit int) error {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := r.db.QueryContext(ctx, `SELECT event.event_key FROM risk_signal_processing_jobs job JOIN risk_identity_events event ON event.id=job.event_id WHERE ((job.status IN ('pending','retry') AND job.next_attempt_at<=NOW()) OR (job.status='processing' AND job.locked_at<NOW()-interval '2 minutes')) ORDER BY job.id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	keys := make([]string, 0, limit)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var firstErr error
	for _, key := range keys {
		if err := r.ProcessSignalJob(ctx, key, cfg); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *IdentityService) Run(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.repo.ExpireSignals(ctx)
			_ = s.repo.ProcessPendingSignalJobs(ctx, s.cfg, 25)
		}
	}
}

func (r *SQLIdentityRepository) ExpireSignals(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("identity repository unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT signal.user_id FROM risk_identity_signals signal
WHERE signal.user_id>0 AND signal.status='active' AND ((signal.active_until IS NOT NULL AND signal.active_until<=NOW()) OR NOT EXISTS(
 SELECT 1 FROM risk_identity_rules rule JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE rule.code=signal.rule_code AND rule.revision=signal.rule_revision AND rule.enabled AND rule.mode='shadow' AND rule.active_from<=NOW() AND (rule.active_until IS NULL OR rule.active_until>NOW()) AND version.enabled AND version.active_from<=NOW() AND (version.active_until IS NULL OR version.active_until>NOW())
))`)
	if err != nil {
		return err
	}
	userIDs := []int64{}
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_signals signal SET status=CASE WHEN signal.active_until IS NOT NULL AND signal.active_until<=NOW() THEN 'expired' ELSE 'superseded' END
WHERE signal.status='active' AND ((signal.active_until IS NOT NULL AND signal.active_until<=NOW()) OR NOT EXISTS(
 SELECT 1 FROM risk_identity_rules rule JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE rule.code=signal.rule_code AND rule.revision=signal.rule_revision AND rule.enabled AND rule.mode='shadow' AND rule.active_from<=NOW() AND (rule.active_until IS NULL OR rule.active_until>NOW()) AND version.enabled AND version.active_from<=NOW() AND (version.active_until IS NULL OR version.active_until>NOW())
))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_decisions decision SET status=signal.status,current_score=0 FROM risk_identity_signals signal WHERE decision.decision_id=signal.decision_id AND decision.status='active' AND signal.status IN ('expired','superseded')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_review_cases case_row SET current_score=COALESCE((
	 SELECT MAX(signal.score) FROM risk_identity_signals signal JOIN risk_identity_rules rule ON rule.code=signal.rule_code AND rule.revision=signal.rule_revision JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
	 WHERE signal.user_id=case_row.user_id AND signal.signal_family=case_row.signal_family AND signal.score>0 AND signal.status='active' AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW()) AND rule.enabled AND rule.mode='shadow' AND version.enabled AND version.active_from<=NOW() AND (version.active_until IS NULL OR version.active_until>NOW())
),0),updated_at=NOW() WHERE case_row.status IN ('pending','in_review','observing')`); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if err := refreshIdentityUserSummary(ctx, tx, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLIdentityRepository) RecordDeliveryHeartbeat(ctx context.Context, report IdentityDeliveryReport) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("identity repository unavailable")
	}
	if report.Source == "" || len(report.Source) > 80 || report.Generation == "" || len(report.Generation) > 64 || report.Sequence == 0 || report.QueueDepth < 0 {
		return false, identityErr("delivery")
	}
	now := time.Now().UTC()
	generatedAt, err := time.Parse(time.RFC3339Nano, report.GeneratedAt)
	if err != nil || generatedAt.Before(now.Add(-2*time.Minute)) || generatedAt.After(now.Add(2*time.Minute)) {
		return false, identityErr("generated_at")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, report.StartedAt)
	if err != nil || startedAt.After(generatedAt) || startedAt.After(now.Add(2*time.Minute)) {
		return false, identityErr("started_at")
	}
	parseOptional := func(field, value string) (any, error) {
		if value == "" {
			return nil, nil
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil || parsed.After(now.Add(2*time.Minute)) {
			return nil, identityErr(field)
		}
		return parsed.UTC(), nil
	}
	lastEventAt, err := parseOptional("last_event_at", report.LastEventAt)
	if err != nil {
		return false, err
	}
	lastSuccessAt, err := parseOptional("last_success_at", report.LastSuccessAt)
	if err != nil {
		return false, err
	}
	lastFailureAt, err := parseOptional("last_failure_at", report.LastFailureAt)
	if err != nil {
		return false, err
	}
	lastDropAt, err := parseOptional("last_drop_at", report.LastDropAt)
	if err != nil {
		return false, err
	}
	var accepted bool
	err = r.db.QueryRowContext(ctx, `WITH previous AS (
 SELECT generation,started_at,sequence,enqueued,succeeded,failed,dropped,queue_depth,gap_detected_at,gap_until FROM risk_delivery_watermarks WHERE source=$1
), upserted AS (
 INSERT INTO risk_delivery_watermarks(source,generation,started_at,sequence,enqueued,succeeded,failed,dropped,queue_depth,last_event_at,last_success_at,last_failure_at,last_drop_at,gap_detected_at,gap_until,generated_at,received_at,updated_at)
 VALUES($1::varchar,$2::varchar,$3::timestamptz,$4::bigint,$5::bigint,$6::bigint,$7::bigint,$8::bigint,$9::integer,$10::timestamptz,$11::timestamptz,$12::timestamptz,$13::timestamptz,
   CASE WHEN $7::bigint>0 OR $8::bigint>0 THEN NOW() END,
   CASE WHEN $7::bigint>0 OR $8::bigint>0 THEN NOW()+interval '24 hours' END,$14::timestamptz,NOW(),NOW())
 ON CONFLICT(source) DO UPDATE SET generation=EXCLUDED.generation,started_at=EXCLUDED.started_at,sequence=EXCLUDED.sequence,enqueued=EXCLUDED.enqueued,succeeded=EXCLUDED.succeeded,failed=EXCLUDED.failed,dropped=EXCLUDED.dropped,queue_depth=EXCLUDED.queue_depth,last_event_at=EXCLUDED.last_event_at,last_success_at=EXCLUDED.last_success_at,last_failure_at=EXCLUDED.last_failure_at,last_drop_at=EXCLUDED.last_drop_at,
 gap_detected_at=CASE WHEN
   (EXCLUDED.generation=risk_delivery_watermarks.generation AND (EXCLUDED.failed>risk_delivery_watermarks.failed OR EXCLUDED.dropped>risk_delivery_watermarks.dropped OR EXCLUDED.enqueued<risk_delivery_watermarks.enqueued OR EXCLUDED.succeeded<risk_delivery_watermarks.succeeded))
   OR (EXCLUDED.generation<>risk_delivery_watermarks.generation AND (risk_delivery_watermarks.queue_depth>0 OR risk_delivery_watermarks.enqueued>risk_delivery_watermarks.succeeded+risk_delivery_watermarks.failed+risk_delivery_watermarks.dropped))
   THEN NOW() ELSE risk_delivery_watermarks.gap_detected_at END,
 gap_until=CASE WHEN
   (EXCLUDED.generation=risk_delivery_watermarks.generation AND (EXCLUDED.failed>risk_delivery_watermarks.failed OR EXCLUDED.dropped>risk_delivery_watermarks.dropped OR EXCLUDED.enqueued<risk_delivery_watermarks.enqueued OR EXCLUDED.succeeded<risk_delivery_watermarks.succeeded))
   OR (EXCLUDED.generation<>risk_delivery_watermarks.generation AND (risk_delivery_watermarks.queue_depth>0 OR risk_delivery_watermarks.enqueued>risk_delivery_watermarks.succeeded+risk_delivery_watermarks.failed+risk_delivery_watermarks.dropped))
   THEN NOW()+interval '24 hours' ELSE risk_delivery_watermarks.gap_until END,
 generated_at=EXCLUDED.generated_at,received_at=NOW(),updated_at=NOW()
 WHERE (EXCLUDED.generation=risk_delivery_watermarks.generation AND EXCLUDED.sequence>risk_delivery_watermarks.sequence)
    OR (EXCLUDED.generation<>risk_delivery_watermarks.generation AND EXCLUDED.started_at>risk_delivery_watermarks.started_at)
 RETURNING TRUE
) SELECT COALESCE((SELECT TRUE FROM upserted),FALSE)`, report.Source, report.Generation, startedAt.UTC(), report.Sequence, report.Enqueued, report.Succeeded, report.Failed, report.Dropped, report.QueueDepth, lastEventAt, lastSuccessAt, lastFailureAt, lastDropAt, generatedAt.UTC()).Scan(&accepted)
	return accepted, err
}
