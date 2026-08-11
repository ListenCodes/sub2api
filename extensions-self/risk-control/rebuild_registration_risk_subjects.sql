\set ON_ERROR_STOP on

-- Rebuild only the risk_subjects aggregate. Raw risk_events stay immutable.
-- IP-based registration signals start at cutover_at because older IP hashes may
-- have been derived from an untrusted reverse-proxy peer address.

\if :{?cutover_at}
\else
\echo 'cutover_at is required (RFC3339)'
\quit 3
\endif

\if :{?risk_mode}
\else
\echo 'risk_mode is required (shadow, review, or enforce)'
\quit 3
\endif

SELECT :'risk_mode' IN ('shadow', 'review', 'enforce') AS valid_risk_mode \gset
\if :valid_risk_mode
\else
\echo 'risk_mode must be shadow, review, or enforce'
\quit 3
\endif

BEGIN;
LOCK TABLE risk_events IN SHARE MODE;
LOCK TABLE risk_subjects IN ACCESS EXCLUSIVE MODE;

CREATE TEMP TABLE rebuilt_risk_subjects ON COMMIT DROP AS
WITH
identity_rule AS (
    SELECT *
    FROM risk_rules
    WHERE code = 'registration_identity_abuse' AND enabled
    LIMIT 1
),
ip_rule AS (
    SELECT *
    FROM risk_rules
    WHERE code = 'registration_ip_multi_account' AND enabled
    LIMIT 1
),
registration_successes AS (
    SELECT event.*
    FROM risk_events AS event
    WHERE event.user_id > 0 AND event.event_type = 'registration_success'
),
identity_metrics AS (
    SELECT target.id, COUNT(matched.id)::INTEGER AS match_count
    FROM registration_successes AS target
    LEFT JOIN identity_rule AS rule ON TRUE
    LEFT JOIN risk_events AS matched
      ON rule.id IS NOT NULL
     AND rule.event_types ? matched.event_type
     AND matched.occurred_at >= target.occurred_at - make_interval(secs => rule.window_seconds)
     AND matched.occurred_at <= target.occurred_at
     AND (
          (target.subject_id <> '' AND matched.subject_id = target.subject_id)
          OR (target.device_hash <> '' AND matched.device_hash = target.device_hash)
     )
    GROUP BY target.id
),
ip_metrics AS (
    SELECT target.id, COUNT(DISTINCT NULLIF(matched.subject_id, ''))::INTEGER AS match_count
    FROM registration_successes AS target
    LEFT JOIN ip_rule AS rule ON TRUE
    LEFT JOIN risk_events AS matched
      ON rule.id IS NOT NULL
     AND target.occurred_at >= :'cutover_at'::timestamptz
     AND matched.occurred_at >= :'cutover_at'::timestamptz
     AND rule.event_types ? matched.event_type
     AND target.ip_hash <> ''
     AND matched.ip_hash = target.ip_hash
     AND matched.occurred_at >= target.occurred_at - make_interval(secs => rule.window_seconds)
     AND matched.occurred_at <= target.occurred_at
    GROUP BY target.id
),
registration_matches AS (
    SELECT
        target.id,
        identity.id IS NOT NULL AND identity_metric.match_count >= identity.threshold AS identity_match,
        ip.id IS NOT NULL AND ip_metric.match_count >= ip.threshold AS ip_match,
        identity_metric.match_count AS identity_count,
        ip_metric.match_count AS ip_count,
        identity.window_seconds AS identity_window,
        ip.window_seconds AS ip_window,
        identity.score AS identity_score,
        ip.score AS ip_score,
        identity.risk_level AS identity_level,
        ip.risk_level AS ip_level,
        identity.action AS identity_action,
        ip.action AS ip_action
    FROM registration_successes AS target
    LEFT JOIN identity_rule AS identity ON TRUE
    LEFT JOIN ip_rule AS ip ON TRUE
    LEFT JOIN identity_metrics AS identity_metric ON identity_metric.id = target.id
    LEFT JOIN ip_metrics AS ip_metric ON ip_metric.id = target.id
),
registration_signals AS (
    SELECT
        match.id,
        LEAST(100,
            CASE WHEN match.identity_match THEN match.identity_score ELSE 0 END
            + CASE WHEN match.ip_match THEN match.ip_score ELSE 0 END
        ) AS score,
        CASE
            WHEN match.identity_match AND match.identity_level = 'critical' THEN 'critical'
            WHEN match.ip_match AND match.ip_level = 'critical' THEN 'critical'
            WHEN match.identity_match AND match.identity_level = 'high' THEN 'high'
            WHEN match.ip_match AND match.ip_level = 'high' THEN 'high'
            WHEN match.identity_match AND match.identity_level = 'medium' THEN 'medium'
            WHEN match.ip_match AND match.ip_level = 'medium' THEN 'medium'
            WHEN match.identity_match OR match.ip_match THEN 'low'
            ELSE 'none'
        END AS risk_level,
        CONCAT_WS('；',
            CASE WHEN match.identity_match THEN FORMAT(
                '命中规则：同邮箱或设备重复注册（%s 分钟内同邮箱或设备注册事件 %s 次）',
                match.identity_window / 60,
                match.identity_count
            ) END,
            CASE WHEN match.ip_match THEN FORMAT(
                '命中规则：同 IP 多账号注册（%s 分钟内同 IP 注册 %s 个账号）',
                match.ip_window / 60,
                match.ip_count
            ) END
        ) AS reason,
        CASE
            WHEN NOT match.identity_match AND NOT match.ip_match THEN 'allow'
            WHEN :'risk_mode' = 'shadow' THEN 'observe'
            WHEN :'risk_mode' = 'review' AND (
                match.identity_action IN ('ban', 'reject_candidate')
                OR match.ip_action IN ('ban', 'reject_candidate')
            ) THEN 'review'
            WHEN match.identity_match AND match.identity_action IN ('ban', 'reject_candidate') THEN match.identity_action
            WHEN match.ip_match AND match.ip_action IN ('ban', 'reject_candidate') THEN match.ip_action
            WHEN match.identity_match AND match.identity_action = 'review' THEN 'review'
            WHEN match.ip_match AND match.ip_action = 'review' THEN 'review'
            ELSE 'observe'
        END AS decision
    FROM registration_matches AS match
),
normalized_events AS (
    SELECT
        event.*,
        CASE
            WHEN event.event_type = 'registration_success' THEN COALESCE(signal.score, 0)
            WHEN event.rule_codes ? 'registration_abuse' THEN 0
            ELSE event.score
        END AS rebuilt_score,
        CASE
            WHEN event.event_type = 'registration_success' THEN COALESCE(signal.risk_level, 'none')
            WHEN event.rule_codes ? 'registration_abuse' THEN 'none'
            ELSE event.risk_level
        END AS rebuilt_risk_level,
        CASE
            WHEN event.event_type = 'registration_success' THEN COALESCE(signal.reason, '')
            WHEN event.rule_codes ? 'registration_abuse' THEN ''
            ELSE event.reason
        END AS rebuilt_reason,
        CASE
            WHEN event.event_type = 'registration_success' THEN COALESCE(signal.decision, 'allow')
            WHEN event.rule_codes ? 'registration_abuse' THEN 'allow'
            ELSE event.decision
        END AS rebuilt_decision
    FROM risk_events AS event
    LEFT JOIN registration_signals AS signal ON signal.id = event.id
    WHERE event.user_id > 0
),
ranked_signals AS (
    SELECT
        event.*,
        ROW_NUMBER() OVER (
            PARTITION BY event.user_id
            ORDER BY
                event.rebuilt_score DESC,
                CASE event.rebuilt_risk_level
                    WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0
                END DESC,
                CASE event.rebuilt_decision
                    WHEN 'ban' THEN 5 WHEN 'reject_candidate' THEN 4 WHEN 'review' THEN 3 WHEN 'observe' THEN 2 WHEN 'allow' THEN 1 ELSE 0
                END DESC,
                event.occurred_at DESC,
                event.id DESC
        ) AS signal_rank
    FROM normalized_events AS event
),
latest_events AS (
    SELECT DISTINCT ON (event.user_id)
        event.user_id,
        event.username_snapshot,
        event.email_hash,
        event.account_status_snapshot,
        event.occurred_at
    FROM normalized_events AS event
    ORDER BY event.user_id, event.occurred_at DESC, event.id DESC
),
event_counts AS (
    SELECT
        event.user_id,
        COUNT(*)::INTEGER AS event_count,
        COUNT(DISTINCT NULLIF(event.ip_hash, ''))::INTEGER AS ip_count,
        COUNT(DISTINCT NULLIF(event.device_hash, ''))::INTEGER AS device_count
    FROM normalized_events AS event
    GROUP BY event.user_id
)
SELECT
    latest.user_id,
    latest.username_snapshot AS username,
    latest.email_hash,
    latest.account_status_snapshot AS account_status,
    signal.risk_type,
    signal.rebuilt_risk_level AS risk_level,
    signal.rebuilt_score AS score,
    signal.rebuilt_reason AS reason,
    counts.event_count,
    counts.ip_count,
    counts.device_count,
    signal.rebuilt_decision AS last_action,
    signal.rebuilt_decision = 'review' AS pending,
    latest.occurred_at AS last_event_at
FROM latest_events AS latest
JOIN ranked_signals AS signal ON signal.user_id = latest.user_id AND signal.signal_rank = 1
JOIN event_counts AS counts ON counts.user_id = latest.user_id;

DELETE FROM risk_subjects;

INSERT INTO risk_subjects (
    user_id, username, email_hash, account_status, risk_type, risk_level, score, reason,
    event_count, ip_count, device_count, last_action, pending, last_event_at, updated_at
)
SELECT
    user_id, username, email_hash, account_status, risk_type, risk_level, score, reason,
    event_count, ip_count, device_count, last_action, pending, last_event_at, NOW()
FROM rebuilt_risk_subjects
ORDER BY user_id;

SELECT
    COUNT(*) AS rebuilt_subjects,
    COUNT(*) FILTER (WHERE risk_level IN ('high', 'critical')) AS high_risk_subjects,
    COUNT(*) FILTER (WHERE reason LIKE '命中规则：同邮箱或设备重复注册%') AS identity_registration_subjects,
    COUNT(*) FILTER (WHERE reason LIKE '命中规则：同 IP 多账号注册%') AS ip_multi_account_subjects
FROM risk_subjects;

COMMIT;
