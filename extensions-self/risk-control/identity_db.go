package main

import (
	"context"
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

func NewSQLIdentityRepository(db *sql.DB) *SQLIdentityRepository {
	return &SQLIdentityRepository{db: db}
}

func (r *SQLIdentityRepository) ActivateShadowRules(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("identity repository unavailable")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE risk_rules
SET enabled=FALSE,action='observe',description='V1 历史身份/API 规则；V2 启用后停止计算且不与 V2 混算',revision=revision+1,updated_at=NOW()
WHERE code IN ('registration_identity_abuse','registration_ip_multi_account','api_request_observation')
  AND (enabled=TRUE OR action<>'observe')`)
	return err
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
			return PersistedIdentityEvent{}, true, nil
		}
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

	err = tx.QueryRowContext(ctx, `INSERT INTO risk_identity_events
(event_key,event_type,event_class,outcome,user_id,email_lookup_key,network_identity_id,browser_identity_id,profile_identity_id,api_client_identity_id,ip_quality_valid,device_quality_valid,proxy_chain_valid,occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0),NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),$11,$12,$13,$14)
	ON CONFLICT(event_key) DO NOTHING RETURNING id`, fact.EventKey, fact.EventType, fact.EventClass, fact.Outcome, fact.UserID, fact.EmailLookupKey, networkID, browserID, profileID, apiClientID, fact.IPQualityValid, fact.DeviceQualityValid, fact.ProxyChainValid, fact.OccurredAt).Scan(&stored.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistedIdentityEvent{}, true, nil
	}
	if err != nil {
		return PersistedIdentityEvent{}, false, err
	}
	if err := upsertIdentityLinks(ctx, tx, fact, networkID, browserID, profileID, apiClientID); err != nil {
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
 country_code=CASE WHEN EXCLUDED.geo_verified THEN EXCLUDED.country_code ELSE risk_network_identities.country_code END,
 region=CASE WHEN EXCLUDED.geo_verified THEN EXCLUDED.region ELSE risk_network_identities.region END,
 city=CASE WHEN EXCLUDED.geo_verified THEN EXCLUDED.city ELSE risk_network_identities.city END,
 asn=CASE WHEN EXCLUDED.geo_verified THEN EXCLUDED.asn ELSE risk_network_identities.asn END,
 geo_source=CASE WHEN EXCLUDED.geo_verified THEN EXCLUDED.geo_source ELSE risk_network_identities.geo_source END,
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
	case "login":
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
	if !cfg.RulesEnabled || event.ID <= 0 || event.EventClass != "registration" {
		return nil
	}
	states, _, err := r.qualityStates(ctx, cfg)
	if err != nil {
		return err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT code,domain,window_seconds,threshold,score FROM risk_identity_rules WHERE enabled=TRUE AND mode='shadow'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rule struct {
		code, domain             string
		window, threshold, score int
	}
	var rules []rule
	for rows.Next() {
		var item rule
		if err := rows.Scan(&item.code, &item.domain, &item.window, &item.threshold, &item.score); err != nil {
			return err
		}
		rules = append(rules, item)
	}
	for _, rule := range rules {
		if rule.domain == "account" {
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
				evidence, _ := json.Marshal(map[string]any{"evidence_count": count, "window_seconds": rule.window, "email_fingerprint": identityEmailDisplay(event.EmailLookupKey)})
				if _, err = r.db.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,score,evidence_count,observing,evidence,occurred_at) VALUES ($1,0,'account',$2,0,$3,TRUE,$4,$5) ON CONFLICT(event_id,user_id,rule_code) DO NOTHING`, event.ID, rule.code, count, evidence, event.OccurredAt); err != nil {
					return err
				}
			}
			continue
		}
		if event.Outcome != "success" || event.UserID <= 0 || states[rule.domain] != "healthy" {
			continue
		}
		if rule.domain == "ip" && (!cfg.IPCollectionEnabled || !cfg.IPDomainEnabled || !event.IPQualityValid || event.NetworkID == 0) {
			continue
		}
		deviceID := event.BrowserID
		if deviceID == 0 {
			deviceID = event.APIClientID
		}
		if rule.domain == "device" && (!cfg.DeviceCollectionEnabled || !cfg.DeviceDomainEnabled || !event.DeviceQualityValid || deviceID == 0) {
			continue
		}
		if rule.domain == "composite" && (!cfg.CompositeDomainEnabled || !cfg.IPCollectionEnabled || !cfg.DeviceCollectionEnabled || !cfg.IPDomainEnabled || !cfg.DeviceDomainEnabled || !event.IPQualityValid || !event.DeviceQualityValid || event.NetworkID == 0 || event.BrowserID == 0) {
			continue
		}
		var count int
		since := event.OccurredAt.Add(-time.Duration(rule.window) * time.Second)
		switch rule.domain {
		case "ip":
			err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE event_class='registration' AND outcome='success' AND ip_quality_valid AND network_identity_id=$1 AND user_id>0 AND occurred_at BETWEEN $2 AND $3`, event.NetworkID, since, event.OccurredAt).Scan(&count)
		case "device":
			err = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM risk_identity_events WHERE event_class='registration' AND outcome='success' AND device_quality_valid AND (browser_identity_id=$1 OR api_client_identity_id=$1) AND user_id>0 AND occurred_at BETWEEN $2 AND $3`, deviceID, since, event.OccurredAt).Scan(&count)
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
		evidence, _ := json.Marshal(map[string]any{"evidence_count": count, "window_seconds": rule.window, "same_event_pair": rule.domain == "composite"})
		_, err = r.db.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,TRUE,NULLIF($7,0),NULLIF($8,0),$9,$10) ON CONFLICT(event_id,user_id,rule_code) DO NOTHING`, event.ID, event.UserID, rule.domain, rule.code, rule.score, count, event.NetworkID, deviceID, evidence, event.OccurredAt)
		if err != nil {
			return err
		}
	}
	if event.UserID > 0 {
		return r.refreshUserSummary(ctx, event.UserID)
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

func (r *SQLIdentityRepository) refreshUserSummary(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO risk_identity_user_summaries(user_id,overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count,updated_at)
SELECT $1,GREATEST(COALESCE(MAX(score) FILTER (WHERE domain='ip'),0),COALESCE(MAX(score) FILTER (WHERE domain='device'),0),COALESCE(MAX(score) FILTER (WHERE domain='composite'),0)),COALESCE(MAX(score) FILTER (WHERE domain='ip'),0),COALESCE(MAX(score) FILTER (WHERE domain='device'),0),COALESCE(MAX(score) FILTER (WHERE domain='composite'),0),COUNT(*) FILTER (WHERE domain='ip'),COUNT(*) FILTER (WHERE domain='device'),COUNT(*) FILTER (WHERE domain='composite'),NOW()
FROM risk_identity_signals WHERE user_id=$1
ON CONFLICT(user_id) DO UPDATE SET overall_score=EXCLUDED.overall_score,ip_score=EXCLUDED.ip_score,device_score=EXCLUDED.device_score,composite_score=EXCLUDED.composite_score,ip_signal_count=EXCLUDED.ip_signal_count,device_signal_count=EXCLUDED.device_signal_count,composite_signal_count=EXCLUDED.composite_signal_count,updated_at=NOW()`, userID)
	return err
}

