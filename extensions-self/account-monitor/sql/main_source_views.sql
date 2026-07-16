CREATE SCHEMA IF NOT EXISTS extensions_self_ro;
REVOKE ALL ON SCHEMA extensions_self_ro FROM PUBLIC;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'extensions_self_monitor_ro') THEN
        CREATE ROLE extensions_self_monitor_ro NOLOGIN;
    END IF;
END
$$;

CREATE OR REPLACE VIEW extensions_self_ro.usage_source
WITH (security_barrier = true) AS
SELECT
    u.id,
    u.created_at,
    u.user_id,
    u.api_key_id,
    u.account_id,
    a.parent_account_id,
    u.request_id,
    a.platform,
    u.model,
    u.requested_model,
    u.upstream_model,
    u.input_tokens,
    u.output_tokens,
    u.cache_creation_tokens,
    u.cache_read_tokens,
    u.total_cost,
    u.actual_cost,
    u.account_rate_multiplier,
    u.duration_ms,
    u.request_type,
    u.stream,
    u.image_count,
    u.image_size,
    u.image_input_size,
    u.image_output_size,
    u.image_size_breakdown,
    u.video_count,
    u.video_resolution,
    u.video_duration_seconds,
    u.group_id
FROM public.usage_logs AS u
JOIN public.accounts AS a ON a.id = u.account_id;

CREATE OR REPLACE VIEW extensions_self_ro.error_source
WITH (security_barrier = true) AS
SELECT
    o.id,
    o.created_at,
    o.request_id,
    o.client_request_id,
    o.user_id,
    o.api_key_id,
    o.account_id,
    o.platform,
    o.model,
    o.requested_model,
    o.upstream_model,
    o.request_type,
    o.stream,
    o.error_phase,
    o.error_type,
    o.error_source,
    o.error_owner,
    o.status_code,
    o.upstream_status_code,
    o.provider_error_code,
    o.provider_error_type,
    o.network_error_type,
    o.duration_ms,
    LEFT(COALESCE(o.error_message, ''), 512) AS error_message,
    LEFT(COALESCE(o.upstream_error_message, ''), 512) AS upstream_error_message,
    COALESCE((
        SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
            'at_unix_ms', event.value -> 'at_unix_ms',
            'platform', event.value -> 'platform',
            'account_id', event.value -> 'account_id',
            'account_name', event.value -> 'account_name',
            'upstream_model', event.value -> 'upstream_model',
            'upstream_status_code', event.value -> 'upstream_status_code',
            'kind', event.value -> 'kind',
            'message', LEFT(COALESCE(event.value ->> 'message', ''), 512),
            'detail', LEFT(COALESCE(event.value ->> 'detail', ''), 512)
        )) ORDER BY event.ordinality)
        FROM jsonb_array_elements(COALESCE(o.upstream_errors, '[]'::jsonb))
            WITH ORDINALITY AS event(value, ordinality)
    ), '[]'::jsonb) AS upstream_errors,
    o.group_id
FROM public.ops_error_logs AS o;

CREATE OR REPLACE VIEW extensions_self_ro.account_dimension
WITH (security_barrier = true) AS
SELECT id, parent_account_id, name, platform, status, schedulable, deleted_at
FROM public.accounts;

CREATE OR REPLACE VIEW extensions_self_ro.account_group_dimension
WITH (security_barrier = true) AS
SELECT
    ag.account_id,
    g.id AS group_id,
    g.name AS group_name,
    g.platform AS group_platform,
    g.status AS group_status,
    g.rate_multiplier AS group_rate_multiplier,
    g.deleted_at AS group_deleted_at
FROM public.account_groups AS ag
JOIN public.groups AS g ON g.id = ag.group_id;

CREATE OR REPLACE VIEW extensions_self_ro.user_dimension
WITH (security_barrier = true) AS
SELECT id, email, username, status, deleted_at
FROM public.users;

CREATE OR REPLACE VIEW extensions_self_ro.api_key_dimension
WITH (security_barrier = true) AS
SELECT id, user_id, name,
       CASE WHEN LENGTH(key) <= 8 THEN LEFT(key, 3) || '***' ELSE LEFT(key, 8) || '***' END AS masked_prefix,
       status, deleted_at
FROM public.api_keys;

CREATE OR REPLACE VIEW extensions_self_ro.group_dimension
WITH (security_barrier = true) AS
SELECT id, name, platform, status, deleted_at
FROM public.groups;

GRANT USAGE ON SCHEMA extensions_self_ro TO extensions_self_monitor_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA extensions_self_ro TO extensions_self_monitor_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA extensions_self_ro
    GRANT SELECT ON TABLES TO extensions_self_monitor_ro;
