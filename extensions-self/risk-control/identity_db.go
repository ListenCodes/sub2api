package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/lib/pq"
)

type SQLIdentityRepository struct{ db *sql.DB }

type identityQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const minimumIdentityShadowDuration = 14 * 24 * time.Hour

const legacyV1CleanupMigrationVersion = 3

const riskSchemaAdvisoryLockID int64 = 7357811167603551941

func NewSQLIdentityRepository(db *sql.DB) *SQLIdentityRepository {
	return &SQLIdentityRepository{db: db}
}

func (r *SQLIdentityRepository) EvaluateCompositeRegistration(ctx context.Context, networkLookupKey, browserLookupKey string, occurredAt time.Time) (CompositeRegistrationEvaluation, error) {
	var result CompositeRegistrationEvaluation
	if r == nil || r.db == nil || strings.TrimSpace(networkLookupKey) == "" || strings.TrimSpace(browserLookupKey) == "" {
		return result, errors.New("identity repository unavailable")
	}
	err := r.db.QueryRowContext(ctx, `WITH active_rule AS (
	 SELECT code,window_seconds,threshold,score,revision,configured_action
 FROM risk_identity_rules
 WHERE code='v2_registration_composite_accounts' AND enabled AND mode='shadow'
   AND active_from<=$3 AND (active_until IS NULL OR active_until>$3)
), matched_identity AS (
 SELECT network.id network_id,browser.id browser_id
 FROM risk_network_identities network
 CROSS JOIN risk_device_identities browser
 WHERE network.lookup_key=$1 AND browser.identity_kind='browser_instance' AND browser.lookup_key=$2
   AND NOT EXISTS(
     SELECT 1 FROM risk_shared_network_labels label
     WHERE label.network_identity_id=network.id
       AND label.label IN ('home','company','school','trusted_egress','mobile_cgnat')
   )
), evidence AS (
 SELECT COUNT(DISTINCT event.user_id)::int account_count
 FROM active_rule rule
 LEFT JOIN matched_identity matched ON TRUE
 LEFT JOIN risk_identity_events event
   ON event.network_identity_id=matched.network_id AND event.browser_identity_id=matched.browser_id
  AND event.event_class='registration' AND event.outcome='success'
  AND event.ip_quality_valid AND event.device_quality_valid AND event.user_id>0
  AND event.occurred_at BETWEEN $3-(rule.window_seconds*interval '1 second') AND $3
)
SELECT rule.code,rule.window_seconds,rule.threshold,rule.score,rule.revision,evidence.account_count,rule.configured_action
FROM active_rule rule CROSS JOIN evidence`, networkLookupKey, browserLookupKey, occurredAt.UTC()).Scan(
		&result.RuleCode, &result.WindowSeconds, &result.Threshold, &result.Score, &result.Revision, &result.AccountCount, &result.ConfiguredAction,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CompositeRegistrationEvaluation{}, nil
	}
	return result, err
}

func (r *SQLIdentityRepository) RecordCompositeRegistrationRejection(ctx context.Context, eventKey string, evaluation CompositeRegistrationEvaluation) error {
	if r == nil || r.db == nil {
		return errors.New("identity repository unavailable")
	}
	metadata, _ := json.Marshal(map[string]any{
		"rule_code": evaluation.RuleCode, "rule_revision": evaluation.Revision,
		"evidence_accounts": evaluation.AccountCount, "candidate_account_count": evaluation.AccountCount + 1,
		"threshold": evaluation.Threshold, "window_seconds": evaluation.WindowSeconds, "score": evaluation.Score,
	})
	auditKey := fmt.Sprintf("identity-enforcement:%x", sha256.Sum256([]byte(strings.TrimSpace(eventKey))))
	_, err := r.db.ExecContext(ctx, `INSERT INTO risk_audit_logs(audit_key,actor_id,action,target_type,target_id,result,reason,metadata,created_at)
VALUES($1,0,'identity_reject_candidate','registration','candidate','success','composite registration identity threshold reached',$2,NOW())
ON CONFLICT DO NOTHING`, auditKey, metadata)
	return err
}

func (r *SQLIdentityRepository) ActivateShadowRules(ctx context.Context) error {
	_, err := r.CleanupLegacyV1(ctx)
	return err
}

func (r *SQLIdentityRepository) CleanupLegacyV1(ctx context.Context) (LegacyV1CleanupResult, error) {
	if r == nil || r.db == nil {
		return LegacyV1CleanupResult{}, errors.New("identity repository unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return LegacyV1CleanupResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, riskSchemaAdvisoryLockID); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	var applied bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM risk_schema_migrations WHERE version=$1)`, legacyV1CleanupMigrationVersion).Scan(&applied); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	if applied {
		return LegacyV1CleanupResult{}, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_events IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_schema_migrations IN EXCLUSIVE MODE`); err != nil {
		return LegacyV1CleanupResult{}, err
	}

	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM risk_schema_migrations WHERE version=$1)`, legacyV1CleanupMigrationVersion).Scan(&applied); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	if applied {
		return LegacyV1CleanupResult{}, tx.Commit()
	}

	result := LegacyV1CleanupResult{Applied: true}
	if _, err := tx.ExecContext(ctx, `DELETE FROM risk_event_keys`); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	deleted, err := tx.ExecContext(ctx, `DELETE FROM risk_events WHERE identity_version='legacy_v1'`)
	if err != nil {
		return LegacyV1CleanupResult{}, err
	}
	result.EventsDeleted, _ = deleted.RowsAffected()
	deleted, err = tx.ExecContext(ctx, `DELETE FROM risk_subjects`)
	if err != nil {
		return LegacyV1CleanupResult{}, err
	}
	result.SubjectsDeleted, _ = deleted.RowsAffected()
	deleted, err = tx.ExecContext(ctx, `DELETE FROM risk_rules WHERE code IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')`)
	if err != nil {
		return LegacyV1CleanupResult{}, err
	}
	result.RulesDeleted, _ = deleted.RowsAffected()
	metadata, _ := json.Marshal(result)
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason,metadata)
VALUES (0,'purge_legacy_v1','system','legacy_v1','success','Identity rollout: removed legacy events, projections, and retired rule configurations',$1)`, metadata); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_schema_migrations(version) VALUES ($1)`, legacyV1CleanupMigrationVersion); err != nil {
		return LegacyV1CleanupResult{}, err
	}
	return result, tx.Commit()
}