func (r *SQLIdentityRepository) Summary(ctx context.Context, userID int64, cfg IdentityConfig) (IdentitySummary, error) {
	result := IdentitySummary{UserID: userID, IdentityVersion: identityVersionV2, Mode: "shadow", LegacyNotice: "V1 历史数据仅保留原哈希，无法反推真实 IP，且不与 V2 混算。"}
	var ipScore, deviceScore, compositeScore, ipSignals, deviceSignals, compositeSignals int
	err := r.db.QueryRowContext(ctx, `SELECT overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count FROM risk_identity_user_summaries WHERE user_id=$1`, userID).Scan(&result.OverallScore, &ipScore, &deviceScore, &compositeScore, &ipSignals, &deviceSignals, &compositeSignals)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return result, err
	}
	associated := map[string]int{}
	var ipAssociated, deviceAssociated, compositeAssociated int
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT other.user_id) FROM risk_user_ip_links mine JOIN risk_user_ip_links other USING(network_identity_id) WHERE mine.user_id=$1 AND other.user_id<>$1`, userID).Scan(&ipAssociated)
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT other.user_id) FROM risk_user_device_links mine JOIN risk_user_device_links other USING(device_identity_id) JOIN risk_device_identities identity ON identity.id=mine.device_identity_id WHERE mine.user_id=$1 AND other.user_id<>$1 AND identity.identity_kind IN ('browser_instance','api_client')`, userID).Scan(&deviceAssociated)
	_ = r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT other.user_id) FROM risk_identity_events mine JOIN risk_identity_events other ON other.network_identity_id=mine.network_identity_id AND other.browser_identity_id=mine.browser_identity_id AND ABS(EXTRACT(EPOCH FROM(other.occurred_at-mine.occurred_at)))<=600 WHERE mine.user_id=$1 AND other.user_id<>$1 AND mine.event_class='registration' AND other.event_class='registration' AND mine.outcome='success' AND other.outcome='success' AND mine.ip_quality_valid AND other.ip_quality_valid AND mine.device_quality_valid AND other.device_quality_valid AND mine.network_identity_id IS NOT NULL AND mine.browser_identity_id IS NOT NULL`, userID).Scan(&compositeAssociated)
	associated["ip"], associated["device"], associated["composite"] = ipAssociated, deviceAssociated, compositeAssociated
	states, _, stateErr := r.qualityStates(ctx, cfg)
	if stateErr != nil {
		return result, stateErr
	}
	signals := map[string][]IdentitySignalSummary{"ip": {}, "device": {}, "composite": {}}
	rows, signalErr := r.db.QueryContext(ctx, `SELECT domain,rule_code,score,evidence_count,occurred_at FROM (
 SELECT domain,rule_code,score,evidence_count,occurred_at,ROW_NUMBER() OVER(PARTITION BY domain ORDER BY score DESC,occurred_at DESC) row_number
 FROM risk_identity_signals WHERE user_id=$1 AND domain IN ('ip','device','composite')
) ranked WHERE row_number<=20 ORDER BY domain,score DESC,occurred_at DESC`, userID)
	if signalErr != nil {
		return result, signalErr
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		var item IdentitySignalSummary
		var occurredAt time.Time
		if err := rows.Scan(&domain, &item.RuleCode, &item.Score, &item.EvidenceCount, &occurredAt); err != nil {
			return result, err
		}
		item.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
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
		if !cfg.RulesEnabled || states[domain.name] != "healthy" {
			*domain.score = 0
			*domain.count = 0
			signals[domain.name] = []IdentitySignalSummary{}
		}
	}
	result.OverallScore = max(ipScore, deviceScore, compositeScore)
	result.Domains = []IdentityDomainSummary{
		{Domain: "ip", State: states["ip"], Score: ipScore, SignalCount: ipSignals, AssociatedAccountCount: associated["ip"], Signals: signals["ip"]},
		{Domain: "device", State: states["device"], Score: deviceScore, SignalCount: deviceSignals, AssociatedAccountCount: associated["device"], Signals: signals["device"]},
		{Domain: "composite", State: states["composite"], Score: compositeScore, SignalCount: compositeSignals, AssociatedAccountCount: associated["composite"], Signals: signals["composite"]},
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
SELECT n.id,n.lookup_key,n.ip_ciphertext,n.ip_nonce,n.encryption_key_id,n.ip_family,n.ip_source,n.is_public,n.country_code,n.region,n.city,n.asn,n.geo_source,n.geo_verified,l.first_seen_at,l.last_seen_at,l.registration_success_count,l.login_success_count,l.api_success_count,
(SELECT COUNT(DISTINCT linked.user_id) FROM (
 SELECT user_id FROM risk_user_ip_links WHERE network_identity_id=n.id
 UNION ALL SELECT user_id FROM risk_identity_activity_daily WHERE network_identity_id=n.id
) linked)
FROM grouped l JOIN risk_network_identities n ON n.id=l.network_identity_id WHERE ($2='' OR n.lookup_key=$2) ORDER BY l.last_seen_at DESC LIMIT $3 OFFSET $4`, userID, lookupKey, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []NetworkIdentityRow{}
	for rows.Next() {
		var item NetworkIdentityRow
		var first, last time.Time
		if err := rows.Scan(&item.ID, &item.LookupKey, &item.Ciphertext, &item.Nonce, &item.KeyID, &item.IPFamily, &item.IPSource, &item.Public, &item.CountryCode, &item.Region, &item.City, &item.ASN, &item.GeoSource, &item.GeoVerified, &first, &last, &item.RegistrationSuccesses, &item.LoginSuccesses, &item.APISuccesses, &item.AssociatedAccountCount); err != nil {
			return nil, 0, err
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
	query := `WITH relations AS (
 SELECT other.user_id,COUNT(DISTINCT mine.network_identity_id)::int shared_ip,0::int shared_device,0::int cooccur,MIN(other.first_seen_at) first_seen,MAX(other.last_seen_at) last_seen FROM risk_user_ip_links mine JOIN risk_user_ip_links other USING(network_identity_id) WHERE mine.user_id=$1 AND other.user_id<>$1 GROUP BY other.user_id
 UNION ALL
 SELECT other.user_id,0,COUNT(DISTINCT mine.device_identity_id)::int,0,MIN(other.first_seen_at),MAX(other.last_seen_at) FROM risk_user_device_links mine JOIN risk_user_device_links other USING(device_identity_id) JOIN risk_device_identities identity ON identity.id=mine.device_identity_id WHERE mine.user_id=$1 AND other.user_id<>$1 AND identity.identity_kind IN ('browser_instance','api_client') GROUP BY other.user_id
 UNION ALL
 SELECT other.user_id,0,0,COUNT(DISTINCT other.id)::int,MIN(other.occurred_at),MAX(other.occurred_at) FROM risk_identity_events mine JOIN risk_identity_events other ON other.network_identity_id=mine.network_identity_id AND other.browser_identity_id=mine.browser_identity_id AND ABS(EXTRACT(EPOCH FROM(other.occurred_at-mine.occurred_at)))<=600 WHERE mine.user_id=$1 AND other.user_id<>$1 AND mine.event_class='registration' AND other.event_class='registration' AND mine.outcome='success' AND other.outcome='success' AND mine.ip_quality_valid AND other.ip_quality_valid AND mine.device_quality_valid AND other.device_quality_valid AND mine.network_identity_id IS NOT NULL AND mine.browser_identity_id IS NOT NULL GROUP BY other.user_id
), grouped AS (SELECT user_id,SUM(shared_ip)::int shared_ip,SUM(shared_device)::int shared_device,SUM(cooccur)::int cooccur,MIN(first_seen) first_seen,MAX(last_seen) last_seen FROM relations GROUP BY user_id)
SELECT user_id,shared_ip,shared_device,cooccur,first_seen,last_seen,COUNT(*) OVER() FROM grouped ORDER BY cooccur DESC,shared_ip+shared_device DESC,last_seen DESC LIMIT $2 OFFSET $3`
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
		if err := rows.Scan(&item.UserID, &item.SharedNetworkCount, &item.SharedDeviceCount, &item.CooccurringEvidenceCount, &first, &last, &total); err != nil {
			return nil, 0, err
		}
		item.FirstSeenAt = first.UTC().Format(time.RFC3339Nano)
		item.LastSeenAt = last.UTC().Format(time.RFC3339Nano)
		switch {
		case item.CooccurringEvidenceCount > 0:
			item.Relation = "composite"
			item.EvidenceStrength = "high"
			item.EvidenceWindowSeconds = 600
		case item.SharedNetworkCount > 0 && item.SharedDeviceCount > 0:
			item.Relation = "multi_domain"
			item.EvidenceStrength = "medium_high"
		case item.SharedNetworkCount > 0:
			item.Relation = "ip"
			item.EvidenceStrength = "weak"
		default:
			item.Relation = "device"
			item.EvidenceStrength = "medium_high"
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
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
		if cfg.RulesEnabled && states[domain] == "healthy" {
			healthyDomains = append(healthyDomains, domain)
		}
	}
	rows, err := r.db.QueryContext(ctx, `WITH requested AS (SELECT DISTINCT UNNEST($1::bigint[]) user_id),
latest_network AS (
 SELECT DISTINCT ON (l.user_id) l.user_id,n.lookup_key,n.ip_ciphertext,n.ip_nonce,n.encryption_key_id,n.country_code,n.region
 FROM risk_user_ip_links l JOIN risk_network_identities n ON n.id=l.network_identity_id JOIN requested r USING(user_id)
 ORDER BY l.user_id,l.last_seen_at DESC
), device_counts AS (
 SELECT l.user_id,COUNT(*) FILTER(WHERE d.identity_kind='browser_instance')::int browsers,COUNT(*) FILTER(WHERE d.identity_kind='api_client')::int api_clients
 FROM risk_user_device_links l JOIN risk_device_identities d ON d.id=l.device_identity_id JOIN requested r USING(user_id) GROUP BY l.user_id
), associated AS (
 SELECT user_id,COUNT(DISTINCT other_user_id)::int associated_accounts FROM (
  SELECT mine.user_id,other.user_id other_user_id FROM risk_user_ip_links mine JOIN risk_user_ip_links other USING(network_identity_id) JOIN requested r ON r.user_id=mine.user_id WHERE other.user_id<>mine.user_id
  UNION ALL
  SELECT mine.user_id,other.user_id FROM risk_user_device_links mine JOIN risk_user_device_links other USING(device_identity_id) JOIN risk_device_identities identity ON identity.id=mine.device_identity_id JOIN requested r ON r.user_id=mine.user_id WHERE other.user_id<>mine.user_id AND identity.identity_kind IN ('browser_instance','api_client')
 ) relations GROUP BY user_id
), active_rules AS (
 SELECT s.user_id,COUNT(DISTINCT s.rule_code)::int active_rules FROM risk_identity_signals s JOIN risk_identity_rules rule ON rule.code=s.rule_code AND rule.enabled AND rule.mode='shadow' JOIN requested r USING(user_id) WHERE s.domain=ANY($2::text[]) GROUP BY s.user_id
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
		case "degraded":
			result = "degraded"
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
	result := IdentityHealth{Enabled: cfg.Enabled, Mode: "shadow", Schema: "v2", KeyID: cfg.EncryptionKeyID, GeoSource: cfg.GeoSource, Domains: map[string]string{}, Quality24H: map[string]any{}}
	if !cfg.ShadowUntil.IsZero() {
		result.ShadowUntil = cfg.ShadowUntil.Format(time.RFC3339)
	}
	states, counts, err := r.qualityStates(ctx, cfg)
	if err != nil {
		return result, err
	}
	result.Domains = states
	result.Quality24H = map[string]any{"events": counts.Total, "valid_ip": counts.ValidIP, "valid_device": counts.ValidDevice, "linked_users": counts.LinkedUsers, "max_network_users": counts.MaxNetworkUsers, "minimum_events": cfg.QualityMinEvents, "minimum_coverage_percent": cfg.QualityMinCoverage, "maximum_ip_share_percent": cfg.QualityMaxIPShare}
	return result, nil
}

func (r *SQLIdentityRepository) qualityStates(ctx context.Context, cfg IdentityConfig) (map[string]string, identityQualityCounts, error) {
	return queryIdentityQualityStates(ctx, r.db, cfg)
}

func queryIdentityQualityStates(ctx context.Context, queryer identityQueryer, cfg IdentityConfig) (map[string]string, identityQualityCounts, error) {
	var counts identityQualityCounts
	err := queryer.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE ip_quality_valid),COUNT(*) FILTER(WHERE device_quality_valid) FROM risk_identity_events WHERE occurred_at>=NOW()-interval '24 hours'`).Scan(&counts.Total, &counts.ValidIP, &counts.ValidDevice)
	if err == nil {
		err = queryer.QueryRowContext(ctx, `SELECT COALESCE((SELECT COUNT(DISTINCT user_id) FROM risk_user_ip_links),0),COALESCE(MAX(user_count),0) FROM (SELECT network_identity_id,COUNT(DISTINCT user_id) user_count FROM risk_user_ip_links GROUP BY network_identity_id) grouped`).Scan(&counts.LinkedUsers, &counts.MaxNetworkUsers)
	}
	return identityDomainStates(cfg, counts), counts, err
}

func (r *SQLIdentityRepository) Rebuild(ctx context.Context, actorID int64, dryRun bool, cfg IdentityConfig) (RebuildResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RebuildResult{}, err
	}
	defer tx.Rollback()
	if !dryRun {
		if !cfg.RulesEnabled {
			return RebuildResult{}, errors.New("identity rules are not enabled")
		}
		if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_identity_events, risk_identity_rules IN SHARE MODE`); err != nil {
			return RebuildResult{}, err
		}
		var shadowUntil time.Time
		if err := tx.QueryRowContext(ctx, `SELECT shadow_until FROM risk_identity_shadow_activation WHERE singleton=1`).Scan(&shadowUntil); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RebuildResult{}, errors.New("identity Shadow activation has not started")
			}
			return RebuildResult{}, err
		}
		if time.Now().UTC().Before(shadowUntil.UTC()) {
			return RebuildResult{}, fmt.Errorf("identity Shadow period is active until %s", shadowUntil.UTC().Format(time.RFC3339))
		}
		var dryRunID int64
		var dryRunCompleted time.Time
		if err := tx.QueryRowContext(ctx, `SELECT id,completed_at FROM risk_identity_rebuild_jobs
WHERE dry_run=TRUE AND status='completed' AND requested_by=$1 AND completed_at IS NOT NULL
ORDER BY completed_at DESC,id DESC LIMIT 1`, actorID).Scan(&dryRunID, &dryRunCompleted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RebuildResult{}, errors.New("a completed Dry Run by the same administrator is required")
			}
			return RebuildResult{}, err
		}
		if age := time.Since(dryRunCompleted.UTC()); age < 0 || age > 30*time.Minute {
			return RebuildResult{}, fmt.Errorf("Dry Run %d is older than 30 minutes", dryRunID)
		}
		var changed bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM risk_identity_events WHERE created_at>$1
) OR EXISTS(
 SELECT 1 FROM risk_identity_rules WHERE updated_at>$1
)`, dryRunCompleted.UTC()).Scan(&changed); err != nil {
			return RebuildResult{}, err
		}
		if changed {
			return RebuildResult{}, fmt.Errorf("identity evidence or rules changed after Dry Run %d", dryRunID)
		}
	}
	states, _, err := queryIdentityQualityStates(ctx, tx, cfg)
	if err != nil {
		return RebuildResult{}, err
	}
	result := RebuildResult{DryRun: dryRun, Status: "completed", RuleHits: map[string]int64{}, SampleUserIDs: []int64{}, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_subjects WHERE risk_type='api_request' OR reason ILIKE '%API 请求观察%'`).Scan(&result.LegacyAPISubjects); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id),COUNT(*) FROM risk_identity_signals WHERE domain IN ('ip','device','composite')`).Scan(&result.CurrentSignalUsers, &result.CurrentSignals); err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE identity_rebuild_candidates ON COMMIT DROP AS
