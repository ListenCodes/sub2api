package accountmonitor

import (
	"context"
	"database/sql"
	"time"
)

var refreshAggregateSQL = []string{
	`WITH cleared AS (
    DELETE FROM account_monitor_account_minute
    WHERE bucket_at >= date_trunc('minute',$1::timestamptz)
      AND bucket_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'
)
INSERT INTO account_monitor_account_minute
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
	`WITH cleared AS (
    DELETE FROM account_monitor_account_model_minute
    WHERE bucket_at >= date_trunc('minute',$1::timestamptz)
      AND bucket_at < date_trunc('minute',$2::timestamptz) + interval '1 minute'
)
INSERT INTO account_monitor_account_model_minute
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
	`WITH cleared AS (
    DELETE FROM account_monitor_account_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date
)
INSERT INTO account_monitor_account_daily
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
WHERE attempted_at >= date_trunc('day',$1::timestamptz)
  AND attempted_at < date_trunc('day',$2::timestamptz) + interval '1 day'
GROUP BY 1,2`,
	`WITH cleared AS (
    DELETE FROM account_monitor_account_model_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date
)
INSERT INTO account_monitor_account_model_daily
(bucket_date,account_id,actual_model,attempts,successes,failures,tokens,user_cost,account_cost,duration_sum_ms,duration_count,p95_duration_ms)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,actual_model,COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),
       SUM(user_cost),SUM(account_cost),SUM(COALESCE(duration_ms,0)),COUNT(duration_ms),
       percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL)
FROM account_monitor_attempt_facts
WHERE attempted_at >= date_trunc('day',$1::timestamptz)
  AND attempted_at < date_trunc('day',$2::timestamptz) + interval '1 day'
GROUP BY 1,2,3`,
	`WITH cleared AS (
    DELETE FROM account_monitor_account_user_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date
)
INSERT INTO account_monitor_account_user_daily
(bucket_date,account_id,user_id,api_key_id,attempts,successes,failures,tokens,user_cost)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,COALESCE(user_id,0),COALESCE(api_key_id,0),COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),SUM(user_cost)
FROM account_monitor_attempt_facts
WHERE attempted_at >= date_trunc('day',$1::timestamptz)
  AND attempted_at < date_trunc('day',$2::timestamptz) + interval '1 day'
GROUP BY 1,2,3,4`,
	`WITH cleared AS (
    DELETE FROM account_monitor_account_error_daily
    WHERE bucket_date >= ($1::timestamptz AT TIME ZONE 'UTC')::date
      AND bucket_date <= ($2::timestamptz AT TIME ZONE 'UTC')::date
)
INSERT INTO account_monitor_account_error_daily
(bucket_date,account_id,error_category,upstream_status_code,provider_error_code,failures,recovered_failures)
SELECT (attempted_at AT TIME ZONE 'UTC')::date,account_id,error_category,COALESCE(upstream_status_code,0),provider_error_code,
       COUNT(*),COUNT(*) FILTER (WHERE recovered)
FROM account_monitor_attempt_facts
WHERE result='failed' AND attempted_at >= date_trunc('day',$1::timestamptz)
  AND attempted_at < date_trunc('day',$2::timestamptz) + interval '1 day'
GROUP BY 1,2,3,4,5`,
}

func refreshAggregatesTx(ctx context.Context, tx *sql.Tx, batch Batch) error {
	from, to, ok := attemptRange(batch.Attempts)
	if !ok {
		return nil
	}
	for _, query := range refreshAggregateSQL {
		if _, err := tx.ExecContext(ctx, query, from, to); err != nil {
			return err
		}
	}
	return nil
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