func (r *SQLIdentityRepository) ListIdentityRules(ctx context.Context) ([]IdentityRule, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("identity repository unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT code,domain,enabled,window_seconds,threshold,score,mode,configured_action,revision,signal_family,subject_kind,
COALESCE(to_char(active_from AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),''),
COALESCE(to_char(active_until AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),''),
COALESCE(to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),'')
FROM risk_identity_rules ORDER BY CASE domain WHEN 'account' THEN 1 WHEN 'ip' THEN 2 WHEN 'device' THEN 3 ELSE 4 END,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]IdentityRule, 0, 4)
	for rows.Next() {
		var rule IdentityRule
		if err := rows.Scan(&rule.Code, &rule.Domain, &rule.ConfiguredEnabled, &rule.WindowSeconds, &rule.Threshold, &rule.Score, &rule.Mode, &rule.ConfiguredAction, &rule.Revision, &rule.SignalFamily, &rule.SubjectKind, &rule.ActiveFrom, &rule.ActiveUntil, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (r *SQLIdentityRepository) EnsureShadowActivation(ctx context.Context, cfg IdentityConfig, now time.Time) error {
	if !cfg.RulesEnabled {
		return nil
	}
	if r == nil || r.db == nil {
		return errors.New("identity repository unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var recordedUntil time.Time
	err = tx.QueryRowContext(ctx, `SELECT shadow_until FROM risk_identity_shadow_activation WHERE singleton=1 FOR UPDATE`).Scan(&recordedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		if err := validateInitialShadowDeadline(now, cfg.ShadowUntil); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO risk_identity_shadow_activation(singleton,started_at,shadow_until) VALUES (1,$1,$2)`, now.UTC(), cfg.ShadowUntil.UTC()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if cfg.ShadowUntil.Before(recordedUntil) {
		return errors.New("RISK_IDENTITY_SHADOW_UNTIL must not shorten the recorded Shadow period")
	} else if cfg.ShadowUntil.After(recordedUntil) {
		if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_shadow_activation SET shadow_until=$1 WHERE singleton=1`, cfg.ShadowUntil.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateInitialShadowDeadline(now, deadline time.Time) error {
	if deadline.IsZero() || deadline.Before(now.UTC().Add(minimumIdentityShadowDuration)) {
		return errors.New("initial identity rule activation requires at least 14 full days of Shadow")
	}
	return nil
}

func (r *SQLIdentityRepository) UseNonce(ctx context.Context, nonce string, now time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("identity repository unavailable")
	}
	var inserted bool
	err := r.db.QueryRowContext(ctx, `WITH cleaned AS (
  DELETE FROM risk_signature_nonces WHERE expires_at < $2
), inserted AS (
  INSERT INTO risk_signature_nonces(nonce, expires_at) VALUES ($1, $2 + interval '10 minutes')
  ON CONFLICT (nonce) DO NOTHING RETURNING TRUE
) SELECT COALESCE((SELECT TRUE FROM inserted), FALSE)`, nonce, now).Scan(&inserted)
	return inserted, err
}

func (r *SQLIdentityRepository) Persist(ctx context.Context, fact IdentityFact) (PersistedIdentityEvent, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fact.EventKey); err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	if fact.EventClass != identityEventAPI || fact.Outcome != "success" {
		var existing PersistedIdentityEvent
		err := tx.QueryRowContext(ctx, `SELECT id,user_id,email_lookup_key,COALESCE(network_identity_id,0),COALESCE(browser_identity_id,0),COALESCE(profile_identity_id,0),COALESCE(api_client_identity_id,0),event_type,event_class,outcome,occurred_at,ip_quality_valid,device_quality_valid FROM risk_identity_events WHERE event_key=$1`, fact.EventKey).Scan(&existing.ID, &existing.UserID, &existing.EmailLookupKey, &existing.NetworkID, &existing.BrowserID, &existing.ProfileID, &existing.APIClientID, &existing.EventType, &existing.EventClass, &existing.Outcome, &existing.OccurredAt, &existing.IPQualityValid, &existing.DeviceQualityValid)
		if err == nil {
			return existing, true, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PersistedIdentityEvent{}, false, err
		}
	} else {
		if fact.UserID <= 0 {
			return PersistedIdentityEvent{}, false, errors.New("api success identity event requires user_id")
		}
		var accepted bool
		err = tx.QueryRowContext(ctx, `WITH cleaned AS (
  DELETE FROM risk_identity_api_dedup WHERE expires_at < NOW()
), inserted AS (
  INSERT INTO risk_identity_api_dedup(event_key,expires_at) VALUES ($1,NOW() + interval '2 days')
  ON CONFLICT(event_key) DO NOTHING RETURNING TRUE
) SELECT COALESCE((SELECT TRUE FROM inserted),FALSE)`, fact.EventKey).Scan(&accepted)
		if err != nil {
			return PersistedIdentityEvent{}, false, err
		}
		if !accepted {
			return PersistedIdentityEvent{UserID: fact.UserID, EventType: fact.EventType, EventClass: fact.EventClass, Outcome: fact.Outcome, OccurredAt: fact.OccurredAt}, true, tx.Commit()
		}
	}

	networkID, err := upsertNetworkIdentity(ctx, tx, fact.Network, fact.OccurredAt)
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	browserID, err := upsertDeviceIdentity(ctx, tx, fact.Browser, fact.OccurredAt)
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	profileID, err := upsertDeviceIdentity(ctx, tx, fact.Profile, fact.OccurredAt)
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	apiClientID, err := upsertDeviceIdentity(ctx, tx, fact.APIClient, fact.OccurredAt)
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}

	stored := PersistedIdentityEvent{UserID: fact.UserID, EmailLookupKey: fact.EmailLookupKey, NetworkID: networkID, BrowserID: browserID, ProfileID: profileID, APIClientID: apiClientID, EventType: fact.EventType, EventClass: fact.EventClass, Outcome: fact.Outcome, OccurredAt: fact.OccurredAt, IPQualityValid: fact.IPQualityValid, DeviceQualityValid: fact.DeviceQualityValid}
	if fact.EventClass == identityEventAPI && fact.Outcome == "success" {
		deviceID := int64(0)
		clientKind := "unknown"
		if fact.DeviceQualityValid {
			deviceID = browserID
			clientKind = "browser"
			if apiClientID > 0 {
				deviceID = apiClientID
				clientKind = "api_client"
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_activity_daily
(activity_day,user_id,network_identity_id,device_identity_id,client_kind,event_class,success_count,failure_count,first_seen_at,last_seen_at)
VALUES (($1 AT TIME ZONE 'UTC')::date,$2,$3,$4,$5,$6,1,0,$1,$1)
ON CONFLICT(activity_day,user_id,network_identity_id,device_identity_id,client_kind,event_class) DO UPDATE SET
 success_count=risk_identity_activity_daily.success_count+1,
 first_seen_at=LEAST(risk_identity_activity_daily.first_seen_at,EXCLUDED.first_seen_at),
 last_seen_at=GREATEST(risk_identity_activity_daily.last_seen_at,EXCLUDED.last_seen_at)`, fact.OccurredAt, fact.UserID, networkID, deviceID, clientKind, fact.EventClass)
		if err != nil {
			return PersistedIdentityEvent{}, false, err
		}
		return stored, false, tx.Commit()
	}

	snapshot, _ := json.Marshal(fact.EvidenceSnapshot)
	err = tx.QueryRowContext(ctx, `INSERT INTO risk_identity_events
(event_key,event_type,event_class,outcome,user_id,email_lookup_key,network_identity_id,browser_identity_id,profile_identity_id,api_client_identity_id,ip_quality_valid,device_quality_valid,proxy_chain_valid,evidence_snapshot,occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0),NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),$11,$12,$13,$14,$15)
	ON CONFLICT(event_key) DO NOTHING RETURNING id`, fact.EventKey, fact.EventType, fact.EventClass, fact.Outcome, fact.UserID, fact.EmailLookupKey, networkID, browserID, profileID, apiClientID, fact.IPQualityValid, fact.DeviceQualityValid, fact.ProxyChainValid, snapshot, fact.OccurredAt).Scan(&stored.ID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id,user_id,email_lookup_key,COALESCE(network_identity_id,0),COALESCE(browser_identity_id,0),COALESCE(profile_identity_id,0),COALESCE(api_client_identity_id,0),event_type,event_class,outcome,occurred_at,ip_quality_valid,device_quality_valid FROM risk_identity_events WHERE event_key=$1`, fact.EventKey).Scan(&stored.ID, &stored.UserID, &stored.EmailLookupKey, &stored.NetworkID, &stored.BrowserID, &stored.ProfileID, &stored.APIClientID, &stored.EventType, &stored.EventClass, &stored.Outcome, &stored.OccurredAt, &stored.IPQualityValid, &stored.DeviceQualityValid)
		if err != nil {
			return PersistedIdentityEvent{}, false, err
		}
		return stored, true, tx.Commit()
	}
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	if err := upsertIdentityLinks(ctx, tx, fact, networkID, browserID, profileID, apiClientID); err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_signal_processing_jobs(event_id,status,next_attempt_at) VALUES($1,'pending',NOW()) ON CONFLICT(event_id) DO NOTHING`, stored.ID); err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	return stored, false, nil
}

func upsertNetworkIdentity(ctx context.Context, tx *sql.Tx, value *IdentityNetworkFact, occurredAt time.Time) (int64, error) {
	if value == nil || value.LookupKey == "" {
		return 0, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO risk_network_identities
(lookup_key,prefix_lookup_key,ip_ciphertext,ip_nonce,encryption_key_id,ip_family,ip_source,is_public,country_code,region,city,asn,geo_source,geo_verified,first_seen_at,last_seen_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15)
ON CONFLICT(lookup_key) DO UPDATE SET last_seen_at=GREATEST(risk_network_identities.last_seen_at,EXCLUDED.last_seen_at),
 ip_source=EXCLUDED.ip_source,
 country_code=CASE WHEN EXCLUDED.geo_verified AND (risk_network_identities.geo_source!='cloudflare_verified' OR EXCLUDED.geo_source='cloudflare_verified') THEN EXCLUDED.country_code ELSE risk_network_identities.country_code END,
 region=CASE WHEN EXCLUDED.geo_verified AND (risk_network_identities.geo_source!='cloudflare_verified' OR EXCLUDED.geo_source='cloudflare_verified') THEN EXCLUDED.region ELSE risk_network_identities.region END,
 city=CASE WHEN EXCLUDED.geo_verified AND (risk_network_identities.geo_source!='cloudflare_verified' OR EXCLUDED.geo_source='cloudflare_verified') THEN EXCLUDED.city ELSE risk_network_identities.city END,
 asn=CASE WHEN EXCLUDED.geo_verified AND (risk_network_identities.geo_source!='cloudflare_verified' OR EXCLUDED.geo_source='cloudflare_verified') THEN EXCLUDED.asn ELSE risk_network_identities.asn END,
 geo_source=CASE WHEN EXCLUDED.geo_verified AND (risk_network_identities.geo_source!='cloudflare_verified' OR EXCLUDED.geo_source='cloudflare_verified') THEN EXCLUDED.geo_source ELSE risk_network_identities.geo_source END,
 geo_verified=risk_network_identities.geo_verified OR EXCLUDED.geo_verified
RETURNING id`, value.LookupKey, value.PrefixLookupKey, value.Ciphertext, value.Nonce, value.KeyID, value.Family, value.Source, value.Public, value.CountryCode, value.Region, value.City, value.ASN, value.GeoSource, value.GeoVerified, occurredAt).Scan(&id)
	return id, err
}

func upsertDeviceIdentity(ctx context.Context, tx *sql.Tx, value *IdentityDeviceFact, occurredAt time.Time) (int64, error) {
	if value == nil || value.LookupKey == "" {
		return 0, nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO risk_device_identities
(identity_kind,lookup_key,browser_family,os_family,device_class,language_family,cookie_status,first_seen_at,last_seen_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
ON CONFLICT(identity_kind,lookup_key) DO UPDATE SET last_seen_at=GREATEST(risk_device_identities.last_seen_at,EXCLUDED.last_seen_at),
 browser_family=EXCLUDED.browser_family,os_family=EXCLUDED.os_family,device_class=EXCLUDED.device_class,
 language_family=EXCLUDED.language_family,cookie_status=EXCLUDED.cookie_status
RETURNING id`, value.Kind, value.LookupKey, value.BrowserFamily, value.OSFamily, value.DeviceClass, value.LanguageFamily, value.CookieStatus, occurredAt).Scan(&id)
	return id, err
}

func upsertIdentityLinks(ctx context.Context, tx *sql.Tx, fact IdentityFact, networkID, browserID, profileID, apiClientID int64) error {
	if fact.UserID <= 0 || fact.Outcome != "success" || fact.EventClass == identityEventAPI {
		return nil
	}
	registration, login := int64(0), int64(0)
	switch fact.EventClass {
	case "registration":
		registration = 1
	case "login", "oauth":
		login = 1
	}
	if networkID > 0 && fact.IPQualityValid {
		_, err := tx.ExecContext(ctx, `INSERT INTO risk_user_ip_links
(user_id,network_identity_id,first_seen_at,last_seen_at,registration_success_count,login_success_count,api_success_count)
VALUES ($1,$2,$3,$3,$4,$5,0)
ON CONFLICT(user_id,network_identity_id) DO UPDATE SET first_seen_at=LEAST(risk_user_ip_links.first_seen_at,EXCLUDED.first_seen_at),last_seen_at=GREATEST(risk_user_ip_links.last_seen_at,EXCLUDED.last_seen_at),registration_success_count=risk_user_ip_links.registration_success_count+EXCLUDED.registration_success_count,login_success_count=risk_user_ip_links.login_success_count+EXCLUDED.login_success_count`, fact.UserID, networkID, fact.OccurredAt, registration, login)
		if err != nil {
			return err
		}
	}
	for _, device := range []struct {
		id     int64
		kind   string
		strong bool
	}{{browserID, "browser_instance", true}, {profileID, "browser_profile", false}, {apiClientID, "api_client", true}} {
		if device.id <= 0 || (device.strong && !fact.DeviceQualityValid) {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO risk_user_device_links
(user_id,device_identity_id,identity_kind,first_seen_at,last_seen_at,registration_success_count,login_success_count,api_success_count)
VALUES ($1,$2,$3,$4,$4,$5,$6,0)
ON CONFLICT(user_id,device_identity_id) DO UPDATE SET first_seen_at=LEAST(risk_user_device_links.first_seen_at,EXCLUDED.first_seen_at),last_seen_at=GREATEST(risk_user_device_links.last_seen_at,EXCLUDED.last_seen_at),registration_success_count=risk_user_device_links.registration_success_count+EXCLUDED.registration_success_count,login_success_count=risk_user_device_links.login_success_count+EXCLUDED.login_success_count`, fact.UserID, device.id, device.kind, fact.OccurredAt, registration, login)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLIdentityRepository) EvaluateAndStoreSignals(ctx context.Context, event PersistedIdentityEvent, cfg IdentityConfig) error {
	if !cfg.RulesEnabled || event.ID <= 0 || (event.EventClass != "registration" && event.EventClass != identityEventAPI) {
		return nil
	}
	_, qualityCounts, err := r.qualityStates(ctx, cfg)
	if err != nil {
		return err
	}
	// Processing gaps keep public summaries not_evaluable, but workers must be
	// able to drain that same backlog after its external quality blockers clear.
	qualityCounts.ProcessingPending = 0
	qualityCounts.ProcessingRetry = 0
	qualityCounts.ProcessingFailed = 0
	states := identityDomainStates(cfg, qualityCounts)
	if _, err := r.db.ExecContext(ctx, `INSERT INTO risk_rule_versions(rule_kind,rule_code,revision,signal_family,domain,active_from,active_until,enabled,rule_snapshot)
SELECT 'identity',code,revision,signal_family,domain,active_from,active_until,enabled,
 jsonb_build_object('code',code,'domain',domain,'window_seconds',window_seconds,'threshold',threshold,'score',score,'mode',mode,'configured_action',configured_action,'revision',revision,'signal_family',signal_family,'subject_kind',subject_kind)
FROM risk_identity_rules ON CONFLICT(rule_kind,rule_code,revision) DO NOTHING`); err != nil {
		return err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT rule.code,rule.domain,rule.window_seconds,rule.threshold,rule.score,rule.revision,rule.signal_family,rule.subject_kind,version.id,version.rule_snapshot
FROM risk_identity_rules rule JOIN risk_rule_versions version ON version.rule_kind='identity' AND version.rule_code=rule.code AND version.revision=rule.revision
WHERE rule.enabled=TRUE AND rule.mode='shadow' AND rule.active_from<=$1 AND (rule.active_until IS NULL OR rule.active_until>$1)`, event.OccurredAt)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rule struct {
		code, domain, family, subjectKind  string
		window, threshold, score, revision int
		versionID                          int64
		snapshot                           []byte
	}
	var rules []rule
	for rows.Next() {
		var item rule
		if err := rows.Scan(&item.code, &item.domain, &item.window, &item.threshold, &item.score, &item.revision, &item.family, &item.subjectKind, &item.versionID, &item.snapshot); err != nil {
			return err
		}
		rules = append(rules, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	notEvaluable := false
	for _, rule := range rules {
		if rule.subjectKind != "api_client" && event.EventClass != "registration" {
			continue
		}
		if rule.subjectKind == "email" {
			if event.EventType != "registration_attempt" || event.EmailLookupKey == "" {
				continue
			}
			var count int
			since := event.OccurredAt.Add(-time.Duration(rule.window) * time.Second)
			err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_events WHERE event_type='registration_attempt' AND email_lookup_key=$1 AND occurred_at BETWEEN $2 AND $3`, event.EmailLookupKey, since, event.OccurredAt).Scan(&count)
			if err != nil {
				return err
			}
			if count >= rule.threshold {
				evidenceMap := map[string]any{"evidence_count": count, "window_seconds": rule.window, "email_fingerprint": identityEmailDisplay(event.EmailLookupKey), "rule_snapshot": json.RawMessage(rule.snapshot)}
				evidence, _ := json.Marshal(evidenceMap)
				decisionID := fmt.Sprintf("identity:%d:%s:r%d", event.ID, rule.code, rule.revision)
				lifecycle := "active"
				if !event.OccurredAt.Add(time.Duration(rule.window) * time.Second).After(time.Now().UTC()) {
					lifecycle = "expired"
				}
				tx, err := r.beginCurrentIdentityRuleWrite(ctx, rule.code, rule.family, rule.revision, rule.versionID)
				if err != nil {
					return err
				}
				if tx == nil {
					continue
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO risk_decisions(decision_id,user_id,event_id,status,current_score,historical_max_score,risk_level,evidence_snapshot,decided_at) VALUES($1,0,$2,$3,0,0,'none',$4,$5) ON CONFLICT(decision_id) DO NOTHING`, decisionID, event.ID, lifecycle, evidence, event.OccurredAt); err != nil {
					tx.Rollback()
					return err
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,observing,evidence,evidence_snapshot,decision_id,status,active_from,active_until,first_hit_at,last_hit_at,occurred_at) VALUES ($1,0,'account',$2,$3,$4,$5,0,$6,TRUE,$7,$7,$8,$9::varchar,$10::timestamptz,$10::timestamptz+($11::integer*interval '1 second'),$10::timestamptz,$10::timestamptz,$10::timestamptz) ON CONFLICT(event_id,user_id,rule_code) DO UPDATE SET last_hit_at=GREATEST(risk_identity_signals.last_hit_at,EXCLUDED.last_hit_at)`, event.ID, rule.code, rule.versionID, rule.revision, rule.family, count, evidence, decisionID, lifecycle, event.OccurredAt, rule.window); err != nil {
					tx.Rollback()
					return err
				}
				if err := tx.Commit(); err != nil {
					return err
				}
			}
			continue
		}
		if event.Outcome != "success" || event.UserID <= 0 || !identityRuleDomainEnabled(cfg, rule.domain) {
			continue
		}
		if states[rule.domain] != "healthy" {
			notEvaluable = true
			continue
		}
		if rule.domain == "ip" && (!cfg.IPCollectionEnabled || !cfg.IPDomainEnabled || !event.IPQualityValid || event.NetworkID == 0) {
			continue
		}
		deviceID := event.BrowserID
		if rule.subjectKind == "api_client" {
			deviceID = event.APIClientID
		}
		if rule.subjectKind == "browser_instance" && (!cfg.DeviceCollectionEnabled || !cfg.DeviceDomainEnabled || !event.DeviceQualityValid || event.BrowserID == 0) {
			continue
		}
		if rule.subjectKind == "api_client" && (!cfg.DeviceCollectionEnabled || !cfg.DeviceDomainEnabled || event.APIClientID == 0) {
			continue
		}
		if rule.subjectKind == "ip_browser" && (!cfg.CompositeDomainEnabled || !cfg.IPCollectionEnabled || !cfg.DeviceCollectionEnabled || !cfg.IPDomainEnabled || !cfg.DeviceDomainEnabled || !event.IPQualityValid || !event.DeviceQualityValid || event.NetworkID == 0 || event.BrowserID == 0) {
			continue
		}
		var count int
		since := event.OccurredAt.Add(-time.Duration(rule.window) * time.Second)
		switch rule.domain {
		case "ip":
			err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE event_class='registration' AND outcome='success' AND ip_quality_valid AND network_identity_id=$1 AND user_id>0 AND occurred_at BETWEEN $2 AND $3`, event.NetworkID, since, event.OccurredAt).Scan(&count)
		case "device":
			if rule.subjectKind == "api_client" {
				err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE outcome='success' AND api_client_identity_id=$1 AND user_id>0 AND occurred_at BETWEEN $2 AND $3`, deviceID, since, event.OccurredAt).Scan(&count)
			} else {
				err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE event_class='registration' AND outcome='success' AND device_quality_valid AND browser_identity_id=$1 AND user_id>0 AND occurred_at BETWEEN $2 AND $3`, deviceID, since, event.OccurredAt).Scan(&count)
			}
		case "composite":
			deviceID = event.BrowserID
			err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE event_class='registration' AND outcome='success' AND ip_quality_valid AND device_quality_valid AND network_identity_id=$1 AND browser_identity_id=$2 AND user_id>0 AND occurred_at BETWEEN $3 AND $4`, event.NetworkID, deviceID, since, event.OccurredAt).Scan(&count)
		}
		if err != nil {
			return err
		}
		if count < rule.threshold {
			continue
		}
		evidenceMap := map[string]any{"evidence_count": count, "window_seconds": rule.window, "source_event_id": event.ID, "network_identity_id": event.NetworkID, "device_identity_id": deviceID, "same_event_pair": rule.subjectKind == "ip_browser", "subject_kind": rule.subjectKind, "rule_snapshot": json.RawMessage(rule.snapshot)}
		evidence, _ := json.Marshal(evidenceMap)
		decisionID := fmt.Sprintf("identity:%d:%s:r%d", event.ID, rule.code, rule.revision)
		riskLevel := identityRiskLevel(rule.score)
		lifecycle := "active"
		if !event.OccurredAt.Add(time.Duration(rule.window) * time.Second).After(time.Now().UTC()) {
			lifecycle = "expired"
		}
		tx, err := r.beginCurrentIdentityRuleWrite(ctx, rule.code, rule.family, rule.revision, rule.versionID)
		if err != nil {
			return err
		}
		if tx == nil {
			continue
		}
		suppressedBySharedNetwork := false
		if rule.domain == "ip" || rule.domain == "composite" {
			if _, err = tx.ExecContext(ctx, `LOCK TABLE risk_identity_signals IN ROW EXCLUSIVE MODE`); err != nil {
				tx.Rollback()
				return err
			}
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM risk_shared_network_labels WHERE network_identity_id=$1 AND label IN ('home','company','school','trusted_egress','mobile_cgnat'))`, event.NetworkID).Scan(&suppressedBySharedNetwork); err != nil {
				tx.Rollback()
				return err
			}
		}
		signalLifecycle := lifecycle
		if suppressedBySharedNetwork && lifecycle == "active" {
			signalLifecycle = "resolved"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_decisions(decision_id,user_id,event_id,status,current_score,historical_max_score,risk_level,evidence_snapshot,decided_at) VALUES($1,$2,$3,$4::varchar,CASE WHEN $4::varchar='active' THEN $5 ELSE 0 END,$5,$6,$7,$8) ON CONFLICT(decision_id) DO NOTHING`, decisionID, event.UserID, event.ID, signalLifecycle, rule.score, riskLevel, evidence, event.OccurredAt); err != nil {
			tx.Rollback()
			return err
		}
		var signalID int64
		err = tx.QueryRowContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,evidence_snapshot,decision_id,status,resolved_by_shared_network,active_from,active_until,first_hit_at,last_hit_at,occurred_at)