WITH rules AS (
 SELECT code,domain,window_seconds,threshold,score,revision FROM risk_identity_rules
 WHERE enabled AND mode='shadow'
   AND (($1 AND domain='ip') OR ($2 AND domain='device') OR ($3 AND domain='composite'))
), anchors AS (
 SELECT id,user_id,network_identity_id,browser_identity_id,api_client_identity_id,ip_quality_valid,device_quality_valid,occurred_at
 FROM risk_identity_events
 WHERE event_class='registration' AND outcome='success' AND user_id>0
)
SELECT anchor.id event_id,anchor.user_id,rule.domain,rule.code rule_code,rule.score,counts.evidence_count,
 anchor.network_identity_id,CASE WHEN rule.domain='composite' THEN anchor.browser_identity_id ELSE COALESCE(anchor.browser_identity_id,anchor.api_client_identity_id) END device_identity_id,
	jsonb_build_object('evidence_count',counts.evidence_count,'window_seconds',rule.window_seconds,'source_event_id',anchor.id,'network_identity_id',anchor.network_identity_id,'device_identity_id',CASE WHEN rule.domain='composite' THEN anchor.browser_identity_id ELSE COALESCE(anchor.browser_identity_id,anchor.api_client_identity_id) END,'rule_revision',rule.revision,'same_event_pair',rule.domain='composite') evidence,
 anchor.occurred_at
