SELECT pg_advisory_xact_lock(7357811167603551941);

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
    count_strategy VARCHAR(64) NOT NULL DEFAULT 'user_events',
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
    identity_version VARCHAR(16) NOT NULL DEFAULT 'event_v2' CHECK (identity_version IN ('legacy_v1','event_v2')),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE;
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
ALTER TABLE risk_rules ADD COLUMN IF NOT EXISTS count_strategy VARCHAR(64) NOT NULL DEFAULT 'user_events';
ALTER TABLE risk_rules ALTER COLUMN count_strategy SET DEFAULT 'user_events';
ALTER TABLE risk_events ADD COLUMN IF NOT EXISTS identity_version VARCHAR(16) NOT NULL DEFAULT 'event_v2';
ALTER TABLE risk_events ALTER COLUMN identity_version SET DEFAULT 'event_v2';
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid='risk_events'::regclass
          AND conname='risk_events_identity_version_check'
          AND pg_get_constraintdef(oid) NOT LIKE '%event_v2%'
    ) THEN
        ALTER TABLE risk_events DROP CONSTRAINT risk_events_identity_version_check;
        ALTER TABLE risk_events ADD CONSTRAINT risk_events_identity_version_check CHECK (identity_version IN ('legacy_v1','event_v2'));
    END IF;
END $$;
CREATE OR REPLACE FUNCTION risk_normalize_post_cleanup_event_version() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.identity_version <> 'legacy_v1' THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM risk_schema_migrations WHERE version=3 FOR SHARE;
    IF FOUND THEN
        NEW.identity_version='event_v2';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS risk_normalize_post_cleanup_event_version ON risk_events;
CREATE TRIGGER risk_normalize_post_cleanup_event_version
BEFORE INSERT OR UPDATE OF identity_version ON risk_events
FOR EACH ROW EXECUTE FUNCTION risk_normalize_post_cleanup_event_version();
CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_audit_key ON risk_audit_logs(audit_key) WHERE audit_key IS NOT NULL AND audit_key <> '';