VALUES($1,$2,$3::varchar,$4,$5,$6,$7,$8,$9,TRUE,NULLIF($10,0),NULLIF($11,0),$12,$12,$13,$14::varchar,$15,$16::timestamptz,$16::timestamptz+($17::integer*interval '1 second'),$16::timestamptz,$16::timestamptz,$16::timestamptz)
ON CONFLICT(event_id,user_id,rule_code) DO UPDATE SET last_hit_at=GREATEST(risk_identity_signals.last_hit_at,EXCLUDED.last_hit_at) RETURNING id`, event.ID, event.UserID, rule.domain, rule.code, rule.versionID, rule.revision, rule.family, rule.score, count, event.NetworkID, deviceID, evidence, decisionID, signalLifecycle, suppressedBySharedNetwork && lifecycle == "active", event.OccurredAt, rule.window).Scan(&signalID)
		if err != nil {
			tx.Rollback()
			return err
		}
		if cfg.CasesEnabled && rule.score > 0 && signalLifecycle == "active" {
			if err := upsertIdentityReviewCase(ctx, tx, event, signalID, decisionID, rule.family, rule.code, rule.score, evidence); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if event.UserID > 0 {
		if err := r.refreshUserSummary(ctx, event.UserID); err != nil {
			return err
		}
	}
	if notEvaluable {
		return ErrIdentityNotEvaluable
	}
	return nil
}

func identityEmailDisplay(lookupKey string) string {
	digest := strings.ToUpper(strings.TrimSpace(lookupKey))
	if len(digest) > 4 {
		digest = digest[:4]
	}
	return "E-" + digest
}

func identityRiskLevel(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 30:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "none"
	}
}

func (r *SQLIdentityRepository) beginCurrentIdentityRuleWrite(ctx context.Context, code, signalFamily string, revision int, versionID int64) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('risk_identity_rule_lifecycle:' || $1,0))`, signalFamily); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	var current int
	err = tx.QueryRowContext(ctx, `SELECT 1
FROM risk_identity_rules rule
JOIN risk_rule_versions version ON version.id=$3 AND version.rule_kind='identity' AND version.rule_code=rule.code AND version.revision=rule.revision
WHERE rule.code=$1 AND rule.revision=$2 AND rule.enabled AND rule.mode='shadow' AND version.enabled
FOR SHARE OF rule,version`, code, revision, versionID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return nil, nil
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (r *SQLIdentityRepository) upsertIdentityReviewCase(ctx context.Context, event PersistedIdentityEvent, signalID int64, decisionID, family, ruleCode string, score int, evidence []byte) error {
	return upsertIdentityReviewCase(ctx, r.db, event, signalID, decisionID, family, ruleCode, score, evidence)
}

type identityQueryExecer interface {
	identityExecer
	identityQueryer
}

func upsertIdentityReviewCase(ctx context.Context, executor identityQueryExecer, event PersistedIdentityEvent, signalID int64, decisionID, family, ruleCode string, score int, evidence []byte) error {
	status, strength := "observing", "weak"
	if score >= 60 {
		status, strength = "pending", "medium_high"
	}
	if strings.Contains(ruleCode, "composite") {
		strength = "high"
	}
	var reviewDueAt any
	observationGoal := ""
	if status == "observing" {
		reviewDueAt = time.Now().UTC().Add(24 * time.Hour)
		observationGoal = "Review whether the weak signal persists or escalates"
	}
	var caseID int64
	err := executor.QueryRowContext(ctx, `INSERT INTO risk_review_cases(user_id,decision_id,signal_family,status,current_score,historical_max_score,primary_signal,evidence_strength,review_due_at,observation_goal,opened_at,last_hit_at)
VALUES($1,$2,$3,$4,$5,$5,$6,$7,$9,$10,$8,$8)
ON CONFLICT(user_id,signal_family) WHERE status IN ('pending','in_review','observing') DO UPDATE SET
status=CASE WHEN risk_review_cases.status='in_review' THEN 'in_review' WHEN risk_review_cases.status='observing' AND risk_review_cases.assignee_id>0 AND EXCLUDED.status='pending' THEN 'in_review' WHEN risk_review_cases.status='pending' OR EXCLUDED.status='pending' THEN 'pending' ELSE 'observing' END,
assignee_id=CASE WHEN risk_review_cases.status IN ('observing','in_review') THEN risk_review_cases.assignee_id ELSE 0 END,
review_due_at=CASE WHEN risk_review_cases.status='observing' AND EXCLUDED.status='observing' THEN COALESCE(risk_review_cases.review_due_at,EXCLUDED.review_due_at) ELSE NULL END,
observation_goal=CASE WHEN risk_review_cases.status='observing' AND EXCLUDED.status='observing' THEN COALESCE(NULLIF(risk_review_cases.observation_goal,''),EXCLUDED.observation_goal) ELSE '' END,
decision_id=EXCLUDED.decision_id,current_score=GREATEST(risk_review_cases.current_score,EXCLUDED.current_score),historical_max_score=GREATEST(risk_review_cases.historical_max_score,EXCLUDED.historical_max_score),primary_signal=CASE WHEN EXCLUDED.current_score>=risk_review_cases.current_score THEN EXCLUDED.primary_signal ELSE risk_review_cases.primary_signal END,evidence_strength=CASE WHEN EXCLUDED.current_score>=risk_review_cases.current_score THEN EXCLUDED.evidence_strength ELSE risk_review_cases.evidence_strength END,last_hit_at=GREATEST(risk_review_cases.last_hit_at,EXCLUDED.last_hit_at),last_activity_at=NOW(),revision=risk_review_cases.revision+1,updated_at=NOW()
RETURNING id`, event.UserID, decisionID, family, status, score, ruleCode, strength, event.OccurredAt, reviewDueAt, observationGoal).Scan(&caseID)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO risk_case_evidence(case_id,signal_id,evidence_snapshot,occurred_at) VALUES($1,$2,$3,$4) ON CONFLICT(case_id,signal_id) DO NOTHING`, caseID, signalID, evidence, event.OccurredAt)
	return err
}

func (r *SQLIdentityRepository) refreshUserSummary(ctx context.Context, userID int64) error {
	return refreshIdentityUserSummary(ctx, r.db, userID)
}

type identityExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func refreshIdentityUserSummary(ctx context.Context, executor identityExecer, userID int64) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO risk_identity_user_summaries(user_id,overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count,updated_at)
WITH eligible AS (
 SELECT signal.* FROM risk_identity_signals signal
 JOIN risk_identity_rules rule ON rule.code=signal.rule_code
 JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE signal.user_id=$1 AND signal.score>0 AND signal.status='active' AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW()) AND rule.enabled AND rule.mode='shadow' AND version.enabled
), family_scores AS (SELECT signal_family,MAX(score) score FROM eligible GROUP BY signal_family)
SELECT $1,COALESCE((SELECT MAX(score) FROM family_scores),0),COALESCE(MAX(score) FILTER (WHERE domain='ip'),0),COALESCE(MAX(score) FILTER (WHERE domain='device'),0),COALESCE(MAX(score) FILTER (WHERE domain='composite'),0),COUNT(DISTINCT rule_code) FILTER (WHERE domain='ip'),COUNT(DISTINCT rule_code) FILTER (WHERE domain='device'),COUNT(DISTINCT rule_code) FILTER (WHERE domain='composite'),NOW()
FROM eligible
ON CONFLICT(user_id) DO UPDATE SET overall_score=EXCLUDED.overall_score,ip_score=EXCLUDED.ip_score,device_score=EXCLUDED.device_score,composite_score=EXCLUDED.composite_score,ip_signal_count=EXCLUDED.ip_signal_count,device_signal_count=EXCLUDED.device_signal_count,composite_signal_count=EXCLUDED.composite_signal_count,updated_at=NOW()`, userID)
	return err
}