FROM anchors anchor CROSS JOIN rules rule
CROSS JOIN LATERAL (
 SELECT COUNT(DISTINCT evidence.user_id)::int evidence_count
 FROM risk_identity_events evidence
 WHERE evidence.event_class='registration' AND evidence.outcome='success' AND evidence.user_id>0
   AND evidence.occurred_at BETWEEN anchor.occurred_at-(rule.window_seconds*interval '1 second') AND anchor.occurred_at
   AND CASE rule.domain
     WHEN 'ip' THEN anchor.ip_quality_valid AND anchor.network_identity_id IS NOT NULL AND evidence.ip_quality_valid AND evidence.network_identity_id=anchor.network_identity_id
     WHEN 'device' THEN anchor.device_quality_valid AND COALESCE(anchor.browser_identity_id,anchor.api_client_identity_id) IS NOT NULL AND evidence.device_quality_valid AND (evidence.browser_identity_id=COALESCE(anchor.browser_identity_id,anchor.api_client_identity_id) OR evidence.api_client_identity_id=COALESCE(anchor.browser_identity_id,anchor.api_client_identity_id))
     WHEN 'composite' THEN anchor.ip_quality_valid AND anchor.device_quality_valid AND anchor.network_identity_id IS NOT NULL AND anchor.browser_identity_id IS NOT NULL AND evidence.ip_quality_valid AND evidence.device_quality_valid AND evidence.network_identity_id=anchor.network_identity_id AND evidence.browser_identity_id=anchor.browser_identity_id
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
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM identity_rebuild_candidates ORDER BY md5(user_id::text),user_id LIMIT 10`)
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
	if err = tx.QueryRowContext(ctx, `WITH current_summary AS (
 SELECT user_id,MAX(score) FILTER(WHERE domain='ip') ip_score,MAX(score) FILTER(WHERE domain='device') device_score,MAX(score) FILTER(WHERE domain='composite') composite_score,COUNT(*) FILTER(WHERE domain='ip') ip_count,COUNT(*) FILTER(WHERE domain='device') device_count,COUNT(*) FILTER(WHERE domain='composite') composite_count FROM risk_identity_signals WHERE domain IN ('ip','device','composite') GROUP BY user_id
), desired_summary AS (
 SELECT user_id,MAX(score) FILTER(WHERE domain='ip') ip_score,MAX(score) FILTER(WHERE domain='device') device_score,MAX(score) FILTER(WHERE domain='composite') composite_score,COUNT(*) FILTER(WHERE domain='ip') ip_count,COUNT(*) FILTER(WHERE domain='device') device_count,COUNT(*) FILTER(WHERE domain='composite') composite_count FROM identity_rebuild_candidates GROUP BY user_id
)
SELECT COUNT(*) FROM current_summary current FULL OUTER JOIN desired_summary desired USING(user_id)
WHERE ROW(current.ip_score,current.device_score,current.composite_score,current.ip_count,current.device_count,current.composite_count) IS DISTINCT FROM ROW(desired.ip_score,desired.device_score,desired.composite_score,desired.ip_count,desired.device_count,desired.composite_count)`).Scan(&result.ChangedSubjects); err != nil {
		return result, err
	}
	result.ChangedSubjects += result.LegacyAPISubjects
	ruleHits, _ := json.Marshal(result.RuleHits)
	sampleUsers, _ := json.Marshal(result.SampleUserIDs)
	var completedAt time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO risk_identity_rebuild_jobs(dry_run,status,requested_by,legacy_api_subjects,current_signal_users,v2_signal_users,current_signals,v2_signals,changed_subjects,rule_hits,sample_user_ids,completed_at) VALUES($1,'completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW()) RETURNING id,completed_at`, dryRun, actorID, result.LegacyAPISubjects, result.CurrentSignalUsers, result.V2SignalUsers, result.CurrentSignals, result.V2Signals, result.ChangedSubjects, ruleHits, sampleUsers).Scan(&result.ID, &completedAt)
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
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_signal_history(original_signal_id,event_id,user_id,domain,rule_code,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,occurred_at,original_created_at)
SELECT id,event_id,user_id,domain,rule_code,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,occurred_at,created_at FROM risk_identity_signals
WHERE domain IN ('ip','device','composite')
ON CONFLICT(original_signal_id) DO NOTHING`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM risk_identity_signals WHERE domain IN ('ip','device','composite')`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,score,evidence_count,observing,network_identity_id,device_identity_id,evidence,occurred_at)
SELECT event_id,user_id,domain,rule_code,score,evidence_count,TRUE,network_identity_id,device_identity_id,evidence,occurred_at FROM identity_rebuild_candidates`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM risk_identity_user_summaries`); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO risk_identity_user_summaries(user_id,overall_score,ip_score,device_score,composite_score,ip_signal_count,device_signal_count,composite_signal_count) SELECT user_id,GREATEST(COALESCE(MAX(score) FILTER(WHERE domain='ip'),0),COALESCE(MAX(score) FILTER(WHERE domain='device'),0),COALESCE(MAX(score) FILTER(WHERE domain='composite'),0)),COALESCE(MAX(score) FILTER(WHERE domain='ip'),0),COALESCE(MAX(score) FILTER(WHERE domain='device'),0),COALESCE(MAX(score) FILTER(WHERE domain='composite'),0),COUNT(*) FILTER(WHERE domain='ip'),COUNT(*) FILTER(WHERE domain='device'),COUNT(*) FILTER(WHERE domain='composite') FROM risk_identity_signals GROUP BY user_id`); err != nil {
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
	var ruleHits, sampleUsers []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,dry_run,status,legacy_api_subjects,current_signal_users,v2_signal_users,current_signals,v2_signals,changed_subjects,rule_hits,sample_user_ids,started_at,completed_at FROM risk_identity_rebuild_jobs WHERE id=$1`, id).Scan(&result.ID, &result.DryRun, &result.Status, &result.LegacyAPISubjects, &result.CurrentSignalUsers, &result.V2SignalUsers, &result.CurrentSignals, &result.V2Signals, &result.ChangedSubjects, &ruleHits, &sampleUsers, &started, &completed)
	if err != nil {
		return result, err
	}
	result.StartedAt = started.UTC().Format(time.RFC3339Nano)
	if completed.Valid {
		result.CompletedAt = completed.Time.UTC().Format(time.RFC3339Nano)
	}
	result.RuleHits = map[string]int64{}
	result.SampleUserIDs = []int64{}
	_ = json.Unmarshal(ruleHits, &result.RuleHits)
	_ = json.Unmarshal(sampleUsers, &result.SampleUserIDs)
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
func identityErr(field string) error { return fmt.Errorf("invalid identity field: %s", field) }
