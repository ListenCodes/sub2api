CREATE TABLE IF NOT EXISTS risk_schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS risk_rules (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(80) NOT NULL UNIQUE,
    name VARCHAR(160) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    count_strategy VARCHAR(40) NOT NULL DEFAULT 'associated_events',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    window_seconds INTEGER NOT NULL DEFAULT 600 CHECK (window_seconds > 0),
    threshold INTEGER NOT NULL DEFAULT 1 CHECK (threshold > 0),
    score INTEGER NOT NULL DEFAULT 0 CHECK (score BETWEEN 0 AND 100),
    risk_level VARCHAR(16) NOT NULL DEFAULT 'low',
    action VARCHAR(24) NOT NULL DEFAULT 'observe',
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS risk_events (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(240) NOT NULL UNIQUE,
    event_type VARCHAR(80) NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 0,
    subject_id VARCHAR(128) NOT NULL DEFAULT '',
    username_snapshot VARCHAR(160) NOT NULL DEFAULT '',
    account_status_snapshot VARCHAR(32) NOT NULL DEFAULT '',
    email_hash CHAR(64) NOT NULL DEFAULT '',
    ip_hash CHAR(64) NOT NULL DEFAULT '',
    device_hash CHAR(64) NOT NULL DEFAULT '',
    risk_type VARCHAR(80) NOT NULL DEFAULT '',
    error_code VARCHAR(120) NOT NULL DEFAULT '',
    reason VARCHAR(500) NOT NULL DEFAULT '',
    endpoint VARCHAR(160) NOT NULL DEFAULT '',
    model VARCHAR(160) NOT NULL DEFAULT '',
    http_status INTEGER NOT NULL DEFAULT 0,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision VARCHAR(24) NOT NULL DEFAULT 'allow',
    score INTEGER NOT NULL DEFAULT 0,
    risk_level VARCHAR(16) NOT NULL DEFAULT 'none',
    rule_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
	identity_version VARCHAR(16) NOT NULL DEFAULT 'legacy_v1' CHECK (identity_version IN ('legacy_v1')),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_events_user_created ON risk_events(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_events_subject_created ON risk_events(subject_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_events_type_created ON risk_events(event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_risk_events_ip_created ON risk_events(ip_hash, occurred_at DESC) WHERE ip_hash <> '';
CREATE INDEX IF NOT EXISTS idx_risk_events_device_created ON risk_events(device_hash, occurred_at DESC) WHERE device_hash <> '';

CREATE TABLE IF NOT EXISTS risk_subjects (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE,
    username VARCHAR(160) NOT NULL DEFAULT '',
    email_hash CHAR(64) NOT NULL DEFAULT '',
    account_status VARCHAR(32) NOT NULL DEFAULT '',
    risk_type VARCHAR(80) NOT NULL DEFAULT '',
    risk_level VARCHAR(16) NOT NULL DEFAULT 'none',
    score INTEGER NOT NULL DEFAULT 0,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    event_count INTEGER NOT NULL DEFAULT 0,
    ip_count INTEGER NOT NULL DEFAULT 0,
    device_count INTEGER NOT NULL DEFAULT 0,
    last_action VARCHAR(24) NOT NULL DEFAULT 'allow',
    pending BOOLEAN NOT NULL DEFAULT FALSE,
    last_event_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS risk_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    audit_key VARCHAR(240),
    actor_id BIGINT NOT NULL DEFAULT 0,
    action VARCHAR(80) NOT NULL,
    target_type VARCHAR(80) NOT NULL,
    target_id VARCHAR(160) NOT NULL,
    result VARCHAR(24) NOT NULL,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_audit_created ON risk_audit_logs(created_at DESC);

ALTER TABLE risk_subjects ADD COLUMN IF NOT EXISTS pending BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE risk_audit_logs ADD COLUMN IF NOT EXISTS audit_key VARCHAR(240);
ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS count_strategy VARCHAR(40) NOT NULL DEFAULT 'associated_events';
ALTER TABLE risk_events ADD COLUMN IF NOT EXISTS identity_version VARCHAR(16) NOT NULL DEFAULT 'legacy_v1';
CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_audit_key ON risk_audit_logs(audit_key) WHERE audit_key IS NOT NULL AND audit_key <> '';

CREATE TABLE IF NOT EXISTS risk_event_keys (
    event_key VARCHAR(240) PRIMARY KEY,
    event_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

UPDATE risk_rules
SET enabled = FALSE,
    description = '已拆分为同邮箱或设备重复注册、同 IP 多账号注册两条规则',
    revision = revision + 1,
    updated_at = NOW()
WHERE code = 'registration_abuse'
  AND revision = 1
  AND enabled = TRUE
  AND event_types = '["registration_attempt", "registration_success"]'::jsonb
  AND window_seconds = 600
  AND threshold = 3
  AND score = 80
  AND risk_level = 'critical'
  AND action = 'reject_candidate';

INSERT INTO risk_rules (code, name, description, event_types, count_strategy, enabled, window_seconds, threshold, score, risk_level, action)
VALUES
 ('login_failure_burst', '登录失败爆发', '同一账号连续登录失败', '["login_failure"]', 'associated_events', TRUE, 600, 5, 70, 'high', 'review'),
 ('api_error_burst', 'API 错误爆发', '同一用户短时间内出现大量 API 错误', '["api_error"]', 'associated_events', TRUE, 300, 10, 35, 'medium', 'observe'),
 ('content_risk', '内容风险', '命中内容安全策略', '["content_risk"]', 'associated_events', TRUE, 86400, 1, 85, 'high', 'review'),
 ('quota_abuse', '配额滥用', '持续触发配额或计费限制', '["quota_exceeded"]', 'associated_events', TRUE, 3600, 5, 55, 'medium', 'review'),
 ('upstream_error', '上游错误', '持续触发上游错误', '["upstream_error"]', 'associated_events', TRUE, 600, 8, 25, 'low', 'observe')
ON CONFLICT (code) DO NOTHING;

INSERT INTO risk_rules (code, name, description, event_types, count_strategy, enabled, window_seconds, threshold, score, risk_level, action)
SELECT seed.* FROM (VALUES
 ('registration_identity_abuse', '同邮箱或设备重复注册', '同一邮箱或设备在短时间内重复提交注册', '["registration_attempt", "registration_success"]'::jsonb, 'subject_device_events', TRUE, 600, 3, 80, 'critical', 'reject_candidate'),
 ('registration_ip_multi_account', '同 IP 多账号注册', '同一真实客户端 IP 在短时间内注册多个不同账号', '["registration_success"]'::jsonb, 'ip_distinct_subjects', TRUE, 600, 5, 60, 'high', 'review'),
 ('api_request_observation', 'V1 历史正常 API 流量记录', 'V1 历史正常 API 流量规则；已停用且不再计入用户风险摘要', '["api_request"]'::jsonb, 'associated_events', FALSE, 86400, 1, 0, 'low', 'observe')
) AS seed(code,name,description,event_types,count_strategy,enabled,window_seconds,threshold,score,risk_level,action)
WHERE NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=3)
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS risk_signature_nonces (
    nonce VARCHAR(128) PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_signature_nonces_expiry ON risk_signature_nonces(expires_at);

CREATE TABLE IF NOT EXISTS risk_network_identities (
    id BIGSERIAL PRIMARY KEY,
    identity_version VARCHAR(16) NOT NULL DEFAULT 'v2' CHECK (identity_version = 'v2'),
    lookup_key CHAR(64) NOT NULL UNIQUE,
    prefix_lookup_key CHAR(64) NOT NULL DEFAULT '',
    ip_ciphertext BYTEA NOT NULL,
    ip_nonce BYTEA NOT NULL,
    encryption_key_id VARCHAR(40) NOT NULL,
    ip_family SMALLINT NOT NULL CHECK (ip_family IN (4, 6)),
    ip_source VARCHAR(40) NOT NULL,
    is_public BOOLEAN NOT NULL,
    country_code VARCHAR(2) NOT NULL DEFAULT '',
    region VARCHAR(80) NOT NULL DEFAULT '',
    city VARCHAR(120) NOT NULL DEFAULT '',
    asn BIGINT NOT NULL DEFAULT 0,
    geo_source VARCHAR(40) NOT NULL DEFAULT '',
    geo_verified BOOLEAN NOT NULL DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_network_last_seen ON risk_network_identities(last_seen_at DESC);

CREATE TABLE IF NOT EXISTS risk_device_identities (
    id BIGSERIAL PRIMARY KEY,
    identity_version VARCHAR(16) NOT NULL DEFAULT 'v2' CHECK (identity_version = 'v2'),
    identity_kind VARCHAR(24) NOT NULL CHECK (identity_kind IN ('browser_instance', 'browser_profile', 'api_client')),
    lookup_key CHAR(64) NOT NULL,
    browser_family VARCHAR(40) NOT NULL DEFAULT '',
    os_family VARCHAR(40) NOT NULL DEFAULT '',
    device_class VARCHAR(24) NOT NULL DEFAULT '',
    language_family VARCHAR(24) NOT NULL DEFAULT '',
    cookie_status VARCHAR(24) NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(identity_kind, lookup_key)
);
CREATE INDEX IF NOT EXISTS idx_risk_device_last_seen ON risk_device_identities(last_seen_at DESC);

CREATE TABLE IF NOT EXISTS risk_identity_events (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(240) NOT NULL UNIQUE,
    identity_version VARCHAR(16) NOT NULL DEFAULT 'v2' CHECK (identity_version = 'v2'),
    event_type VARCHAR(80) NOT NULL,
    event_class VARCHAR(40) NOT NULL,
    outcome VARCHAR(32) NOT NULL DEFAULT '',
    user_id BIGINT NOT NULL DEFAULT 0,
    email_lookup_key CHAR(64) NOT NULL DEFAULT '',
    network_identity_id BIGINT REFERENCES risk_network_identities(id),
    browser_identity_id BIGINT REFERENCES risk_device_identities(id),
    profile_identity_id BIGINT REFERENCES risk_device_identities(id),
    api_client_identity_id BIGINT REFERENCES risk_device_identities(id),
    ip_quality_valid BOOLEAN NOT NULL DEFAULT FALSE,
    device_quality_valid BOOLEAN NOT NULL DEFAULT FALSE,
    proxy_chain_valid BOOLEAN NOT NULL DEFAULT FALSE,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_identity_events_user_time ON risk_identity_events(user_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_identity_events_network_time ON risk_identity_events(network_identity_id, occurred_at DESC) WHERE network_identity_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_risk_identity_events_browser_time ON risk_identity_events(browser_identity_id, occurred_at DESC) WHERE browser_identity_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_risk_identity_events_api_time ON risk_identity_events(api_client_identity_id, occurred_at DESC) WHERE api_client_identity_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS risk_user_ip_links (
    user_id BIGINT NOT NULL,
    network_identity_id BIGINT NOT NULL REFERENCES risk_network_identities(id),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    registration_success_count BIGINT NOT NULL DEFAULT 0,
    login_success_count BIGINT NOT NULL DEFAULT 0,
    api_success_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(user_id, network_identity_id)
);

CREATE TABLE IF NOT EXISTS risk_user_device_links (
    user_id BIGINT NOT NULL,
    device_identity_id BIGINT NOT NULL REFERENCES risk_device_identities(id),
    identity_kind VARCHAR(24) NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    registration_success_count BIGINT NOT NULL DEFAULT 0,
    login_success_count BIGINT NOT NULL DEFAULT 0,
    api_success_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY(user_id, device_identity_id)
);

CREATE TABLE IF NOT EXISTS risk_identity_activity_daily (
    activity_day DATE NOT NULL,
    user_id BIGINT NOT NULL,
    network_identity_id BIGINT NOT NULL DEFAULT 0,
    device_identity_id BIGINT NOT NULL DEFAULT 0,
    client_kind VARCHAR(24) NOT NULL,
    event_class VARCHAR(40) NOT NULL,
    success_count BIGINT NOT NULL DEFAULT 0,
    failure_count BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(activity_day, user_id, network_identity_id, device_identity_id, client_kind, event_class)
);

CREATE TABLE IF NOT EXISTS risk_identity_api_dedup (
    event_key VARCHAR(240) PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_identity_api_dedup_expiry ON risk_identity_api_dedup(expires_at);

CREATE TABLE IF NOT EXISTS risk_identity_rules (
    code VARCHAR(80) PRIMARY KEY,
    domain VARCHAR(24) NOT NULL CHECK (domain IN ('account', 'ip', 'device', 'composite')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    window_seconds INTEGER NOT NULL CHECK (window_seconds > 0),
    threshold INTEGER NOT NULL CHECK (threshold > 0),
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    mode VARCHAR(16) NOT NULL DEFAULT 'shadow' CHECK (mode = 'shadow'),
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION risk_reject_retired_v1_rule()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.code IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')
       AND EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=3) THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname='risk_reject_retired_v1_rule_insert'
          AND tgrelid='risk_rules'::regclass
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER risk_reject_retired_v1_rule_insert
        BEFORE INSERT ON risk_rules
        FOR EACH ROW EXECUTE FUNCTION risk_reject_retired_v1_rule();
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'risk_identity_rules'::regclass
          AND conname = 'risk_identity_rules_domain_check'
          AND pg_get_constraintdef(oid) NOT LIKE '%account%'
    ) THEN
        ALTER TABLE risk_identity_rules DROP CONSTRAINT risk_identity_rules_domain_check;
        ALTER TABLE risk_identity_rules ADD CONSTRAINT risk_identity_rules_domain_check CHECK (domain IN ('account', 'ip', 'device', 'composite'));
    END IF;
END $$;
INSERT INTO risk_identity_rules(code, domain, enabled, window_seconds, threshold, score, mode)
VALUES
 ('v2_registration_email_retries', 'account', TRUE, 600, 5, 0, 'shadow'),
 ('v2_registration_ip_accounts', 'ip', TRUE, 600, 5, 60, 'shadow'),
 ('v2_registration_device_accounts', 'device', TRUE, 600, 3, 70, 'shadow'),
 ('v2_registration_composite_accounts', 'composite', TRUE, 600, 3, 90, 'shadow')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS risk_identity_shadow_activation (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    started_at TIMESTAMPTZ NOT NULL,
    shadow_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (shadow_until >= started_at + INTERVAL '14 days')
);

CREATE TABLE IF NOT EXISTS risk_identity_signals (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES risk_identity_events(id),
    user_id BIGINT NOT NULL,
    domain VARCHAR(24) NOT NULL CHECK (domain IN ('account', 'ip', 'device', 'composite')),
    rule_code VARCHAR(80) NOT NULL,
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    observing BOOLEAN NOT NULL DEFAULT TRUE CHECK (observing = TRUE),
    network_identity_id BIGINT REFERENCES risk_network_identities(id),
    device_identity_id BIGINT REFERENCES risk_device_identities(id),
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(event_id, user_id, rule_code)
);
CREATE INDEX IF NOT EXISTS idx_risk_identity_signals_user_time ON risk_identity_signals(user_id, occurred_at DESC);
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'risk_identity_signals'::regclass
          AND conname = 'risk_identity_signals_domain_check'
          AND pg_get_constraintdef(oid) NOT LIKE '%account%'
    ) THEN
        ALTER TABLE risk_identity_signals DROP CONSTRAINT risk_identity_signals_domain_check;
        ALTER TABLE risk_identity_signals ADD CONSTRAINT risk_identity_signals_domain_check CHECK (domain IN ('account', 'ip', 'device', 'composite'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS risk_identity_signal_history (
    original_signal_id BIGINT PRIMARY KEY,
    event_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    domain VARCHAR(24) NOT NULL CHECK (domain IN ('account', 'ip', 'device', 'composite')),
    rule_code VARCHAR(80) NOT NULL,
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    observing BOOLEAN NOT NULL DEFAULT TRUE CHECK (observing = TRUE),
    network_identity_id BIGINT,
    device_identity_id BIGINT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    original_created_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_identity_signal_history_user_time ON risk_identity_signal_history(user_id, occurred_at DESC);
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'risk_identity_signal_history'::regclass
          AND conname = 'risk_identity_signal_history_domain_check'
          AND pg_get_constraintdef(oid) NOT LIKE '%account%'
    ) THEN
        ALTER TABLE risk_identity_signal_history DROP CONSTRAINT risk_identity_signal_history_domain_check;
        ALTER TABLE risk_identity_signal_history ADD CONSTRAINT risk_identity_signal_history_domain_check CHECK (domain IN ('account', 'ip', 'device', 'composite'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS risk_identity_user_summaries (
    user_id BIGINT PRIMARY KEY,
    overall_score INTEGER NOT NULL DEFAULT 0 CHECK (overall_score BETWEEN 0 AND 100),
    ip_score INTEGER NOT NULL DEFAULT 0 CHECK (ip_score BETWEEN 0 AND 100),
    device_score INTEGER NOT NULL DEFAULT 0 CHECK (device_score BETWEEN 0 AND 100),
    composite_score INTEGER NOT NULL DEFAULT 0 CHECK (composite_score BETWEEN 0 AND 100),
    ip_signal_count INTEGER NOT NULL DEFAULT 0,
    device_signal_count INTEGER NOT NULL DEFAULT 0,
    composite_signal_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS risk_identity_rebuild_jobs (
    id BIGSERIAL PRIMARY KEY,
    dry_run BOOLEAN NOT NULL,
    status VARCHAR(24) NOT NULL,
    requested_by BIGINT NOT NULL,
    legacy_api_subjects BIGINT NOT NULL DEFAULT 0,
    current_signal_users BIGINT NOT NULL DEFAULT 0,
    v2_signal_users BIGINT NOT NULL DEFAULT 0,
    current_signals BIGINT NOT NULL DEFAULT 0,
    v2_signals BIGINT NOT NULL DEFAULT 0,
    changed_subjects BIGINT NOT NULL DEFAULT 0,
    rule_hits JSONB NOT NULL DEFAULT '{}'::jsonb,
    sample_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_message VARCHAR(500) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS current_signal_users BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS current_signals BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS v2_signals BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS rule_hits JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS sample_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