func refreshIdentityReviewCases(ctx context.Context, executor identityExecer, userIDs []int64, signalFamily string) error {
	if len(userIDs) == 0 || strings.TrimSpace(signalFamily) == "" {
		return nil
	}
	_, err := executor.ExecContext(ctx, `WITH case_state AS (
 SELECT case_row.id,COALESCE(best.score,0) current_score,COALESCE(best.rule_code,'') primary_signal,best.decision_id,
  CASE WHEN best.rule_code LIKE '%composite%' THEN 'high' WHEN COALESCE(best.score,0)>=60 THEN 'medium_high' ELSE 'weak' END evidence_strength
 FROM risk_review_cases case_row
 LEFT JOIN LATERAL (
  SELECT signal.score,signal.rule_code,signal.decision_id
  FROM risk_identity_signals signal
  JOIN risk_identity_rules rule ON rule.code=signal.rule_code
  JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
  WHERE signal.user_id=case_row.user_id AND signal.signal_family=case_row.signal_family AND signal.score>0 AND signal.status='active'
   AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW())
   AND rule.enabled AND rule.mode='shadow'
   AND version.enabled
  ORDER BY signal.score DESC,signal.last_hit_at DESC,signal.id DESC LIMIT 1
 ) best ON TRUE
 WHERE case_row.user_id=ANY($1) AND case_row.signal_family=$2 AND case_row.status IN ('pending','in_review','observing')
)
UPDATE risk_review_cases case_row SET current_score=case_state.current_score,primary_signal=case_state.primary_signal,decision_id=case_state.decision_id,evidence_strength=case_state.evidence_strength,revision=case_row.revision+1,updated_at=NOW()
FROM case_state WHERE case_row.id=case_state.id AND (case_row.current_score IS DISTINCT FROM case_state.current_score OR case_row.primary_signal IS DISTINCT FROM case_state.primary_signal OR case_row.decision_id IS DISTINCT FROM case_state.decision_id OR case_row.evidence_strength IS DISTINCT FROM case_state.evidence_strength)`, pq.Array(userIDs), signalFamily)
	return err
}

func refreshAllIdentityReviewCases(ctx context.Context, executor identityExecer) error {
	_, err := executor.ExecContext(ctx, `WITH case_state AS (
 SELECT case_row.id,COALESCE(best.score,0) current_score,COALESCE(best.rule_code,'') primary_signal,best.decision_id,
  CASE WHEN best.rule_code LIKE '%composite%' THEN 'high' WHEN COALESCE(best.score,0)>=60 THEN 'medium_high' ELSE 'weak' END evidence_strength
 FROM risk_review_cases case_row
 LEFT JOIN LATERAL (
  SELECT signal.score,signal.rule_code,signal.decision_id
  FROM risk_identity_signals signal
  JOIN risk_identity_rules rule ON rule.code=signal.rule_code
  JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
  WHERE signal.user_id=case_row.user_id AND signal.signal_family=case_row.signal_family AND signal.score>0 AND signal.status='active'
   AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW())
   AND rule.enabled AND rule.mode='shadow'
   AND version.enabled
  ORDER BY signal.score DESC,signal.last_hit_at DESC,signal.id DESC LIMIT 1
 ) best ON TRUE
 WHERE case_row.status IN ('pending','in_review','observing')
)
UPDATE risk_review_cases case_row SET current_score=case_state.current_score,primary_signal=case_state.primary_signal,decision_id=case_state.decision_id,evidence_strength=case_state.evidence_strength,revision=case_row.revision+1,updated_at=NOW()
FROM case_state WHERE case_row.id=case_state.id AND (case_row.current_score IS DISTINCT FROM case_state.current_score OR case_row.primary_signal IS DISTINCT FROM case_state.primary_signal OR case_row.decision_id IS DISTINCT FROM case_state.decision_id OR case_row.evidence_strength IS DISTINCT FROM case_state.evidence_strength)`)
	return err
}

