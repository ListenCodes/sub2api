CREATE TABLE IF NOT EXISTS account_monitor_schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS account_monitor_attempt_facts (
    id BIGSERIAL PRIMARY KEY,
    event_key TEXT NOT NULL UNIQUE,
    request_key TEXT NOT NULL,
    attempted_at TIMESTAMPTZ NOT NULL,
    account_id BIGINT NOT NULL,
    parent_account_id BIGINT,
    platform VARCHAR(50) NOT NULL DEFAULT '',
    actual_model VARCHAR(160) NOT NULL DEFAULT '',
    model_attribution VARCHAR(16) NOT NULL DEFAULT 'estimated',
    user_id BIGINT,
    api_key_id BIGINT,
    request_type SMALLINT NOT NULL DEFAULT 0,
    result VARCHAR(16) NOT NULL,
    recovered BOOLEAN NOT NULL DEFAULT FALSE,
    error_category VARCHAR(32) NOT NULL DEFAULT '',
    status_code INT,
    upstream_status_code INT,
    provider_error_code VARCHAR(128) NOT NULL DEFAULT '',
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_ms BIGINT,
    image_count INT NOT NULL DEFAULT 0,
    image_size VARCHAR(32) NOT NULL DEFAULT '',
    video_count INT NOT NULL DEFAULT 0,
    video_resolution VARCHAR(16) NOT NULL DEFAULT '',
    video_duration_seconds INT NOT NULL DEFAULT 0,
    identity_quality VARCHAR(16) NOT NULL DEFAULT 'exact',
    source_kind VARCHAR(16) NOT NULL,
    source_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_time ON account_monitor_attempt_facts (attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_account_time ON account_monitor_attempt_facts (account_id, attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_model_time ON account_monitor_attempt_facts (account_id, actual_model, attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_user_time ON account_monitor_attempt_facts (user_id, attempted_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_error_time ON account_monitor_attempt_facts (error_category, attempted_at DESC) WHERE result = 'failed';
CREATE INDEX IF NOT EXISTS idx_account_monitor_attempt_request ON account_monitor_attempt_facts (request_key);

CREATE TABLE IF NOT EXISTS account_monitor_request_facts (
    id BIGSERIAL PRIMARY KEY,
    request_key TEXT NOT NULL UNIQUE,
    occurred_at TIMESTAMPTZ NOT NULL,
    user_id BIGINT,
    api_key_id BIGINT,
    account_id BIGINT,
    group_id BIGINT,
    platform VARCHAR(50) NOT NULL DEFAULT '',
    actual_model VARCHAR(160) NOT NULL DEFAULT '',
    model_attribution VARCHAR(16) NOT NULL DEFAULT 'estimated',
    request_type SMALLINT NOT NULL DEFAULT 0,
    result VARCHAR(16) NOT NULL,
    error_category VARCHAR(32) NOT NULL DEFAULT '',
    status_code INT,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_ms BIGINT,
    image_count INT NOT NULL DEFAULT 0,
    video_count INT NOT NULL DEFAULT 0,
    video_resolution VARCHAR(16) NOT NULL DEFAULT '',
    video_duration_seconds INT NOT NULL DEFAULT 0,
    identity_quality VARCHAR(16) NOT NULL DEFAULT 'exact',
    source_kind VARCHAR(16) NOT NULL,
    source_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_monitor_request_time ON account_monitor_request_facts (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_request_user_time ON account_monitor_request_facts (user_id, occurred_at DESC);
ALTER TABLE account_monitor_request_facts ADD COLUMN IF NOT EXISTS group_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_account_monitor_request_group_time ON account_monitor_request_facts (group_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_request_time_group ON account_monitor_request_facts (occurred_at DESC, group_id);

CREATE TABLE IF NOT EXISTS account_monitor_group_dimensions (
    group_id BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    status TEXT NOT NULL,
    deleted_at TIMESTAMPTZ,
    synced_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS account_monitor_group_model_10m (
    bucket_at TIMESTAMPTZ NOT NULL,
    group_id BIGINT NOT NULL,
    actual_model VARCHAR(160) NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    exact_model_requests BIGINT NOT NULL DEFAULT 0,
    estimated_model_requests BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_at, group_id, actual_model)
);

CREATE INDEX IF NOT EXISTS idx_account_monitor_group_10m_group_time
    ON account_monitor_group_model_10m (group_id, bucket_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_monitor_group_10m_time
    ON account_monitor_group_model_10m (bucket_at DESC);

CREATE TABLE IF NOT EXISTS account_monitor_account_minute (
    bucket_at TIMESTAMPTZ NOT NULL,
    account_id BIGINT NOT NULL,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    recovered_failures BIGINT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    p95_duration_ms BIGINT,
    PRIMARY KEY (bucket_at, account_id)
);

CREATE TABLE IF NOT EXISTS account_monitor_account_model_minute (
    bucket_at TIMESTAMPTZ NOT NULL,
    account_id BIGINT NOT NULL,
    actual_model VARCHAR(160) NOT NULL,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    p95_duration_ms BIGINT,
    PRIMARY KEY (bucket_at, account_id, actual_model)
);

CREATE TABLE IF NOT EXISTS account_monitor_account_daily (
    bucket_date DATE NOT NULL,
    account_id BIGINT NOT NULL,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    recovered_failures BIGINT NOT NULL DEFAULT 0,
    users BIGINT NOT NULL DEFAULT 0,
    api_keys BIGINT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    p95_duration_ms BIGINT,
    image_count BIGINT NOT NULL DEFAULT 0,
    video_count BIGINT NOT NULL DEFAULT 0,
    video_duration_seconds BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, account_id)
);

CREATE TABLE IF NOT EXISTS account_monitor_account_model_daily (
    bucket_date DATE NOT NULL,
    account_id BIGINT NOT NULL,
    actual_model VARCHAR(160) NOT NULL,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    p95_duration_ms BIGINT,
    PRIMARY KEY (bucket_date, account_id, actual_model)
);

CREATE TABLE IF NOT EXISTS account_monitor_account_user_daily (
    bucket_date DATE NOT NULL,
    account_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL DEFAULT 0,
    attempts BIGINT NOT NULL DEFAULT 0,
    successes BIGINT NOT NULL DEFAULT 0,
    failures BIGINT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    user_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, account_id, user_id, api_key_id)
);

CREATE TABLE IF NOT EXISTS account_monitor_account_error_daily (
    bucket_date DATE NOT NULL,
    account_id BIGINT NOT NULL,
    error_category VARCHAR(32) NOT NULL,
    upstream_status_code INT NOT NULL DEFAULT 0,
    provider_error_code VARCHAR(128) NOT NULL DEFAULT '',
    failures BIGINT NOT NULL DEFAULT 0,
    recovered_failures BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, account_id, error_category, upstream_status_code, provider_error_code)
);

CREATE TABLE IF NOT EXISTS account_monitor_sync_state (
    source VARCHAR(16) PRIMARY KEY,
    cursor_time TIMESTAMPTZ NOT NULL,
    cursor_id BIGINT NOT NULL,
    last_success_at TIMESTAMPTZ NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS account_monitor_rebuild_jobs (
    id BIGSERIAL PRIMARY KEY,
    from_time TIMESTAMPTZ NOT NULL,
    to_time TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    processed_rows BIGINT NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    requested_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS account_monitor_thresholds (
    scope_type VARCHAR(16) NOT NULL,
    scope_id BIGINT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope_type, scope_id)
);

INSERT INTO account_monitor_schema_migrations(version) VALUES (1)
ON CONFLICT (version) DO NOTHING;

INSERT INTO account_monitor_schema_migrations(version) VALUES (2)
ON CONFLICT (version) DO NOTHING;
