package accountmonitor

import (
	"context"
	"database/sql"
	"time"
)

var refreshAggregateSQL = []string{
	`DELETE FROM account_monitor_account_minute
    WHERE bucket_at >= date_trunc('minute',$1::timestamptz)
      AND bucket_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'`,
	`INSERT INTO account_monitor_account_minute
(bucket_at,account_id,attempts,successes,failures,recovered_failures,tokens,user_cost,account_cost,duration_sum_ms,duration_count,p95_duration_ms)
SELECT date_trunc('minute',attempted_at),account_id,COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       COUNT(*) FILTER (WHERE recovered),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),
       SUM(user_cost),SUM(account_cost),SUM(COALESCE(duration_ms,0)),COUNT(duration_ms),
       percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL)
FROM account_monitor_attempt_facts
WHERE attempted_at >= date_trunc('minute',$1::timestamptz)
  AND attempted_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'
GROUP BY 1,2`,
	`DELETE FROM account_monitor_account_model_minute
    WHERE bucket_at >= date_trunc('minute',$1::timestamptz)
      AND bucket_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'`,
	`INSERT INTO account_monitor_account_model_minute
(bucket_at,account_id,actual_model,attempts,successes,failures,tokens,user_cost,account_cost,duration_sum_ms,duration_count,p95_duration_ms)
SELECT date_trunc('minute',attempted_at),account_id,actual_model,COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),
       SUM(user_cost),SUM(account_cost),SUM(COALESCE(duration_ms,0)),COUNT(duration_ms),
       percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL)
FROM account_monitor_attempt_facts
WHERE attempted_at >= date_trunc('minute',$1::timestamptz)
  AND attempted_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'
GROUP BY 1,2,3`,
	`DELETE FROM account_monitor_account_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date`,
	`INSERT INTO account_monitor_account_daily
(bucket_date,account_id,attempts,successes,failures,recovered_failures,users,api_keys,tokens,user_cost,account_cost,duration_sum_ms,duration_count,p95_duration_ms,image_count,video_count,video_duration_seconds)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       COUNT(*) FILTER (WHERE recovered),COUNT(DISTINCT user_id) FILTER (WHERE user_id IS NOT NULL),
       COUNT(DISTINCT api_key_id) FILTER (WHERE api_key_id IS NOT NULL),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),
       SUM(user_cost),SUM(account_cost),SUM(COALESCE(duration_ms,0)),COUNT(duration_ms),
       percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL),
       SUM(image_count),SUM(video_count),SUM(video_duration_seconds)
FROM account_monitor_attempt_facts
WHERE attempted_at >= (($1::timestamptz AT TIME ZONE 'UTC')::date::timestamp AT TIME ZONE 'UTC')
  AND attempted_at < (((($2::timestamptz AT TIME ZONE 'UTC')::date + 1)::timestamp) AT TIME ZONE 'UTC')
GROUP BY 1,2`,
	`DELETE FROM account_monitor_account_model_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date`,
	`INSERT INTO account_monitor_account_model_daily
(bucket_date,account_id,actual_model,attempts,successes,failures,tokens,user_cost,account_cost,duration_sum_ms,duration_count,p95_duration_ms)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,actual_model,COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),
       SUM(user_cost),SUM(account_cost),SUM(COALESCE(duration_ms,0)),COUNT(duration_ms),
       percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL)
FROM account_monitor_attempt_facts
WHERE attempted_at >= (($1::timestamptz AT TIME ZONE 'UTC')::date::timestamp AT TIME ZONE 'UTC')
  AND attempted_at < (((($2::timestamptz AT TIME ZONE 'UTC')::date + 1)::timestamp) AT TIME ZONE 'UTC')
GROUP BY 1,2,3`,
	`DELETE FROM account_monitor_account_user_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date`,
	`INSERT INTO account_monitor_account_user_daily
(bucket_date,account_id,user_id,api_key_id,attempts,successes,failures,tokens,user_cost)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,COALESCE(user_id,0),COALESCE(api_key_id,0),COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),SUM(user_cost)
FROM account_monitor_attempt_facts
WHERE attempted_at >= (($1::timestamptz AT TIME ZONE 'UTC')::date::timestamp AT TIME ZONE 'UTC')
  AND attempted_at < (((($2::timestamptz AT TIME ZONE 'UTC')::date + 1)::timestamp) AT TIME ZONE 'UTC')
GROUP BY 1,2,3,4`,
	`DELETE FROM account_monitor_account_error_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date`,
	`INSERT INTO account_monitor_account_error_daily
(bucket_date,account_id,error_category,upstream_status_code,provider_error_code,failures,recovered_failures)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,error_category,COALESCE(upstream_status_code,0),provider_error_code,
       COUNT(*),COUNT(*) FILTER (WHERE recovered)
FROM account_monitor_attempt_facts
WHERE result='failed'
  AND attempted_at >= (($1::timestamptz AT TIME ZONE 'UTC')::date::timestamp AT TIME ZONE 'UTC')
  AND attempted_at < (((($2::timestamptz AT TIME ZONE 'UTC')::date + 1)::timestamp) AT TIME ZONE 'UTC')
GROUP BY 1,2,3,4,5`,
}