func (r *SQLIdentityRepository) Summary(ctx context.Context, userID int64, cfg IdentityConfig) (IdentitySummary, error) {
	result := IdentitySummary{UserID: userID, IdentityVersion: identityVersionV2, Mode: identityMode(cfg)}
	var legacyCleaned bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM risk_schema_migrations WHERE version=$1)`, legacyV1CleanupMigrationVersion).Scan(&legacyCleaned); err != nil {
		return result, err
	}
	if legacyCleaned {
		result.LegacyNotice = "旧风险事件与旧摘要已按批准迁移清理；当前风险只使用现行有效信号，历史证据单独展示。"
	} else {
		result.LegacyNotice = "旧数据清理尚未执行；旧事件被隔离为历史数据，不参与当前风险。"
	}
	var ipScore, deviceScore, compositeScore, ipSignals, deviceSignals, compositeSignals int
	if cfg.CurrentScoreEnabled {
		if err := r.refreshUserSummary(ctx, userID); err != nil {
			return result, err
		}
		err := r.db.QueryRowContext(ctx, `SELECT overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count FROM risk_identity_user_summaries WHERE user_id=$1`, userID).Scan(&result.OverallScore, &ipScore, &deviceScore, &compositeScore, &ipSignals, &deviceSignals, &compositeSignals)
		if !errors.Is(err, sql.ErrNoRows) && err != nil {
			return result, err
		}
	}
	associated := map[string]int{}
	var ipAssociated, deviceAssociated, compositeAssociated int
	if err := r.db.QueryRowContext(ctx, `WITH links AS (
 SELECT user_id,network_identity_id FROM risk_user_ip_links
 UNION SELECT user_id,network_identity_id FROM risk_identity_activity_daily WHERE network_identity_id>0 AND success_count>0
) SELECT COUNT(DISTINCT other.user_id) FROM links mine JOIN links other USING(network_identity_id) WHERE mine.user_id=$1 AND other.user_id<>$1`, userID).Scan(&ipAssociated); err != nil {
		return result, err
	}
	if err := r.db.QueryRowContext(ctx, `WITH links AS (
 SELECT link.user_id,link.device_identity_id FROM risk_user_device_links link JOIN risk_device_identities identity ON identity.id=link.device_identity_id WHERE identity.identity_kind IN ('browser_instance','api_client')
 UNION SELECT daily.user_id,daily.device_identity_id FROM risk_identity_activity_daily daily JOIN risk_device_identities identity ON identity.id=daily.device_identity_id WHERE daily.device_identity_id>0 AND daily.success_count>0 AND identity.identity_kind IN ('browser_instance','api_client')
) SELECT COUNT(DISTINCT other.user_id) FROM links mine JOIN links other USING(device_identity_id) WHERE mine.user_id=$1 AND other.user_id<>$1`, userID).Scan(&deviceAssociated); err != nil {
		return result, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT other.user_id) FROM risk_identity_events mine JOIN risk_identity_events other ON other.network_identity_id=mine.network_identity_id AND other.browser_identity_id=mine.browser_identity_id
WHERE mine.user_id=$1 AND other.user_id<>$1 AND mine.event_class='registration' AND other.event_class='registration' AND mine.outcome='success' AND other.outcome='success' AND mine.ip_quality_valid AND other.ip_quality_valid AND mine.device_quality_valid AND other.device_quality_valid AND mine.network_identity_id IS NOT NULL AND mine.browser_identity_id IS NOT NULL
AND ABS(EXTRACT(EPOCH FROM(other.occurred_at-mine.occurred_at)))<=COALESCE((SELECT window_seconds FROM risk_identity_rules WHERE code='v2_registration_composite_accounts'),600)`, userID).Scan(&compositeAssociated); err != nil {
		return result, err
	}
	associated["ip"], associated["device"], associated["composite"] = ipAssociated, deviceAssociated, compositeAssociated
	states, _, stateErr := r.qualityStates(ctx, cfg)
	if stateErr != nil {
		return result, stateErr
	}
	historicalScores := map[string]int{"ip": 0, "device": 0, "composite": 0}
	historicalCounts := map[string]int{"ip": 0, "device": 0, "composite": 0}
	if err := r.db.QueryRowContext(ctx, `WITH historical AS (
	 SELECT domain,score,rule_code FROM risk_identity_signals WHERE user_id=$1 AND domain IN ('ip','device','composite') AND score>0
	 UNION ALL SELECT domain,score,rule_code FROM risk_identity_signal_history WHERE user_id=$1 AND domain IN ('ip','device','composite') AND score>0
	) SELECT COALESCE(MAX(score),0),COUNT(*)::int FROM historical`, userID).Scan(&result.HistoricalMaxScore, &result.HistoricalSignalCount); err != nil {
		return result, err
	}
	for _, domain := range []string{"ip", "device", "composite"} {
		var maxScore, signalCount int
		if err := r.db.QueryRowContext(ctx, `WITH historical AS (
 SELECT domain,score,rule_code FROM risk_identity_signals WHERE user_id=$1 AND domain=$2 AND score>0
 UNION ALL SELECT domain,score,rule_code FROM risk_identity_signal_history WHERE user_id=$1 AND domain=$2 AND score>0
) SELECT COALESCE(MAX(score),0),COUNT(*)::int FROM historical`, userID, domain).Scan(&maxScore, &signalCount); err != nil {
			return result, err
		}
		historicalScores[domain], historicalCounts[domain] = maxScore, signalCount
	}
	signals := map[string][]IdentitySignalSummary{"ip": {}, "device": {}, "composite": {}}
	rows, signalErr := r.db.QueryContext(ctx, `SELECT domain,rule_code,rule_revision,signal_family,status,COALESCE(decision_id,''),score,evidence_count,evidence_snapshot,occurred_at,active_until FROM (
 SELECT signal.domain,signal.rule_code,signal.rule_revision,signal.signal_family,signal.status,signal.decision_id,signal.score,signal.evidence_count,signal.evidence_snapshot,signal.occurred_at,signal.active_until,ROW_NUMBER() OVER(PARTITION BY signal.domain ORDER BY signal.score DESC,signal.occurred_at DESC) row_number
 FROM risk_identity_signals signal
 JOIN risk_identity_rules rule ON rule.code=signal.rule_code
 JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE signal.user_id=$1 AND signal.domain IN ('ip','device','composite') AND signal.score>0 AND signal.status='active' AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW()) AND rule.enabled AND rule.mode='shadow' AND version.enabled
 ) ranked WHERE row_number<=20 ORDER BY domain,score DESC,occurred_at DESC`, userID)
	if signalErr != nil {
		return result, signalErr
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		var item IdentitySignalSummary
		var occurredAt time.Time
		var activeUntil sql.NullTime
		var evidence []byte
		if err := rows.Scan(&domain, &item.RuleCode, &item.RuleRevision, &item.SignalFamily, &item.Status, &item.DecisionID, &item.Score, &item.EvidenceCount, &evidence, &occurredAt, &activeUntil); err != nil {
			return result, err
		}
		item.EvidenceSnapshot = map[string]any{}
		if cfg.ExplainEnabled {
			_ = json.Unmarshal(evidence, &item.EvidenceSnapshot)
		}
		item.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
		if activeUntil.Valid {
			item.ActiveUntil = activeUntil.Time.UTC().Format(time.RFC3339Nano)
		}
		signals[domain] = append(signals[domain], item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for _, domain := range []struct {
		name  string
		score *int
		count *int
	}{
		{name: "ip", score: &ipScore, count: &ipSignals},
		{name: "device", score: &deviceScore, count: &deviceSignals},
		{name: "composite", score: &compositeScore, count: &compositeSignals},
	} {
		if !cfg.CurrentScoreEnabled || !cfg.RulesEnabled || states[domain.name] != "healthy" {
			*domain.score = 0
			*domain.count = 0
			signals[domain.name] = []IdentitySignalSummary{}
		}
	}
	result.OverallScore = max(ipScore, deviceScore, compositeScore)
	result.Domains = []IdentityDomainSummary{
		{Domain: "ip", State: states["ip"], Score: ipScore, SignalCount: ipSignals, AssociatedAccountCount: associated["ip"], Signals: signals["ip"], HistoricalMaxScore: historicalScores["ip"], HistoricalSignalCount: historicalCounts["ip"]},
		{Domain: "device", State: states["device"], Score: deviceScore, SignalCount: deviceSignals, AssociatedAccountCount: associated["device"], Signals: signals["device"], HistoricalMaxScore: historicalScores["device"], HistoricalSignalCount: historicalCounts["device"]},
		{Domain: "composite", State: states["composite"], Score: compositeScore, SignalCount: compositeSignals, AssociatedAccountCount: associated["composite"], Signals: signals["composite"], HistoricalMaxScore: historicalScores["composite"], HistoricalSignalCount: historicalCounts["composite"]},
	}
	return result, nil
}

func (r *SQLIdentityRepository) ListNetworks(ctx context.Context, userID int64, lookupKey string, limit, offset int) ([]NetworkIdentityRow, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `WITH links AS (
 SELECT network_identity_id,first_seen_at,last_seen_at,registration_success_count,login_success_count,api_success_count FROM risk_user_ip_links WHERE user_id=$1
 UNION ALL
 SELECT network_identity_id,MIN(first_seen_at),MAX(last_seen_at),0,0,SUM(success_count) FROM risk_identity_activity_daily WHERE user_id=$1 AND network_identity_id>0 GROUP BY network_identity_id
), grouped AS (
 SELECT network_identity_id,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at,SUM(registration_success_count) registration_success_count,SUM(login_success_count) login_success_count,SUM(api_success_count) api_success_count FROM links GROUP BY network_identity_id
)
SELECT COUNT(*) FROM grouped l JOIN risk_network_identities n ON n.id=l.network_identity_id WHERE ($2='' OR n.lookup_key=$2)`, userID, lookupKey).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `WITH links AS (
 SELECT network_identity_id,first_seen_at,last_seen_at,registration_success_count,login_success_count,api_success_count FROM risk_user_ip_links WHERE user_id=$1
 UNION ALL
 SELECT network_identity_id,MIN(first_seen_at),MAX(last_seen_at),0,0,SUM(success_count) FROM risk_identity_activity_daily WHERE user_id=$1 AND network_identity_id>0 GROUP BY network_identity_id
), grouped AS (
 SELECT network_identity_id,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at,SUM(registration_success_count) registration_success_count,SUM(login_success_count) login_success_count,SUM(api_success_count) api_success_count FROM links GROUP BY network_identity_id
)
SELECT n.id,n.lookup_key,n.ip_ciphertext,n.ip_nonce,n.encryption_key_id,n.ip_family,n.ip_source,n.is_public,n.country_code,n.region,n.city,n.asn,n.geo_source,n.geo_verified,COALESCE(label.label,''),COALESCE(label.reason,''),l.first_seen_at,l.last_seen_at,l.registration_success_count,l.login_success_count,l.api_success_count,
(SELECT COUNT(DISTINCT linked.user_id) FROM (
 SELECT user_id FROM risk_user_ip_links WHERE network_identity_id=n.id
 UNION ALL SELECT user_id FROM risk_identity_activity_daily WHERE network_identity_id=n.id
) linked)
FROM grouped l JOIN risk_network_identities n ON n.id=l.network_identity_id LEFT JOIN risk_shared_network_labels label ON label.network_identity_id=n.id WHERE ($2='' OR n.lookup_key=$2) ORDER BY l.last_seen_at DESC LIMIT $3 OFFSET $4`, userID, lookupKey, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []NetworkIdentityRow{}
	for rows.Next() {
		var item NetworkIdentityRow
		var first, last time.Time
		if err := rows.Scan(&item.ID, &item.LookupKey, &item.Ciphertext, &item.Nonce, &item.KeyID, &item.IPFamily, &item.IPSource, &item.Public, &item.CountryCode, &item.Region, &item.City, &item.ASN, &item.GeoSource, &item.GeoVerified, &item.NetworkLabel, &item.NetworkLabelReason, &first, &last, &item.RegistrationSuccesses, &item.LoginSuccesses, &item.APISuccesses, &item.AssociatedAccountCount); err != nil {
			return nil, 0, err
		}
		item.DataSource = strings.TrimSpace(item.GeoSource)
		if item.DataSource == "" {
			item.DataSource = "none"
		}
		switch {
		case item.GeoVerified:
			item.Availability = "available"
		case !item.Public:
			item.Availability = "not_evaluable"
			item.UnavailableReason = "non_public_address"
			item.UnavailableImpact = "location_not_used_for_risk"
		case item.GeoSource == "" || item.GeoSource == "none":
			item.Availability = "unavailable"
			item.UnavailableReason = "geo_source_not_configured"
			item.UnavailableImpact = "location_context_missing"
		default:
			item.Availability = "unavailable"
			item.UnavailableReason = "geo_lookup_unverified"
			item.UnavailableImpact = "location_context_missing"
		}
		item.FirstSeenAt = first.UTC().Format(time.RFC3339Nano)
		item.LastSeenAt = last.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLIdentityRepository) ListDevices(ctx context.Context, userID int64, limit, offset int) ([]DeviceIdentityRow, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
 SELECT device_identity_id FROM risk_user_device_links WHERE user_id=$1
 UNION SELECT device_identity_id FROM risk_identity_activity_daily WHERE user_id=$1 AND device_identity_id>0
) linked`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `WITH links AS (
 SELECT device_identity_id,first_seen_at,last_seen_at,registration_success_count,login_success_count,api_success_count FROM risk_user_device_links WHERE user_id=$1
 UNION ALL
 SELECT device_identity_id,MIN(first_seen_at),MAX(last_seen_at),0,0,SUM(success_count) FROM risk_identity_activity_daily WHERE user_id=$1 AND device_identity_id>0 GROUP BY device_identity_id
), grouped AS (
 SELECT device_identity_id,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at,SUM(registration_success_count) registration_success_count,SUM(login_success_count) login_success_count,SUM(api_success_count) api_success_count FROM links GROUP BY device_identity_id
)
SELECT d.id,d.identity_kind,d.lookup_key,d.browser_family,d.os_family,d.device_class,d.language_family,d.cookie_status,l.first_seen_at,l.last_seen_at,l.registration_success_count,l.login_success_count,l.api_success_count,
 (SELECT COUNT(DISTINCT network_identity_id) FROM (
   SELECT e.network_identity_id FROM risk_identity_events e WHERE e.user_id=$1 AND (e.browser_identity_id=d.id OR e.profile_identity_id=d.id OR e.api_client_identity_id=d.id)
   UNION ALL SELECT a.network_identity_id FROM risk_identity_activity_daily a WHERE a.user_id=$1 AND a.device_identity_id=d.id
 ) networks WHERE network_identity_id>0),
 CASE WHEN d.identity_kind='browser_profile' THEN 0 ELSE (SELECT COUNT(DISTINCT linked.user_id) FROM (
   SELECT user_id FROM risk_user_device_links WHERE device_identity_id=d.id
   UNION ALL SELECT user_id FROM risk_identity_activity_daily WHERE device_identity_id=d.id
 ) linked) END
 FROM grouped l JOIN risk_device_identities d ON d.id=l.device_identity_id ORDER BY l.last_seen_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []DeviceIdentityRow{}
	for rows.Next() {
		var item DeviceIdentityRow
		var first, last time.Time
		if err := rows.Scan(&item.ID, &item.Kind, &item.LookupKey, &item.BrowserFamily, &item.OSFamily, &item.DeviceClass, &item.LanguageFamily, &item.CookieStatus, &first, &last, &item.RegistrationSuccesses, &item.LoginSuccesses, &item.APISuccesses, &item.NetworkCount, &item.AssociatedAccountCount); err != nil {
			return nil, 0, err
		}
		item.DisplayCode, item.Confidence = identityDeviceDisplay(item.Kind, item.LookupKey)
		item.FirstSeenAt = first.UTC().Format(time.RFC3339Nano)
		item.LastSeenAt = last.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLIdentityRepository) ListAssociatedUsers(ctx context.Context, userID int64, limit, offset int) ([]AssociatedUserRow, int, error) {
	query := `WITH parameters AS (
 SELECT COALESCE((SELECT window_seconds FROM risk_identity_rules WHERE code='v2_registration_composite_accounts'),600)::int overlap_window
), ip_links AS (
 SELECT user_id,network_identity_id,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at FROM (
  SELECT user_id,network_identity_id,first_seen_at,last_seen_at FROM risk_user_ip_links
  UNION ALL
  SELECT user_id,network_identity_id,first_seen_at,last_seen_at FROM risk_identity_activity_daily WHERE network_identity_id>0 AND success_count>0
 ) source GROUP BY user_id,network_identity_id
), device_links AS (
 SELECT user_id,device_identity_id,identity_kind,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at FROM (
  SELECT link.user_id,link.device_identity_id,identity.identity_kind,link.first_seen_at,link.last_seen_at
  FROM risk_user_device_links link JOIN risk_device_identities identity ON identity.id=link.device_identity_id
  WHERE identity.identity_kind IN ('browser_instance','api_client')
  UNION ALL
  SELECT daily.user_id,daily.device_identity_id,identity.identity_kind,daily.first_seen_at,daily.last_seen_at
  FROM risk_identity_activity_daily daily JOIN risk_device_identities identity ON identity.id=daily.device_identity_id
  WHERE daily.device_identity_id>0 AND daily.success_count>0 AND identity.identity_kind IN ('browser_instance','api_client')
 ) source GROUP BY user_id,device_identity_id,identity_kind
), relations AS (
 SELECT other.user_id,COUNT(DISTINCT mine.network_identity_id)::int shared_ip,0::int shared_browser,0::int shared_api,BOOL_OR(COALESCE(label.label IN ('home','company','school','trusted_egress','mobile_cgnat'),FALSE)) known_shared,MIN(other.first_seen_at) first_seen,MAX(other.last_seen_at) last_seen
 FROM ip_links mine JOIN ip_links other USING(network_identity_id) LEFT JOIN risk_shared_network_labels label ON label.network_identity_id=mine.network_identity_id
 WHERE mine.user_id=$1 AND other.user_id<>$1 GROUP BY other.user_id
 UNION ALL
 SELECT other.user_id,0,COUNT(DISTINCT mine.device_identity_id) FILTER(WHERE mine.identity_kind='browser_instance')::int,COUNT(DISTINCT mine.device_identity_id) FILTER(WHERE mine.identity_kind='api_client')::int,FALSE,MIN(other.first_seen_at),MAX(other.last_seen_at)
 FROM device_links mine JOIN device_links other USING(device_identity_id,identity_kind)
 WHERE mine.user_id=$1 AND other.user_id<>$1 GROUP BY other.user_id
), grouped AS (
 SELECT user_id,SUM(shared_ip)::int shared_ip,SUM(shared_browser)::int shared_browser,SUM(shared_api)::int shared_api,BOOL_OR(known_shared) known_shared,MIN(first_seen) first_seen,MAX(last_seen) last_seen FROM relations GROUP BY user_id
), event_pairs AS (
 SELECT other.user_id,mine.id mine_event_id,other.id other_event_id,LEAST(mine.occurred_at,other.occurred_at) overlap_start,GREATEST(mine.occurred_at,other.occurred_at) overlap_end,parameters.overlap_window
 FROM risk_identity_events mine JOIN risk_identity_events other ON other.network_identity_id=mine.network_identity_id AND other.browser_identity_id=mine.browser_identity_id CROSS JOIN parameters
 WHERE mine.user_id=$1 AND other.user_id<>$1 AND mine.event_class='registration' AND other.event_class='registration' AND mine.outcome='success' AND other.outcome='success'
 AND mine.ip_quality_valid AND other.ip_quality_valid AND mine.device_quality_valid AND other.device_quality_valid
 AND mine.network_identity_id IS NOT NULL AND mine.browser_identity_id IS NOT NULL AND ABS(EXTRACT(EPOCH FROM(other.occurred_at-mine.occurred_at)))<=parameters.overlap_window
), overlap AS (
 SELECT user_id,COUNT(DISTINCT other_event_id)::int cooccur,MIN(overlap_start) overlap_start,MAX(overlap_end) overlap_end,MAX(overlap_window)::int overlap_window,array_agg(DISTINCT mine_event_id)||array_agg(DISTINCT other_event_id) source_ids
 FROM event_pairs GROUP BY user_id
), relation_sources AS (
 SELECT other.user_id,array_agg(DISTINCT mine.id)||array_agg(DISTINCT other.id) source_ids
 FROM risk_identity_events mine JOIN risk_identity_events other ON (
  (mine.ip_quality_valid AND other.ip_quality_valid AND mine.network_identity_id IS NOT NULL AND other.network_identity_id=mine.network_identity_id)
  OR (mine.device_quality_valid AND other.device_quality_valid AND mine.browser_identity_id IS NOT NULL AND other.browser_identity_id=mine.browser_identity_id)
 )
 WHERE mine.user_id=$1 AND other.user_id<>$1 AND mine.outcome='success' AND other.outcome='success'
 GROUP BY other.user_id
)
SELECT grouped.user_id,grouped.shared_ip,grouped.shared_browser,grouped.shared_api,grouped.known_shared,COALESCE(overlap.cooccur,0),grouped.first_seen,grouped.last_seen,overlap.overlap_start,overlap.overlap_end,COALESCE(overlap.overlap_window,0),COALESCE(relation_sources.source_ids,'{}'::bigint[])||COALESCE(overlap.source_ids,'{}'::bigint[]),COUNT(*) OVER()
FROM grouped LEFT JOIN overlap USING(user_id) LEFT JOIN relation_sources USING(user_id)
ORDER BY COALESCE(overlap.cooccur,0) DESC,grouped.shared_browser DESC,grouped.shared_ip DESC,grouped.last_seen DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AssociatedUserRow{}
	total := 0
	for rows.Next() {
		var item AssociatedUserRow
		var first, last time.Time
		var overlapStart, overlapEnd sql.NullTime
		var sourceIDs pq.Int64Array
		var knownSharedNetwork bool
		if err := rows.Scan(&item.UserID, &item.SharedNetworkCount, &item.SharedBrowserInstanceCount, &item.SharedAPIClientCount, &knownSharedNetwork, &item.CooccurringEvidenceCount, &first, &last, &overlapStart, &overlapEnd, &item.EvidenceWindowSeconds, &sourceIDs, &total); err != nil {
			return nil, 0, err
		}
		item.SharedDeviceCount = item.SharedBrowserInstanceCount + item.SharedAPIClientCount
		item.FirstSeenAt = first.UTC().Format(time.RFC3339Nano)
		item.LastSeenAt = last.UTC().Format(time.RFC3339Nano)
		item.SourceEventIDs = uniqueInt64s([]int64(sourceIDs))
		if overlapStart.Valid && overlapEnd.Valid {
			item.Concurrent = true
			item.OverlapStart = overlapStart.Time.UTC().Format(time.RFC3339Nano)
			item.OverlapEnd = overlapEnd.Time.UTC().Format(time.RFC3339Nano)
		}
		switch {
		case item.CooccurringEvidenceCount > 0:
			item.Relation = "composite"
			item.EvidenceStrength = "high"
			item.Limitations = []string{"same_ip_and_browser_instance_within_window", "shared_context_requires_manual_review"}
		case item.SharedBrowserInstanceCount > 0:
			item.Relation = "browser_instance"
			item.EvidenceStrength = "medium_high"
			item.Limitations = []string{"historical_relationship_not_proof_of_concurrency", "browser_instance_can_be_shared"}
		case item.SharedNetworkCount > 0 && item.SharedAPIClientCount > 0:
			item.Relation = "multi_domain"
			item.EvidenceStrength = "weak"
			item.Limitations = []string{"api_client_is_observation_only", "historical_relationship_not_proof_of_concurrency"}
		case item.SharedNetworkCount > 0:
			item.Relation = "ip"
			item.EvidenceStrength = "weak"
			item.Limitations = []string{"ip_only_is_weak_evidence", "historical_relationship_not_proof_of_concurrency"}
		default:
			item.Relation = "api_client"
			item.EvidenceStrength = "observation"
			item.Limitations = []string{"api_client_is_observation_only", "daily_aggregate_has_no_event_ids", "historical_relationship_not_proof_of_concurrency"}
		}
		if item.SharedAPIClientCount > 0 && item.Relation != "api_client" {
			item.Limitations = append(item.Limitations, "daily_aggregate_has_no_event_ids")
		}
		if knownSharedNetwork {
			item.Limitations = append(item.Limitations, "known_shared_network_label")
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func identityDeviceDisplay(kind, lookupKey string) (string, string) {
	prefix, confidence := "D", "low"
	switch kind {
	case "browser_instance":
		prefix, confidence = "B", "medium_high"
	case "api_client":
		prefix, confidence = "K", "high"
	case "browser_profile":
		prefix = "F"
	}
	digest := strings.ToUpper(strings.TrimSpace(lookupKey))
	if len(digest) > 6 {
		digest = digest[:6]
	}
	return prefix + "-" + digest, confidence
}

func (r *SQLIdentityRepository) ListSummaries(ctx context.Context, userIDs []int64, cfg IdentityConfig, protector *IdentityProtector) ([]IdentityListSummary, error) {
	if len(userIDs) == 0 {
		return []IdentityListSummary{}, nil
	}
	if len(userIDs) > 100 {
		userIDs = userIDs[:100]
	}
	states, _, err := r.qualityStates(ctx, cfg)
	if err != nil {
		return nil, err
	}
	qualityState := combinedIdentityState(states)
	healthyDomains := make([]string, 0, 3)
	for _, domain := range []string{"ip", "device", "composite"} {
		if cfg.CurrentScoreEnabled && cfg.RulesEnabled && states[domain] == "healthy" {
			healthyDomains = append(healthyDomains, domain)
		}
	}
	rows, err := r.db.QueryContext(ctx, `WITH requested AS (SELECT DISTINCT UNNEST($1::bigint[]) user_id),
ip_links AS (
 SELECT user_id,network_identity_id,MAX(last_seen_at) last_seen_at FROM (
  SELECT user_id,network_identity_id,last_seen_at FROM risk_user_ip_links
  UNION ALL SELECT user_id,network_identity_id,last_seen_at FROM risk_identity_activity_daily WHERE network_identity_id>0 AND success_count>0
 ) source GROUP BY user_id,network_identity_id
), device_links AS (
 SELECT user_id,device_identity_id FROM risk_user_device_links
 UNION SELECT user_id,device_identity_id FROM risk_identity_activity_daily WHERE device_identity_id>0 AND success_count>0
),
latest_network AS (
 SELECT DISTINCT ON (l.user_id) l.user_id,n.lookup_key,n.ip_ciphertext,n.ip_nonce,n.encryption_key_id,n.country_code,n.region
 FROM ip_links l JOIN risk_network_identities n ON n.id=l.network_identity_id JOIN requested r USING(user_id)
 ORDER BY l.user_id,l.last_seen_at DESC
), device_counts AS (
 SELECT l.user_id,COUNT(*) FILTER(WHERE d.identity_kind='browser_instance')::int browsers,COUNT(*) FILTER(WHERE d.identity_kind='api_client')::int api_clients
 FROM device_links l JOIN risk_device_identities d ON d.id=l.device_identity_id JOIN requested r USING(user_id) GROUP BY l.user_id
), associated AS (
 SELECT user_id,COUNT(DISTINCT other_user_id)::int associated_accounts FROM (
  SELECT mine.user_id,other.user_id other_user_id FROM ip_links mine JOIN ip_links other USING(network_identity_id) JOIN requested r ON r.user_id=mine.user_id WHERE other.user_id<>mine.user_id
  UNION ALL
  SELECT mine.user_id,other.user_id FROM device_links mine JOIN device_links other USING(device_identity_id) JOIN risk_device_identities identity ON identity.id=mine.device_identity_id JOIN requested r ON r.user_id=mine.user_id WHERE other.user_id<>mine.user_id AND identity.identity_kind IN ('browser_instance','api_client')
 ) relations GROUP BY user_id
), active_rules AS (
 SELECT s.user_id,COUNT(DISTINCT s.rule_code)::int active_rules FROM risk_identity_signals s JOIN risk_identity_rules rule ON rule.code=s.rule_code JOIN risk_rule_versions version ON version.id=s.rule_version_id AND version.rule_kind='identity' AND version.rule_code=s.rule_code AND version.revision=s.rule_revision JOIN requested r USING(user_id)
 WHERE s.domain=ANY($2::text[]) AND s.score>0 AND s.status='active' AND s.active_from<=NOW() AND (s.active_until IS NULL OR s.active_until>NOW()) AND rule.enabled AND rule.mode='shadow' AND version.enabled GROUP BY s.user_id
)
SELECT requested.user_id,latest_network.user_id IS NOT NULL,COALESCE(latest_network.lookup_key,''),latest_network.ip_ciphertext,latest_network.ip_nonce,COALESCE(latest_network.encryption_key_id,''),COALESCE(latest_network.country_code,''),COALESCE(latest_network.region,''),COALESCE(device_counts.browsers,0),COALESCE(device_counts.api_clients,0),COALESCE(associated.associated_accounts,0),COALESCE(active_rules.active_rules,0)
FROM requested LEFT JOIN latest_network USING(user_id) LEFT JOIN device_counts USING(user_id) LEFT JOIN associated USING(user_id) LEFT JOIN active_rules USING(user_id) ORDER BY requested.user_id`, pq.Array(userIDs), pq.Array(healthyDomains))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]IdentityListSummary, 0, len(userIDs))
	for rows.Next() {
		var item IdentityListSummary
		if err := rows.Scan(&item.UserID, &item.HasIdentity, &item.LookupKey, &item.Ciphertext, &item.Nonce, &item.KeyID, &item.CountryCode, &item.Region, &item.BrowserInstanceCount, &item.APIClientCount, &item.AssociatedAccountCount, &item.ActiveRuleCount); err != nil {
			return nil, err
		}
		item.ActiveSignalCount = item.ActiveRuleCount
		item.QualityState = qualityState
		if item.HasIdentity {
			plainIP, decryptErr := protector.DecryptIP(item.Ciphertext, item.Nonce, item.LookupKey, item.KeyID)
			if decryptErr != nil {
				return nil, errors.New("identity key unavailable")
			}
			item.LatestIP = maskIdentityIP(plainIP)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func combinedIdentityState(states map[string]string) string {
	result := "disabled"
	for _, domain := range []string{"ip", "device", "composite"} {
		switch states[domain] {
		case "paused":
			return "paused"
		case "not_evaluable":
			if result != "paused" {
				result = "not_evaluable"
			}
		case "degraded":
			if result != "not_evaluable" {
				result = "degraded"
			}
		case "healthy":
			if result == "disabled" {
				result = "healthy"
			}
		}
	}
	return result
}

func maskIdentityIP(raw string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	address = address.Unmap()
	bits := 64
	if address.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(address, bits).Masked().String()
}

func (r *SQLIdentityRepository) Health(ctx context.Context, cfg IdentityConfig) (IdentityHealth, error) {
	result := IdentityHealth{Enabled: cfg.Enabled, AdminEnabled: cfg.AdminEnabled, Mode: identityMode(cfg), Schema: "v2", KeyID: cfg.EncryptionKeyID, GeoSource: cfg.GeoSource, Domains: map[string]string{}, Quality24H: map[string]any{}, Delivery: map[string]any{}, Processing: map[string]any{}, Features: map[string]bool{
		"current_score":         cfg.CurrentScoreEnabled,
		"cases":                 cfg.CasesEnabled,
		"explain":               cfg.ExplainEnabled,
		"delivery":              cfg.DeliveryEnabled,
		"composite_enforcement": cfg.CompositeEnforcementEnabled,
	}}
	if !cfg.ShadowUntil.IsZero() {
		result.ShadowUntil = cfg.ShadowUntil.Format(time.RFC3339)
	}
	states, counts, err := r.qualityStates(ctx, cfg)
	if err != nil {
		return result, err
	}
	result.Domains = states
	qualityConfig := cfg
	qualityConfig.IPDomainEnabled = true
	qualityConfig.DeviceDomainEnabled = true
	qualityConfig.CompositeDomainEnabled = true
	result.QualityDomains = identityDomainStates(qualityConfig, counts)
	result.Quality24H = map[string]any{"events": counts.Total, "valid_ip": counts.ValidIP, "valid_device": counts.ValidDevice, "linked_users": counts.LinkedUsers, "max_network_users": counts.MaxNetworkUsers, "minimum_events": cfg.QualityMinEvents, "minimum_coverage_percent": cfg.QualityMinCoverage, "maximum_ip_share_percent": cfg.QualityMaxIPShare}
	result.Processing = map[string]any{"pending": counts.ProcessingPending, "retry": counts.ProcessingRetry, "failed": counts.ProcessingFailed}
	result.Delivery = map[string]any{"enabled": cfg.DeliveryEnabled, "sources": counts.DeliverySources, "gap_sources": counts.DeliveryGapSources, "stale_sources": counts.DeliveryStaleSources, "queue_depth": counts.DeliveryQueueDepth, "dropped": counts.DeliveryDropped, "failed": counts.DeliveryFailed}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE enabled AND mode='shadow' AND active_from<=NOW() AND (active_until IS NULL OR active_until>NOW())) FROM risk_identity_rules`).Scan(&result.ConfiguredRuleCount, &result.ProspectiveRuleCount); err != nil {
		return result, err
	}
	if cfg.RulesEnabled {
		result.EffectiveRuleCount = result.ProspectiveRuleCount
	}
	return result, nil
}