CREATE TABLE IF NOT EXISTS risk_event_keys (
    event_key VARCHAR(240) PRIMARY KEY,
    event_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO risk_rules (code, name, description, event_types, count_strategy, enabled, window_seconds, threshold, score, risk_level, action)
VALUES
 ('login_failure_burst', '登录失败爆发', '只统计同一账号自身的连续登录失败', '["login_failure"]', 'user_events', TRUE, 600, 5, 70, 'high', 'review'),
 ('api_error_burst', 'API 错误爆发', '已迁移到可靠性与账号监控，不再作为用户风险原因', '["api_error"]', 'user_events', FALSE, 300, 10, 35, 'medium', 'observe'),
 ('content_risk', '内容风险', '只统计当前用户自身命中的内容安全策略', '["content_risk"]', 'user_events', TRUE, 86400, 1, 85, 'high', 'review'),
 ('quota_abuse', '配额滥用', '只统计当前用户自身持续触发的配额或计费限制', '["quota_exceeded"]', 'user_events', TRUE, 3600, 5, 55, 'medium', 'review'),
 ('upstream_error', '上游错误', '已迁移到可靠性与账号监控，不再作为用户风险原因', '["upstream_error"]', 'user_events', FALSE, 600, 8, 25, 'low', 'observe')
ON CONFLICT (code) DO NOTHING;

UPDATE risk_rules SET count_strategy=CASE count_strategy
  WHEN 'associated_events' THEN 'user_events'
  WHEN 'subject_device_events' THEN 'email_subject_events'
  WHEN 'ip_distinct_subjects' THEN 'ip_distinct_success_users'
  ELSE 'user_events'
END
WHERE count_strategy IN ('associated_events','subject_device_events','ip_distinct_subjects')
  AND code NOT IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation');
UPDATE risk_rules SET enabled=FALSE,
    description=CASE code
      WHEN 'api_error_burst' THEN '已迁移到可靠性与账号监控，不再作为用户风险原因'
      ELSE '已迁移到可靠性与账号监控，不再作为用户风险原因'
    END,
    revision=revision+1,
    updated_at=NOW()
WHERE code IN ('api_error_burst','upstream_error') AND enabled=TRUE;
INSERT INTO risk_rules (code, name, description, event_types, count_strategy, enabled, window_seconds, threshold, score, risk_level, action)
SELECT seed.* FROM (VALUES
 ('registration_identity_abuse', '同邮箱或设备重复注册', '同一邮箱或设备在短时间内重复提交注册', '["registration_attempt", "registration_success"]'::jsonb, 'email_subject_events', TRUE, 600, 3, 80, 'critical', 'reject_candidate'),
 ('registration_ip_multi_account', '同 IP 多账号注册', '同一真实客户端 IP 在短时间内注册多个不同账号', '["registration_success"]'::jsonb, 'ip_distinct_success_users', TRUE, 600, 5, 60, 'high', 'review'),
 ('api_request_observation', 'V1 历史正常 API 流量记录', 'V1 历史正常 API 流量规则；已停用且不再计入用户风险摘要', '["api_request"]'::jsonb, 'user_events', FALSE, 86400, 1, 0, 'low', 'observe')
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
    evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_identity_events ADD COLUMN IF NOT EXISTS evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
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
CREATE INDEX IF NOT EXISTS idx_risk_identity_activity_user_time ON risk_identity_activity_daily(user_id,last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_identity_activity_network_time ON risk_identity_activity_daily(network_identity_id,last_seen_at DESC) WHERE network_identity_id>0;
CREATE INDEX IF NOT EXISTS idx_risk_identity_activity_device_time ON risk_identity_activity_daily(device_identity_id,last_seen_at DESC) WHERE device_identity_id>0;

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
    signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity',
    subject_kind VARCHAR(32) NOT NULL DEFAULT 'browser_instance',
    active_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_identity_rules ADD COLUMN IF NOT EXISTS signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity';
ALTER TABLE risk_identity_rules ADD COLUMN IF NOT EXISTS subject_kind VARCHAR(32) NOT NULL DEFAULT 'browser_instance';
ALTER TABLE risk_identity_rules ADD COLUMN IF NOT EXISTS active_from TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE risk_identity_rules ADD COLUMN IF NOT EXISTS active_until TIMESTAMPTZ;
ALTER TABLE risk_identity_rules ADD COLUMN IF NOT EXISTS configured_action VARCHAR(32) NOT NULL DEFAULT 'observe';
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='risk_identity_rules'::regclass AND conname='risk_identity_rules_configured_action_check') THEN
        ALTER TABLE risk_identity_rules ADD CONSTRAINT risk_identity_rules_configured_action_check CHECK (configured_action IN ('observe','review','reject_candidate','auto_ban'));
    END IF;
END $$;

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

CREATE TABLE IF NOT EXISTS risk_identity_rule_drafts (
    rule_code VARCHAR(80) PRIMARY KEY REFERENCES risk_identity_rules(code) ON DELETE CASCADE,
    base_revision INTEGER NOT NULL,
    window_seconds INTEGER NOT NULL CHECK (window_seconds > 0),
    threshold INTEGER NOT NULL CHECK (threshold > 0),
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    configured_action VARCHAR(32) NOT NULL CHECK (configured_action IN ('observe','review','reject_candidate','auto_ban')),
    reason VARCHAR(500) NOT NULL,
    updated_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS risk_identity_rule_simulations (
    id BIGSERIAL PRIMARY KEY,
    rule_code VARCHAR(80) NOT NULL REFERENCES risk_identity_rules(code) ON DELETE CASCADE,
    base_revision INTEGER NOT NULL,
    draft_snapshot JSONB NOT NULL,
    result_snapshot JSONB NOT NULL,
    requested_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_risk_identity_rule_simulations_expiry ON risk_identity_rule_simulations(rule_code,expires_at DESC);
CREATE TABLE IF NOT EXISTS risk_shared_network_label_history (
    id BIGSERIAL PRIMARY KEY,
    network_identity_id BIGINT NOT NULL REFERENCES risk_network_identities(id),
    action VARCHAR(16) NOT NULL CHECK (action IN ('apply','revoke')),
    label VARCHAR(32) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    actor_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
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
UPDATE risk_identity_rules SET signal_family='registration_email_flow',subject_kind='email' WHERE code='v2_registration_email_retries';
UPDATE risk_identity_rules SET signal_family='registration_identity',subject_kind='ip' WHERE code='v2_registration_ip_accounts';
UPDATE risk_identity_rules SET signal_family='registration_identity',subject_kind='browser_instance' WHERE code='v2_registration_device_accounts';
UPDATE risk_identity_rules SET signal_family='registration_identity',subject_kind='ip_browser' WHERE code='v2_registration_composite_accounts';
INSERT INTO risk_identity_rules(code,domain,enabled,window_seconds,threshold,score,mode,signal_family,subject_kind)
VALUES ('v2_api_client_accounts','device',TRUE,86400,2,0,'shadow','api_client_observation','api_client')
ON CONFLICT(code) DO NOTHING;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=7) THEN
    UPDATE risk_identity_rules SET configured_action='review' WHERE code IN ('v2_registration_ip_accounts','v2_registration_device_accounts');
    UPDATE risk_identity_rules SET configured_action='reject_candidate' WHERE code='v2_registration_composite_accounts';
    INSERT INTO risk_schema_migrations(version) VALUES (7);
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS risk_rule_versions (
    id BIGSERIAL PRIMARY KEY,
    rule_kind VARCHAR(24) NOT NULL CHECK (rule_kind IN ('event','identity')),
    rule_code VARCHAR(80) NOT NULL,
    revision INTEGER NOT NULL,
    signal_family VARCHAR(80) NOT NULL,
    domain VARCHAR(24) NOT NULL,
    active_from TIMESTAMPTZ NOT NULL,
    active_until TIMESTAMPTZ,
    enabled BOOLEAN NOT NULL,
    rule_snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(rule_kind,rule_code,revision)
);
CREATE INDEX IF NOT EXISTS idx_risk_rule_versions_active ON risk_rule_versions(rule_kind,rule_code,active_from DESC);

INSERT INTO risk_rule_versions(rule_kind,rule_code,revision,signal_family,domain,active_from,active_until,enabled,rule_snapshot)
SELECT 'identity',code,revision,signal_family,domain,active_from,active_until,enabled,
       jsonb_build_object('code',code,'domain',domain,'window_seconds',window_seconds,'threshold',threshold,'score',score,'mode',mode,'configured_action',configured_action,'revision',revision,'signal_family',signal_family,'subject_kind',subject_kind)
FROM risk_identity_rules
ON CONFLICT(rule_kind,rule_code,revision) DO NOTHING;

CREATE TABLE IF NOT EXISTS risk_decisions (
    decision_id VARCHAR(96) PRIMARY KEY,
    user_id BIGINT NOT NULL DEFAULT 0,
    event_id BIGINT REFERENCES risk_identity_events(id),
    mode VARCHAR(16) NOT NULL DEFAULT 'shadow' CHECK (mode='shadow'),
    status VARCHAR(24) NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','resolved','superseded')),
    current_score INTEGER NOT NULL DEFAULT 0 CHECK (current_score BETWEEN 0 AND 100),
    historical_max_score INTEGER NOT NULL DEFAULT 0 CHECK (historical_max_score BETWEEN 0 AND 100),
    risk_level VARCHAR(16) NOT NULL DEFAULT 'none',
    evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    decided_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_user_time ON risk_decisions(user_id,decided_at DESC);

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
    rule_version_id BIGINT REFERENCES risk_rule_versions(id),
    rule_revision INTEGER NOT NULL DEFAULT 1,
    signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity',
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    observing BOOLEAN NOT NULL DEFAULT TRUE CHECK (observing = TRUE),
    network_identity_id BIGINT REFERENCES risk_network_identities(id),
    device_identity_id BIGINT REFERENCES risk_device_identities(id),
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision_id VARCHAR(96) REFERENCES risk_decisions(decision_id),
    status VARCHAR(24) NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','resolved','superseded')),
    active_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active_until TIMESTAMPTZ,
    first_hit_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_hit_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(event_id, user_id, rule_code)
);
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS rule_version_id BIGINT REFERENCES risk_rule_versions(id);
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS rule_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity';
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS decision_id VARCHAR(96) REFERENCES risk_decisions(decision_id);
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS status VARCHAR(24) NOT NULL DEFAULT 'active';
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS active_from TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS active_until TIMESTAMPTZ;
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS first_hit_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE risk_identity_signals ADD COLUMN IF NOT EXISTS last_hit_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
CREATE INDEX IF NOT EXISTS idx_risk_identity_signals_current ON risk_identity_signals(user_id,status,active_until DESC);

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=4) THEN
    UPDATE risk_identity_signals SET
      status='superseded',
      active_from=occurred_at,
      active_until=COALESCE(active_until,occurred_at),
      first_hit_at=occurred_at,
      last_hit_at=occurred_at,
      evidence_snapshot=CASE WHEN evidence_snapshot='{}'::jsonb THEN evidence ELSE evidence_snapshot END
    WHERE rule_version_id IS NULL;
    UPDATE risk_identity_signals signal SET
      rule_revision=rule.revision,
      signal_family=rule.signal_family,
      rule_version_id=version.id,
      status='superseded'
    FROM risk_identity_rules rule
    LEFT JOIN risk_rule_versions version ON version.rule_kind='identity' AND version.rule_code=rule.code AND version.revision=rule.revision
    WHERE signal.rule_code=rule.code AND signal.rule_version_id IS NULL;
  END IF;
END $$;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='risk_identity_signals_active_version_check' AND conrelid='risk_identity_signals'::regclass) THEN
    ALTER TABLE risk_identity_signals ADD CONSTRAINT risk_identity_signals_active_version_check CHECK (status<>'active' OR rule_version_id IS NOT NULL) NOT VALID;
  END IF;
  ALTER TABLE risk_identity_signals VALIDATE CONSTRAINT risk_identity_signals_active_version_check;
END $$;

CREATE TABLE IF NOT EXISTS risk_signal_processing_jobs (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL UNIQUE REFERENCES risk_identity_events(id),
    status VARCHAR(24) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','retry','completed','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    lock_token VARCHAR(64),
    completed_at TIMESTAMPTZ,
    last_error VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_signal_processing_jobs ADD COLUMN IF NOT EXISTS lock_token VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_risk_signal_jobs_ready ON risk_signal_processing_jobs(status,next_attempt_at,id);

CREATE TABLE IF NOT EXISTS risk_delivery_watermarks (
    source VARCHAR(80) PRIMARY KEY,
    generation VARCHAR(64) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    sequence BIGINT NOT NULL DEFAULT 0,
    enqueued BIGINT NOT NULL DEFAULT 0,
    succeeded BIGINT NOT NULL DEFAULT 0,
    failed BIGINT NOT NULL DEFAULT 0,
    dropped BIGINT NOT NULL DEFAULT 0,
    queue_depth INTEGER NOT NULL DEFAULT 0,
    last_event_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    last_drop_at TIMESTAMPTZ,
    gap_detected_at TIMESTAMPTZ,
    gap_until TIMESTAMPTZ,
    generated_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_delivery_watermarks ADD COLUMN IF NOT EXISTS generation VARCHAR(64) NOT NULL DEFAULT 'legacy';
ALTER TABLE risk_delivery_watermarks ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE risk_delivery_watermarks ADD COLUMN IF NOT EXISTS generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE IF NOT EXISTS risk_review_cases (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    decision_id VARCHAR(96) REFERENCES risk_decisions(decision_id),
    signal_family VARCHAR(80) NOT NULL,
    status VARCHAR(24) NOT NULL CHECK (status IN ('pending','observing','in_review','resolved')),
    resolution VARCHAR(32) NOT NULL DEFAULT '',
    current_score INTEGER NOT NULL DEFAULT 0 CHECK (current_score BETWEEN 0 AND 100),
    historical_max_score INTEGER NOT NULL DEFAULT 0 CHECK (historical_max_score BETWEEN 0 AND 100),
    primary_signal VARCHAR(80) NOT NULL DEFAULT '',
    evidence_strength VARCHAR(24) NOT NULL DEFAULT 'weak',
    assignee_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    review_due_at TIMESTAMPTZ,
    observation_goal VARCHAR(500) NOT NULL DEFAULT '',
    resolution_reason VARCHAR(500) NOT NULL DEFAULT '',
    resolution_request_id VARCHAR(160) NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    opened_at TIMESTAMPTZ NOT NULL,
    last_hit_at TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS review_due_at TIMESTAMPTZ;
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS observation_goal VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS resolution_reason VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS resolution_request_id VARCHAR(160) NOT NULL DEFAULT '';
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE risk_review_cases ADD COLUMN IF NOT EXISTS last_activity_at TIMESTAMPTZ;
UPDATE risk_review_cases SET last_activity_at=GREATEST(opened_at,last_hit_at,COALESCE(resolved_at,opened_at),updated_at) WHERE last_activity_at IS NULL;
ALTER TABLE risk_review_cases ALTER COLUMN last_activity_at SET DEFAULT NOW();
ALTER TABLE risk_review_cases ALTER COLUMN last_activity_at SET NOT NULL;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=8) THEN
    UPDATE risk_review_cases SET status='in_review' WHERE status='pending' AND assignee_id>0;
    INSERT INTO risk_schema_migrations(version) VALUES (8);
  END IF;
END $$;
ALTER TABLE risk_review_cases DROP CONSTRAINT IF EXISTS risk_review_cases_user_id_signal_family_status_key;
CREATE INDEX IF NOT EXISTS idx_risk_review_cases_queue ON risk_review_cases(status,assignee_id,current_score DESC,last_hit_at DESC);
CREATE INDEX IF NOT EXISTS idx_risk_review_cases_due ON risk_review_cases(review_due_at,last_activity_at DESC) WHERE status='observing';

CREATE TABLE IF NOT EXISTS risk_case_evidence (
    id BIGSERIAL PRIMARY KEY,
    case_id BIGINT NOT NULL REFERENCES risk_review_cases(id),
    signal_id BIGINT REFERENCES risk_identity_signals(id) ON DELETE SET NULL,
    evidence_snapshot JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(case_id,signal_id)
);
DO $$ BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid='risk_case_evidence'::regclass
      AND conname='risk_case_evidence_signal_id_fkey'
      AND pg_get_constraintdef(oid) NOT LIKE '%ON DELETE SET NULL%'
  ) THEN
    ALTER TABLE risk_case_evidence DROP CONSTRAINT risk_case_evidence_signal_id_fkey;
    ALTER TABLE risk_case_evidence ADD CONSTRAINT risk_case_evidence_signal_id_fkey FOREIGN KEY(signal_id) REFERENCES risk_identity_signals(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=9) THEN
    UPDATE risk_review_cases
    SET review_due_at=COALESCE(review_due_at,COALESCE(last_activity_at,NOW())+INTERVAL '24 hours'),
        observation_goal=CASE WHEN BTRIM(observation_goal)='' THEN 'Review whether the weak signal persists or escalates' ELSE observation_goal END
    WHERE status='observing';

    WITH ranked AS (
      SELECT id,
             FIRST_VALUE(id) OVER (
               PARTITION BY user_id,signal_family
               ORDER BY CASE status WHEN 'in_review' THEN 3 WHEN 'pending' THEN 2 ELSE 1 END DESC,last_hit_at DESC,id DESC
             ) keep_id,
             ROW_NUMBER() OVER (
               PARTITION BY user_id,signal_family
               ORDER BY CASE status WHEN 'in_review' THEN 3 WHEN 'pending' THEN 2 ELSE 1 END DESC,last_hit_at DESC,id DESC
             ) ordinal
      FROM risk_review_cases
      WHERE status IN ('pending','in_review','observing')
    )
    INSERT INTO risk_case_evidence(case_id,signal_id,evidence_snapshot,occurred_at,created_at)
    SELECT ranked.keep_id,evidence.signal_id,evidence.evidence_snapshot,evidence.occurred_at,evidence.created_at
    FROM ranked JOIN risk_case_evidence evidence ON evidence.case_id=ranked.id
    WHERE ranked.ordinal>1
    ON CONFLICT(case_id,signal_id) DO NOTHING;

    WITH ranked AS (
      SELECT id,
             FIRST_VALUE(id) OVER (
               PARTITION BY user_id,signal_family
               ORDER BY CASE status WHEN 'in_review' THEN 3 WHEN 'pending' THEN 2 ELSE 1 END DESC,last_hit_at DESC,id DESC
             ) keep_id,
             ROW_NUMBER() OVER (
               PARTITION BY user_id,signal_family
               ORDER BY CASE status WHEN 'in_review' THEN 3 WHEN 'pending' THEN 2 ELSE 1 END DESC,last_hit_at DESC,id DESC
             ) ordinal
      FROM risk_review_cases
      WHERE status IN ('pending','in_review','observing')
    )
    UPDATE risk_review_cases duplicate_case
    SET status='resolved',
        resolution='data_error',
        resolution_reason='Merged into open case #'||ranked.keep_id||' while enforcing one open case per signal family',
        resolved_at=NOW(),last_activity_at=NOW(),revision=duplicate_case.revision+1,updated_at=NOW()
    FROM ranked
    WHERE duplicate_case.id=ranked.id AND ranked.ordinal>1;

    INSERT INTO risk_schema_migrations(version) VALUES (9);
  END IF;
END $$;
DROP INDEX IF EXISTS idx_risk_review_cases_open_family;
DROP INDEX IF EXISTS idx_risk_review_cases_observing_family;
CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_review_cases_unresolved_family ON risk_review_cases(user_id,signal_family) WHERE status IN ('pending','in_review','observing');

CREATE TABLE IF NOT EXISTS risk_review_feedback (
    id BIGSERIAL PRIMARY KEY,
    case_id BIGINT NOT NULL REFERENCES risk_review_cases(id),
    actor_id BIGINT NOT NULL,
    feedback VARCHAR(32) NOT NULL CHECK (feedback IN ('confirmed_abuse','legitimate_shared','insufficient_evidence','data_error','business_violation')),
    reason VARCHAR(500) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_risk_review_feedback_case ON risk_review_feedback(case_id,created_at DESC);

CREATE TABLE IF NOT EXISTS risk_shared_network_labels (
    network_identity_id BIGINT PRIMARY KEY REFERENCES risk_network_identities(id),
    label VARCHAR(32) NOT NULL CHECK (label IN ('home','company','school','public_proxy','trusted_egress','mobile_cgnat','unknown')),
    reason VARCHAR(500) NOT NULL,
    actor_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    rule_version_id BIGINT REFERENCES risk_rule_versions(id),
    rule_revision INTEGER NOT NULL DEFAULT 1,
    signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity',
    score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    observing BOOLEAN NOT NULL DEFAULT TRUE CHECK (observing = TRUE),
    network_identity_id BIGINT,
    device_identity_id BIGINT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision_id VARCHAR(96),
    status VARCHAR(24) NOT NULL DEFAULT 'superseded',
    active_from TIMESTAMPTZ,
    active_until TIMESTAMPTZ,
    occurred_at TIMESTAMPTZ NOT NULL,
    original_created_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS rule_version_id BIGINT REFERENCES risk_rule_versions(id);
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS rule_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS signal_family VARCHAR(80) NOT NULL DEFAULT 'registration_identity';
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS evidence_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS decision_id VARCHAR(96);
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS status VARCHAR(24) NOT NULL DEFAULT 'superseded';
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS active_from TIMESTAMPTZ;
ALTER TABLE risk_identity_signal_history ADD COLUMN IF NOT EXISTS active_until TIMESTAMPTZ;
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
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS evidence_high_water BIGINT NOT NULL DEFAULT 0;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS rule_watermark JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE risk_identity_rebuild_jobs ADD COLUMN IF NOT EXISTS approved_dry_run_id BIGINT;

INSERT INTO risk_schema_migrations(version) VALUES (4) ON CONFLICT(version) DO NOTHING;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=5) THEN
    UPDATE risk_rules
    SET enabled=FALSE, revision=revision+1, updated_at=NOW()
    WHERE enabled=TRUE
      AND code NOT IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')
      AND (
        count_strategy NOT IN ('user_events','email_subject_events','ip_distinct_success_users','browser_instance_distinct_success_users','api_client_distinct_users','ip_browser_cooccurrence')
        OR CASE WHEN jsonb_typeof(event_types)='array'
             THEN jsonb_array_length(event_types)=0 OR EXISTS (
               SELECT 1 FROM jsonb_array_elements_text(event_types) item(value)
               GROUP BY value HAVING COUNT(*) > 1
             )
             ELSE TRUE
           END
        OR (count_strategy='email_subject_events' AND (
          NOT event_types <@ '["registration_attempt","registration_success"]'::jsonb
          OR score<>0 OR action<>'observe'
        ))
        OR (count_strategy IN ('ip_distinct_success_users','browser_instance_distinct_success_users','ip_browser_cooccurrence')
          AND event_types<>'["registration_success"]'::jsonb)
        OR (count_strategy='api_client_distinct_users'
          AND (event_types<>'["api_error"]'::jsonb OR score<>0 OR action<>'observe'))
      );
    INSERT INTO risk_schema_migrations(version) VALUES (5);
  END IF;
END $$;

DO $$ DECLARE corrected_events BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=3)
     AND NOT EXISTS (SELECT 1 FROM risk_schema_migrations WHERE version=6) THEN
    UPDATE risk_events
    SET identity_version='event_v2'
    WHERE identity_version='legacy_v1';
    GET DIAGNOSTICS corrected_events = ROW_COUNT;
    INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason,metadata)
    VALUES (
      0,
      'reclassify_post_cleanup_v1_events',
      'system',
      'legacy_v1',
      'success',
      'Reclassified events written after the one-time V1 cleanup without deleting evidence',
      jsonb_build_object('events_reclassified',corrected_events)
    );
    INSERT INTO risk_schema_migrations(version) VALUES (6);
  END IF;
END $$;