var refreshGroupAggregateSQL = []string{
	`DELETE FROM account_monitor_group_model_10m
    WHERE bucket_at >= date_bin('10 minutes',$1::timestamptz,TIMESTAMPTZ '1970-01-01 00:00:00+00')
      AND bucket_at < LEAST(
        date_bin('10 minutes',$2::timestamptz,TIMESTAMPTZ '1970-01-01 00:00:00+00') + interval '10 minutes',
        date_bin('10 minutes',CURRENT_TIMESTAMP,TIMESTAMPTZ '1970-01-01 00:00:00+00')
      )`,
	`INSERT INTO account_monitor_group_model_10m
(bucket_at,group_id,actual_model,total_requests,successes,failures,exact_model_requests,estimated_model_requests)
SELECT date_bin('10 minutes',occurred_at,TIMESTAMPTZ '1970-01-01 00:00:00+00'),
       group_id,COALESCE(NULLIF(actual_model,''),'未知实际模型'),COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),
       COUNT(*) FILTER (WHERE result='failed'),
       COUNT(*) FILTER (WHERE model_attribution='exact'),
       COUNT(*) FILTER (WHERE model_attribution<>'exact')
FROM account_monitor_request_facts
WHERE group_id IS NOT NULL
  AND occurred_at >= date_bin('10 minutes',$1::timestamptz,TIMESTAMPTZ '1970-01-01 00:00:00+00')
  AND occurred_at < LEAST(
    date_bin('10 minutes',$2::timestamptz,TIMESTAMPTZ '1970-01-01 00:00:00+00') + interval '10 minutes',
    date_bin('10 minutes',CURRENT_TIMESTAMP,TIMESTAMPTZ '1970-01-01 00:00:00+00')
  )
GROUP BY 1,2,3`,
}

func refreshAggregatesTx(ctx context.Context, tx *sql.Tx, batch Batch) error {
	if from, to, ok := attemptRange(batch.Attempts); ok {
		for _, query := range refreshAggregateSQL {
			if _, err := tx.ExecContext(ctx, query, from, to); err != nil {
				return err
			}
		}
	}
	if from, to, ok := requestRange(batch.Requests); ok {
		for _, query := range refreshGroupAggregateSQL {
			if _, err := tx.ExecContext(ctx, query, from, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func requestRange(facts []RequestFact) (time.Time, time.Time, bool) {
	var from, to time.Time
	for _, fact := range facts {
		if fact.OccurredAt.IsZero() {
			continue
		}
		if from.IsZero() || fact.OccurredAt.Before(from) {
			from = fact.OccurredAt
		}
		if to.IsZero() || fact.OccurredAt.After(to) {
			to = fact.OccurredAt
		}
	}
	return from, to, !from.IsZero()
}

func attemptRange(facts []AttemptFact) (time.Time, time.Time, bool) {
	var from, to time.Time
	for _, fact := range facts {
		if fact.AttemptedAt.IsZero() {
			continue
		}
		if from.IsZero() || fact.AttemptedAt.Before(from) {
			from = fact.AttemptedAt
		}
		if to.IsZero() || fact.AttemptedAt.After(to) {
			to = fact.AttemptedAt
		}
	}
	return from, to, !from.IsZero()
}