func (r *SQLIdentityRepository) qualityStates(ctx context.Context, cfg IdentityConfig) (map[string]string, identityQualityCounts, error) {
	return queryIdentityQualityStates(ctx, r.db, cfg)
}

func queryIdentityQualityStates(ctx context.Context, queryer identityQueryer, cfg IdentityConfig) (map[string]string, identityQualityCounts, error) {
	var counts identityQualityCounts
	err := queryer.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE ip_quality_valid),COUNT(*) FILTER(WHERE device_quality_valid) FROM risk_identity_events WHERE occurred_at>=NOW()-interval '24 hours'`).Scan(&counts.Total, &counts.ValidIP, &counts.ValidDevice)
	if err == nil {
		err = queryer.QueryRowContext(ctx, `SELECT COALESCE((SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE occurred_at>=NOW()-interval '24 hours' AND network_identity_id IS NOT NULL),0),COALESCE(MAX(user_count),0) FROM (SELECT event.network_identity_id,COUNT(DISTINCT event.user_id) user_count FROM risk_identity_events event LEFT JOIN risk_shared_network_labels label ON label.network_identity_id=event.network_identity_id WHERE event.occurred_at>=NOW()-interval '24 hours' AND event.network_identity_id IS NOT NULL AND (label.label IS NULL OR label.label NOT IN ('home','company','school','trusted_egress','mobile_cgnat')) GROUP BY event.network_identity_id) grouped`).Scan(&counts.LinkedUsers, &counts.MaxNetworkUsers)
	}
	if err == nil {
		err = queryer.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE status IN ('pending','processing')),COUNT(*) FILTER(WHERE status='retry'),COUNT(*) FILTER(WHERE status='failed') FROM risk_signal_processing_jobs`).Scan(&counts.ProcessingPending, &counts.ProcessingRetry, &counts.ProcessingFailed)
	}
	if err == nil {
		err = queryer.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE gap_until>NOW()),COUNT(*) FILTER(WHERE generated_at<NOW()-interval '2 minutes'),COALESCE(SUM(queue_depth),0),COALESCE(SUM(dropped),0),COALESCE(SUM(failed),0) FROM risk_delivery_watermarks`).Scan(&counts.DeliverySources, &counts.DeliveryGapSources, &counts.DeliveryStaleSources, &counts.DeliveryQueueDepth, &counts.DeliveryDropped, &counts.DeliveryFailed)
	}
	return identityDomainStates(cfg, counts), counts, err
}

func (r *SQLIdentityRepository) Rebuild(ctx context.Context, actorID int64, dryRun bool, requestedApprovedDryRunID int64, cfg IdentityConfig) (RebuildResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RebuildResult{}, err
	}
	defer tx.Rollback()
	approvedDryRunID := int64(0)
	if !dryRun {
		if requestedApprovedDryRunID <= 0 {
			return RebuildResult{}, errors.New("历史回放写入缺少有效预检")
		}
		if !cfg.RulesEnabled {
			return RebuildResult{}, errors.New("identity rules are not enabled")
		}
		if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_identity_events, risk_identity_rules IN SHARE MODE`); err != nil {
			return RebuildResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_identity_signals IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return RebuildResult{}, err
		}
		var shadowUntil time.Time
		if err := tx.QueryRowContext(ctx, `SELECT shadow_until FROM risk_identity_shadow_activation WHERE singleton=1`).Scan(&shadowUntil); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RebuildResult{}, errors.New("identity Shadow activation has not started")
			}
			return RebuildResult{}, err
		}
		effectiveShadowUntil := shadowUntil.UTC()
		if cfg.ShadowUntil.After(effectiveShadowUntil) {
			effectiveShadowUntil = cfg.ShadowUntil.UTC()
		}
		if time.Now().UTC().Before(effectiveShadowUntil) {
			return RebuildResult{}, fmt.Errorf("identity Shadow period is active until %s", effectiveShadowUntil.Format(time.RFC3339))
		}
		var dryRunID, evidenceHighWater, labelHighWater int64
		var dryRunCompleted time.Time
		var ruleWatermark []byte
		if err := tx.QueryRowContext(ctx, `SELECT id,completed_at,evidence_high_water,label_high_water,rule_watermark FROM risk_identity_rebuild_jobs
WHERE id=$2 AND dry_run=TRUE AND status='completed' AND requested_by=$1 AND completed_at IS NOT NULL`, actorID, requestedApprovedDryRunID).Scan(&dryRunID, &dryRunCompleted, &evidenceHighWater, &labelHighWater, &ruleWatermark); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RebuildResult{}, errors.New("所选预检不存在、未完成或不属于当前管理员")
			}
			return RebuildResult{}, err
		}
		if age := time.Since(dryRunCompleted.UTC()); age < 0 || age > 30*time.Minute {
			return RebuildResult{}, fmt.Errorf("预检 %d 已超过 30 分钟有效期", dryRunID)
		}
		var matches bool
		if err := tx.QueryRowContext(ctx, `SELECT
 COALESCE((SELECT MAX(id) FROM risk_identity_events),0)=$1
 AND COALESCE((SELECT MAX(id) FROM risk_shared_network_label_history),0)=$2
 AND COALESCE((SELECT jsonb_object_agg(code,revision ORDER BY code) FROM risk_identity_rules),'{}'::jsonb)=$3::jsonb`, evidenceHighWater, labelHighWater, ruleWatermark).Scan(&matches); err != nil {
			return RebuildResult{}, err
		}
		if !matches {
			return RebuildResult{}, fmt.Errorf("预检 %d 完成后身份依据、共享网络标签或规则已变化", dryRunID)
		}
		approvedDryRunID = dryRunID
	}
	states, _, err := queryIdentityQualityStates(ctx, tx, cfg)
	if err != nil {
		return RebuildResult{}, err
	}
	if !dryRun {
		if !cfg.CurrentScoreEnabled {
			return RebuildResult{}, errors.New("current identity score reading is disabled")
		}
		for _, domain := range []string{"ip", "device", "composite"} {
			if identityRuleDomainEnabled(cfg, domain) && states[domain] != "healthy" {
				return RebuildResult{}, fmt.Errorf("identity domain %s is %s", domain, states[domain])
			}
		}
	}
	result := RebuildResult{DryRun: dryRun, Status: "completed", RuleHits: map[string]int64{}, RuleWatermark: map[string]int{}, SampleUserIDs: []int64{}, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), ApprovedDryRunID: approvedDryRunID}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM risk_identity_events`).Scan(&result.EvidenceHighWater); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM risk_shared_network_label_history`).Scan(&result.LabelHighWater); err != nil {
		return result, err
	}
	ruleWatermarkRows, watermarkErr := tx.QueryContext(ctx, `SELECT code,revision FROM risk_identity_rules ORDER BY code`)
	if watermarkErr != nil {
		return result, watermarkErr
	}
	for ruleWatermarkRows.Next() {
		var code string
		var revision int
		if err = ruleWatermarkRows.Scan(&code, &revision); err != nil {
			ruleWatermarkRows.Close()
			return result, err
		}
		result.RuleWatermark[code] = revision
	}
	if err = ruleWatermarkRows.Close(); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_subjects WHERE risk_type='api_request' OR reason ILIKE '%API 请求观察%'`).Scan(&result.LegacyAPISubjects); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id),COUNT(*) FROM risk_identity_signals WHERE domain IN ('ip','device','composite')`).Scan(&result.CurrentSignalUsers, &result.CurrentSignals); err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE identity_rebuild_candidates ON COMMIT DROP AS
