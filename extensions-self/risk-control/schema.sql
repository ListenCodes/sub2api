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
 ('registration_identity_abuse', '同邮箱或设备重复注册', '同一邮箱或设备在短时间内重复提交注册', '["registration_attempt", "registration_success"]', 'subject_device_events', TRUE, 600, 3, 80, 'critical', 'reject_candidate'),
 ('registration_ip_multi_account', '同 IP 多账号注册', '同一真实客户端 IP 在短时间内注册多个不同账号', '["registration_success"]', 'ip_distinct_subjects', TRUE, 600, 5, 60, 'high', 'review'),
 ('login_failure_burst', '登录失败爆发', '同一账号连续登录失败', '["login_failure"]', 'associated_events', TRUE, 600, 5, 70, 'high', 'review'),
 ('api_error_burst', 'API 错误爆发', '同一用户短时间内出现大量 API 错误', '["api_error"]', 'associated_events', TRUE, 300, 10, 35, 'medium', 'observe'),
 ('content_risk', '内容风险', '命中内容安全策略', '["content_risk"]', 'associated_events', TRUE, 86400, 1, 85, 'high', 'review'),
 ('quota_abuse', '配额滥用', '持续触发配额或计费限制', '["quota_exceeded"]', 'associated_events', TRUE, 3600, 5, 55, 'medium', 'review'),
 ('upstream_error', '上游错误', '持续触发上游错误', '["upstream_error"]', 'associated_events', TRUE, 600, 8, 25, 'low', 'observe'),
 ('api_request_observation', 'API 请求观察', '保留正常请求基线', '["api_request"]', 'associated_events', TRUE, 86400, 1, 0, 'low', 'observe')
ON CONFLICT (code) DO NOTHING;