WITH rules AS (
 SELECT rule.code,rule.domain,rule.signal_family,rule.subject_kind,rule.window_seconds,rule.threshold,rule.score,rule.revision,rule.active_from,rule.active_until,version.id rule_version_id,version.rule_snapshot
 FROM risk_identity_rules rule JOIN risk_rule_versions version ON version.rule_kind='identity' AND version.rule_code=rule.code AND version.revision=rule.revision
 WHERE rule.enabled AND rule.mode='shadow' AND rule.active_from<=NOW() AND (rule.active_until IS NULL OR rule.active_until>NOW())
   AND (($1 AND rule.domain='ip') OR ($2 AND rule.domain='device') OR ($3 AND rule.domain='composite'))
), anchors AS (
 SELECT id,user_id,network_identity_id,browser_identity_id,api_client_identity_id,ip_quality_valid,device_quality_valid,occurred_at
 FROM risk_identity_events
 WHERE event_class='registration' AND outcome='success' AND user_id>0
)
SELECT anchor.id event_id,anchor.user_id,rule.domain,rule.code rule_code,rule.rule_version_id,rule.revision rule_revision,rule.signal_family,rule.score,counts.evidence_count,
 anchor.network_identity_id,CASE rule.subject_kind WHEN 'browser_instance' THEN anchor.browser_identity_id WHEN 'api_client' THEN anchor.api_client_identity_id WHEN 'ip_browser' THEN anchor.browser_identity_id ELSE NULL END device_identity_id,
	jsonb_build_object('evidence_count',counts.evidence_count,'window_seconds',rule.window_seconds,'source_event_id',anchor.id,'network_identity_id',anchor.network_identity_id,'device_identity_id',CASE rule.subject_kind WHEN 'browser_instance' THEN anchor.browser_identity_id WHEN 'api_client' THEN anchor.api_client_identity_id WHEN 'ip_browser' THEN anchor.browser_identity_id ELSE NULL END,'rule_revision',rule.revision,'signal_family',rule.signal_family,'same_event_pair',rule.subject_kind='ip_browser','rule_snapshot',rule.rule_snapshot) evidence_snapshot,
	'rebuild:'||anchor.id||':'||rule.code||':'||rule.revision decision_id,anchor.occurred_at active_from,anchor.occurred_at+(rule.window_seconds*interval '1 second') active_until,
	CASE WHEN anchor.occurred_at+(rule.window_seconds*interval '1 second')<=NOW() THEN 'expired'
	 WHEN rule.domain IN ('ip','composite') AND EXISTS(SELECT 1 FROM risk_shared_network_labels label WHERE label.network_identity_id=anchor.network_identity_id AND label.label IN ('home','company','school','trusted_egress','mobile_cgnat')) THEN 'resolved'
	 ELSE 'active' END status,
	(rule.domain IN ('ip','composite') AND anchor.occurred_at+(rule.window_seconds*interval '1 second')>NOW() AND EXISTS(SELECT 1 FROM risk_shared_network_labels label WHERE label.network_identity_id=anchor.network_identity_id AND label.label IN ('home','company','school','trusted_egress','mobile_cgnat'))) resolved_by_shared_network,
	anchor.occurred_at
FROM anchors anchor CROSS JOIN rules rule
CROSS JOIN LATERAL (
 SELECT COUNT(DISTINCT evidence.user_id)::int evidence_count
 FROM risk_identity_events evidence
 WHERE evidence.event_class='registration' AND evidence.outcome='success' AND evidence.user_id>0
   AND evidence.occurred_at BETWEEN anchor.occurred_at-(rule.window_seconds*interval '1 second') AND anchor.occurred_at
   AND CASE rule.subject_kind
	     WHEN 'ip' THEN anchor.ip_quality_valid AND anchor.network_identity_id IS NOT NULL AND evidence.ip_quality_valid AND evidence.network_identity_id=anchor.network_identity_id
     WHEN 'browser_instance' THEN anchor.device_quality_valid AND anchor.browser_identity_id IS NOT NULL AND evidence.device_quality_valid AND evidence.browser_identity_id=anchor.browser_identity_id
     WHEN 'api_client' THEN anchor.api_client_identity_id IS NOT NULL AND evidence.api_client_identity_id=anchor.api_client_identity_id
     WHEN 'ip_browser' THEN anchor.ip_quality_valid AND anchor.device_quality_valid AND anchor.network_identity_id IS NOT NULL AND anchor.browser_identity_id IS NOT NULL AND evidence.ip_quality_valid AND evidence.device_quality_valid AND evidence.network_identity_id=anchor.network_identity_id AND evidence.browser_identity_id=anchor.browser_identity_id
     ELSE FALSE
   END
) counts
WHERE counts.evidence_count>=rule.threshold`, cfg.RulesEnabled && states["ip"] == "healthy" && cfg.IPCollectionEnabled && cfg.IPDomainEnabled, cfg.RulesEnabled && states["device"] == "healthy" && cfg.DeviceCollectionEnabled && cfg.DeviceDomainEnabled, cfg.RulesEnabled && states["composite"] == "healthy" && cfg.CompositeDomainEnabled)
	if err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id),COUNT(*) FROM identity_rebuild_candidates`).Scan(&result.V2SignalUsers, &result.V2Signals); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT rule_code,COUNT(DISTINCT user_id) FROM identity_rebuild_candidates GROUP BY rule_code ORDER BY rule_code`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var code string
		var count int64
		if err = rows.Scan(&code, &count); err != nil {
			rows.Close()
			return result, err
		}
		result.RuleHits[code] = count
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT user_id FROM (SELECT DISTINCT user_id FROM identity_rebuild_candidates) candidates ORDER BY md5(user_id::text),user_id LIMIT 10`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			rows.Close()
			return result, err
		}
		result.SampleUserIDs = append(result.SampleUserIDs, userID)
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `WITH current_families AS (
 SELECT signal.user_id,signal.domain,signal.signal_family,MAX(signal.score) score FROM risk_identity_signals signal JOIN risk_identity_rules rule ON rule.code=signal.rule_code JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE signal.domain IN ('ip','device','composite') AND signal.score>0 AND signal.status='active' AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW())
 AND rule.enabled AND rule.mode='shadow' AND version.enabled GROUP BY signal.user_id,signal.domain,signal.signal_family
), desired_families AS (
 SELECT user_id,domain,signal_family,MAX(score) score FROM identity_rebuild_candidates WHERE status='active' AND score>0 GROUP BY user_id,domain,signal_family
), current_summary AS (
 SELECT user_id,MAX(score) FILTER(WHERE domain='ip') ip_score,MAX(score) FILTER(WHERE domain='device') device_score,MAX(score) FILTER(WHERE domain='composite') composite_score,COUNT(*) FILTER(WHERE domain='ip') ip_count,COUNT(*) FILTER(WHERE domain='device') device_count,COUNT(*) FILTER(WHERE domain='composite') composite_count FROM current_families GROUP BY user_id
), desired_summary AS (
 SELECT user_id,MAX(score) FILTER(WHERE domain='ip') ip_score,MAX(score) FILTER(WHERE domain='device') device_score,MAX(score) FILTER(WHERE domain='composite') composite_score,COUNT(*) FILTER(WHERE domain='ip') ip_count,COUNT(*) FILTER(WHERE domain='device') device_count,COUNT(*) FILTER(WHERE domain='composite') composite_count FROM desired_families GROUP BY user_id
)
SELECT COUNT(*) FROM current_summary current FULL OUTER JOIN desired_summary desired USING(user_id)
WHERE ROW(current.ip_score,current.device_score,current.composite_score,current.ip_count,current.device_count,current.composite_count) IS DISTINCT FROM ROW(desired.ip_score,desired.device_score,desired.composite_score,desired.ip_count,desired.device_count,desired.composite_count)`).Scan(&result.ChangedSubjects); err != nil {
		return result, err
	}
	result.ChangedSubjects += result.LegacyAPISubjects
	ruleHits, _ := json.Marshal(result.RuleHits)
	sampleUsers, _ := json.Marshal(result.SampleUserIDs)
	ruleWatermark, _ := json.Marshal(result.RuleWatermark)
	var completedAt time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO risk_identity_rebuild_jobs(dry_run,status,requested_by,legacy_api_subjects,current_signal_users,v2_signal_users,current_signals,v2_signals,changed_subjects,rule_hits,sample_user_ids,evidence_high_water,label_high_water,rule_watermark,approved_dry_run_id,completed_at) VALUES($1,'completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NOW()) RETURNING id,completed_at`, dryRun, actorID, result.LegacyAPISubjects, result.CurrentSignalUsers, result.V2SignalUsers, result.CurrentSignals, result.V2Signals, result.ChangedSubjects, ruleHits, sampleUsers, result.EvidenceHighWater, result.LabelHighWater, ruleWatermark, nullablePositiveInt64(result.ApprovedDryRunID)).Scan(&result.ID, &completedAt)
	if err != nil {
		return result, err
	}
	result.CompletedAt = completedAt.UTC().Format(time.RFC3339Nano)
	if !dryRun {
		if _, err = tx.ExecContext(ctx, `WITH affected AS (
  SELECT user_id FROM risk_subjects WHERE risk_type='api_request' OR reason ILIKE '%API 请求观察%'
), replacement AS (
  SELECT DISTINCT ON (e.user_id) e.user_id,e.username_snapshot,e.email_hash,e.account_status_snapshot,e.risk_type,e.risk_level,e.score,e.reason,e.decision,e.occurred_at,
    (SELECT COUNT(*)::int FROM risk_events x WHERE x.user_id=e.user_id AND x.event_type<>'api_request') event_count,
    (SELECT COUNT(DISTINCT NULLIF(x.ip_hash,''))::int FROM risk_events x WHERE x.user_id=e.user_id AND x.event_type<>'api_request') ip_count,
    (SELECT COUNT(DISTINCT NULLIF(x.device_hash,''))::int FROM risk_events x WHERE x.user_id=e.user_id AND x.event_type<>'api_request') device_count
  FROM risk_events e JOIN affected a USING(user_id)
  WHERE e.event_type<>'api_request' ORDER BY e.user_id,e.occurred_at DESC,e.id DESC
)
UPDATE risk_subjects s SET username=r.username_snapshot,email_hash=r.email_hash,account_status=r.account_status_snapshot,risk_type=r.risk_type,risk_level=r.risk_level,score=r.score,reason=r.reason,event_count=r.event_count,ip_count=r.ip_count,device_count=r.device_count,last_action=r.decision,last_event_at=r.occurred_at,updated_at=NOW() FROM replacement r WHERE s.user_id=r.user_id`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM risk_subjects s WHERE (s.risk_type='api_request' OR s.reason ILIKE '%API 请求观察%') AND NOT EXISTS(SELECT 1 FROM risk_events e WHERE e.user_id=s.user_id AND e.event_type<>'api_request')`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_signal_history(original_signal_id,event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,evidence_snapshot,decision_id,status,active_from,active_until,occurred_at,original_created_at)
SELECT id,event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,evidence_snapshot,decision_id,'superseded',active_from,active_until,occurred_at,created_at FROM risk_identity_signals
WHERE domain IN ('ip','device','composite')
ON CONFLICT(original_signal_id) DO NOTHING`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE risk_decisions decision SET status='superseded',current_score=0
WHERE decision.status='active' AND EXISTS(SELECT 1 FROM risk_identity_signals signal WHERE signal.decision_id=decision.decision_id AND signal.domain IN ('ip','device','composite'))`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM risk_identity_signals WHERE domain IN ('ip','device','composite')`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_decisions(decision_id,user_id,event_id,mode,status,current_score,historical_max_score,risk_level,evidence_snapshot,decided_at)
SELECT decision_id,user_id,event_id,'shadow',status,CASE WHEN status='active' THEN score ELSE 0 END,score,CASE WHEN score>=85 THEN 'critical' WHEN score>=60 THEN 'high' WHEN score>=30 THEN 'medium' WHEN score>0 THEN 'low' ELSE 'none' END,evidence_snapshot,occurred_at FROM identity_rebuild_candidates
ON CONFLICT(decision_id) DO UPDATE SET status=EXCLUDED.status,current_score=EXCLUDED.current_score,historical_max_score=GREATEST(risk_decisions.historical_max_score,EXCLUDED.historical_max_score)`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,evidence_snapshot,decision_id,status,resolved_by_shared_network,active_from,active_until,first_hit_at,last_hit_at,occurred_at)
SELECT event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score,evidence_count,TRUE,network_identity_id,device_identity_id,evidence_snapshot,evidence_snapshot,decision_id,status,resolved_by_shared_network,active_from,active_until,active_from,active_from,occurred_at FROM identity_rebuild_candidates`); err != nil {
			return result, err
		}
		if cfg.CasesEnabled {
			if _, err = tx.ExecContext(ctx, `WITH chosen AS (
 SELECT DISTINCT ON (user_id,signal_family) user_id,decision_id,signal_family,score,rule_code,occurred_at
 FROM identity_rebuild_candidates WHERE status='active' AND score>0
 ORDER BY user_id,signal_family,score DESC,occurred_at DESC,event_id DESC
)
INSERT INTO risk_review_cases(user_id,decision_id,signal_family,status,current_score,historical_max_score,primary_signal,evidence_strength,review_due_at,observation_goal,opened_at,last_hit_at)
SELECT user_id,decision_id,signal_family,CASE WHEN score>=60 THEN 'pending' ELSE 'observing' END,score,score,rule_code,
 CASE WHEN rule_code LIKE '%composite%' THEN 'high' WHEN score>=60 THEN 'medium_high' ELSE 'weak' END,
 CASE WHEN score<60 THEN NOW()+interval '24 hours' ELSE NULL END,
 CASE WHEN score<60 THEN 'Review whether the weak signal persists or escalates' ELSE '' END,
 occurred_at,occurred_at FROM chosen
ON CONFLICT(user_id,signal_family) WHERE status IN ('pending','in_review','observing') DO UPDATE SET
 status=CASE WHEN risk_review_cases.status='in_review' THEN 'in_review' WHEN risk_review_cases.status='observing' AND risk_review_cases.assignee_id>0 AND EXCLUDED.status='pending' THEN 'in_review' WHEN risk_review_cases.status='pending' OR EXCLUDED.status='pending' THEN 'pending' ELSE 'observing' END,
 assignee_id=CASE WHEN risk_review_cases.status IN ('observing','in_review') THEN risk_review_cases.assignee_id ELSE 0 END,
 review_due_at=CASE WHEN risk_review_cases.status='observing' AND EXCLUDED.status='observing' THEN COALESCE(risk_review_cases.review_due_at,EXCLUDED.review_due_at) ELSE NULL END,
 observation_goal=CASE WHEN risk_review_cases.status='observing' AND EXCLUDED.status='observing' THEN COALESCE(NULLIF(risk_review_cases.observation_goal,''),EXCLUDED.observation_goal) ELSE '' END,
 decision_id=EXCLUDED.decision_id,current_score=EXCLUDED.current_score,historical_max_score=GREATEST(risk_review_cases.historical_max_score,EXCLUDED.historical_max_score),primary_signal=EXCLUDED.primary_signal,evidence_strength=EXCLUDED.evidence_strength,last_hit_at=GREATEST(risk_review_cases.last_hit_at,EXCLUDED.last_hit_at),last_activity_at=NOW(),revision=risk_review_cases.revision+1,updated_at=NOW()`); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO risk_case_evidence(case_id,signal_id,evidence_snapshot,occurred_at)
SELECT case_row.id,signal.id,signal.evidence_snapshot,signal.occurred_at
FROM risk_identity_signals signal JOIN risk_review_cases case_row ON case_row.user_id=signal.user_id AND case_row.signal_family=signal.signal_family
WHERE signal.status='active' AND signal.score>0 AND case_row.status IN ('pending','in_review','observing')
  AND case_row.id=(SELECT candidate_case.id FROM risk_review_cases candidate_case WHERE candidate_case.user_id=signal.user_id AND candidate_case.signal_family=signal.signal_family AND candidate_case.status IN ('pending','in_review','observing') ORDER BY CASE candidate_case.status WHEN 'in_review' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END,candidate_case.id DESC LIMIT 1)
ON CONFLICT(case_id,signal_id) DO NOTHING`); err != nil {
				return result, err
			}
			if err = refreshAllIdentityReviewCases(ctx, tx); err != nil {
				return result, err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM risk_identity_user_summaries`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_user_summaries(user_id,overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count)
WITH families AS (SELECT user_id,domain,signal_family,MAX(score) score FROM risk_identity_signals WHERE score>0 AND status='active' AND active_from<=NOW() AND (active_until IS NULL OR active_until>NOW()) GROUP BY user_id,domain,signal_family)
SELECT user_id,GREATEST(COALESCE(MAX(score) FILTER(WHERE domain='ip'),0),COALESCE(MAX(score) FILTER(WHERE domain='device'),0),COALESCE(MAX(score) FILTER(WHERE domain='composite'),0)),COALESCE(MAX(score) FILTER(WHERE domain='ip'),0),COALESCE(MAX(score) FILTER(WHERE domain='device'),0),COALESCE(MAX(score) FILTER(WHERE domain='composite'),0),COUNT(*) FILTER(WHERE domain='ip'),COUNT(*) FILTER(WHERE domain='device'),COUNT(*) FILTER(WHERE domain='composite') FROM families GROUP BY user_id`); err != nil {
			return result, err
		}
	}
	err = tx.Commit()
	return result, err
}

func (r *SQLIdentityRepository) GetRebuild(ctx context.Context, id int64) (RebuildResult, error) {
	var result RebuildResult
	var started time.Time
	var completed sql.NullTime
	var ruleHits, sampleUsers, ruleWatermark []byte
	var approvedDryRunID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT id,dry_run,status,legacy_api_subjects,current_signal_users,v2_signal_users,current_signals,v2_signals,changed_subjects,rule_hits,sample_user_ids,evidence_high_water,label_high_water,rule_watermark,approved_dry_run_id,started_at,completed_at FROM risk_identity_rebuild_jobs WHERE id=$1`, id).Scan(&result.ID, &result.DryRun, &result.Status, &result.LegacyAPISubjects, &result.CurrentSignalUsers, &result.V2SignalUsers, &result.CurrentSignals, &result.V2Signals, &result.ChangedSubjects, &ruleHits, &sampleUsers, &result.EvidenceHighWater, &result.LabelHighWater, &ruleWatermark, &approvedDryRunID, &started, &completed)
	if err != nil {
		return result, err
	}
	result.StartedAt = started.UTC().Format(time.RFC3339Nano)
	if completed.Valid {
		result.CompletedAt = completed.Time.UTC().Format(time.RFC3339Nano)
	}
	result.RuleHits = map[string]int64{}
	result.RuleWatermark = map[string]int{}
	result.SampleUserIDs = []int64{}
	_ = json.Unmarshal(ruleHits, &result.RuleHits)
	_ = json.Unmarshal(ruleWatermark, &result.RuleWatermark)
	_ = json.Unmarshal(sampleUsers, &result.SampleUserIDs)
	if approvedDryRunID.Valid {
		result.ApprovedDryRunID = approvedDryRunID.Int64
	}
	return result, nil
}

func validateIdentityText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
func identityPage(limit, offset int) map[string]int {
	return map[string]int{"page": offset/limit + 1, "page_size": limit}
}
func identityErr(field string) error { return fmt.Errorf("%w: %s", ErrInvalidIdentity, field) }
func nullablePositiveInt64(value int64) any {
	if value > 0 {
		return value
	}
	return nil
}
