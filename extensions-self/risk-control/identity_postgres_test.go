package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

var riskTestSchemaSequence atomic.Uint64

func openIsolatedRiskTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("RISK_CONTROL_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("RISK_CONTROL_TEST_DATABASE_URL is not configured")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("invalid RISK_CONTROL_TEST_DATABASE_URL: %v", err)
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("risk_test_%d_%d", time.Now().UTC().UnixNano(), riskTestSchemaSequence.Add(1))
	if _, err := admin.ExecContext(context.Background(), `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatalf("create isolated schema: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	return db
}

func openRiskTestSession(t *testing.T, schema, applicationName string) *sql.DB {
	t.Helper()
	parsed, err := url.Parse(strings.TrimSpace(os.Getenv("RISK_CONTROL_TEST_DATABASE_URL")))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	query.Set("application_name", applicationName)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestIdentityPostgresStageZeroAndPersistenceContract(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE risk_rules SET enabled=TRUE,action='review',name='operator override',description='keep on stage zero' WHERE code='api_request_observation'`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatalf("repeat ApplySchema() error = %v", err)
	}
	var enabled bool
	var action, name string
	if err := db.QueryRowContext(ctx, `SELECT enabled,action,name FROM risk_rules WHERE code='api_request_observation'`).Scan(&enabled, &action, &name); err != nil {
		t.Fatal(err)
	}
	if !enabled || action != "review" || name != "operator override" {
		t.Fatalf("retired reliability rule state: enabled=%v action=%q name=%q", enabled, action, name)
	}

	cfg := testIdentityConfig()
	cfg.IPCollectionEnabled = true
	cfg.DeviceCollectionEnabled = true
	service, err := NewIdentityService(cfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	for index, address := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		_, err := service.Ingest(ctx, IdentityEventReport{
			EventKey: "registration-success-" + fmt.Sprint(index), EventType: "registration_success", EventClass: "registration", Outcome: "success",
			OccurredAt: base.Add(time.Duration(index) * time.Minute).Format(time.RFC3339Nano), UserID: int64(100 + index), ClientIP: address,
			IPSource: "remote_addr", ProxyChainValid: true, BrowserInstanceID: fmt.Sprintf("browser-%d", index), BrowserCookieStatus: "valid",
		})
		if err != nil {
			t.Fatalf("persist %s: %v", address, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT ip_family FROM risk_network_identities ORDER BY ip_family`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var families []int
	for rows.Next() {
		var family int
		if err := rows.Scan(&family); err != nil {
			t.Fatal(err)
		}
		families = append(families, family)
	}
	if fmt.Sprint(families) != "[4 6]" {
		t.Fatalf("stored IP families = %v", families)
	}
	geoLookupKey := service.protector.LookupKey("ip", "9.9.9.9")
	geoReports := []IdentityEventReport{
		{EventKey: "geo-unverified", EventType: "login_success", EventClass: "login", Outcome: "success", OccurredAt: base.Add(-4 * time.Minute).Format(time.RFC3339Nano), UserID: 300, ClientIP: "9.9.9.9", IPSource: "trusted_xff", ProxyChainValid: true, CountryCode: "ZZ", Region: "spoofed", ASN: 64512, GeoSource: "cloudflare_verified", GeoVerified: false},
		{EventKey: "geo-local", EventType: "login_success", EventClass: "login", Outcome: "success", OccurredAt: base.Add(-3 * time.Minute).Format(time.RFC3339Nano), UserID: 300, ClientIP: "9.9.9.9", IPSource: "trusted_xff", ProxyChainValid: true, CountryCode: "US", Region: "local-region", ASN: 19281, GeoSource: "maxmind_local", GeoVerified: true},
		{EventKey: "geo-cloudflare", EventType: "login_success", EventClass: "login", Outcome: "success", OccurredAt: base.Add(-2 * time.Minute).Format(time.RFC3339Nano), UserID: 300, ClientIP: "9.9.9.9", IPSource: "cf_connecting_ip", ProxyChainValid: true, CountryCode: "US", Region: "cloudflare-region", ASN: 19281, GeoSource: "cloudflare_verified", GeoVerified: true},
		{EventKey: "geo-local-later", EventType: "login_success", EventClass: "login", Outcome: "success", OccurredAt: base.Add(-time.Minute).Format(time.RFC3339Nano), UserID: 300, ClientIP: "9.9.9.9", IPSource: "trusted_xff", ProxyChainValid: true, CountryCode: "CA", Region: "later-local", ASN: 123, GeoSource: "maxmind_local", GeoVerified: true},
	}
	if _, err := service.Ingest(ctx, geoReports[0]); err != nil {
		t.Fatalf("persist geo report %s: %v", geoReports[0].EventKey, err)
	}
	var country, region, geoSource string
	var asn int64
	var geoVerified bool
	if err := db.QueryRowContext(ctx, `SELECT country_code,region,asn,geo_source,geo_verified FROM risk_network_identities WHERE lookup_key=$1`, geoLookupKey).Scan(&country, &region, &asn, &geoSource, &geoVerified); err != nil {
		t.Fatal(err)
	}
	if country != "" || region != "" || asn != 0 || geoSource != "" || geoVerified {
		t.Fatalf("unverified geo persisted = %q/%q/%d/%q/%v", country, region, asn, geoSource, geoVerified)
	}
	for _, report := range geoReports[1:] {
		if _, err := service.Ingest(ctx, report); err != nil {
			t.Fatalf("persist geo report %s: %v", report.EventKey, err)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT country_code,region,asn,geo_source,geo_verified FROM risk_network_identities WHERE lookup_key=$1`, geoLookupKey).Scan(&country, &region, &asn, &geoSource, &geoVerified); err != nil {
		t.Fatal(err)
	}
	if country != "US" || region != "cloudflare-region" || asn != 19281 || geoSource != "cloudflare_verified" || !geoVerified {
		t.Fatalf("geo source priority = %q/%q/%d/%q/%v", country, region, asn, geoSource, geoVerified)
	}

	apiInput := IdentityEventReport{
		EventKey: "api-success-202", EventType: "api_request", EventClass: identityEventAPI, Outcome: "success",
		OccurredAt: base.Format(time.RFC3339Nano), UserID: 202, ClientIP: "1.1.1.1", IPSource: "remote_addr", ProxyChainValid: true, APIKeyID: 77,
	}
	duplicate, err := service.Ingest(ctx, apiInput)
	if err != nil {
		t.Fatalf("persist API aggregate: %v", err)
	}
	if duplicate {
		t.Fatal("first API aggregate was marked duplicate")
	}
	duplicate, err = service.Ingest(ctx, apiInput)
	if err != nil || !duplicate {
		t.Fatalf("duplicate API aggregate = %v, %v", duplicate, err)
	}
	var aggregateCount, eventCount, ipLinks, deviceLinks int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(success_count),0) FROM risk_identity_activity_daily WHERE user_id=202`).Scan(&aggregateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_events WHERE user_id=202`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_user_ip_links WHERE user_id=202`).Scan(&ipLinks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_user_device_links WHERE user_id=202`).Scan(&deviceLinks); err != nil {
		t.Fatal(err)
	}
	if aggregateCount != 1 || eventCount != 0 || ipLinks != 0 || deviceLinks != 0 {
		t.Fatalf("API success storage = aggregate:%d events:%d ip_links:%d device_links:%d", aggregateCount, eventCount, ipLinks, deviceLinks)
	}
	networks, totalNetworks, err := service.repo.ListNetworks(ctx, 202, "", 20, 0)
	if err != nil || totalNetworks != 1 || len(networks) != 1 || networks[0].APISuccesses != 1 {
		t.Fatalf("API network detail = total:%d items:%#v error:%v", totalNetworks, networks, err)
	}
	devices, totalDevices, err := service.repo.ListDevices(ctx, 202, 20, 0)
	if err != nil || totalDevices != 1 || len(devices) != 1 || devices[0].Kind != "api_client" || devices[0].APISuccesses != 1 {
		t.Fatalf("API client detail = total:%d items:%#v error:%v", totalDevices, devices, err)
	}

	repo := NewSQLIdentityRepository(db)
	activation := cfg
	activation.RulesEnabled = true
	activation.ShadowUntil = base.Add(minimumIdentityShadowDuration)
	if err := repo.EnsureShadowActivation(ctx, activation, base); err != nil {
		t.Fatalf("record initial Shadow: %v", err)
	}
	var identityEventsBefore int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_events`).Scan(&identityEventsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES ('legacy-cleanup','login_failure',909,'login_failure','legacy evidence',$1,'legacy_v1')`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_event_keys(event_key,event_id) VALUES ('legacy-cleanup',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,event_count,last_action,last_event_at) VALUES (909,'login_failure','high',70,'legacy evidence',1,'review',$1)`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason) VALUES (7,'review','user','909','success','keep audit')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_rules(code,name,event_types,enabled,window_seconds,threshold,score,risk_level,action) VALUES ('registration_abuse','legacy combined rule','["registration_attempt","registration_success"]',FALSE,600,3,80,'critical','reject_candidate')`); err != nil {
		t.Fatal(err)
	}
	cleanup, err := repo.CleanupLegacyV1(ctx)
	if err != nil {
		t.Fatalf("cleanup legacy V1: %v", err)
	}
	if !cleanup.Applied || cleanup.EventsDeleted != 1 || cleanup.SubjectsDeleted != 1 || cleanup.RulesDeleted != 4 {
		t.Fatalf("cleanup result = %+v", cleanup)
	}
	for table, want := range map[string]int{"risk_event_keys": 0, "risk_events": 0, "risk_subjects": 0, "risk_identity_events": identityEventsBefore} {
		var got int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled,action FROM risk_rules WHERE code='api_request_observation'`).Scan(&enabled, &action); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("retired V1 rule query error = %v, want sql.ErrNoRows", err)
	}
	var genericRuleCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_rules WHERE code='login_failure_burst'`).Scan(&genericRuleCount); err != nil || genericRuleCount != 1 {
		t.Fatalf("generic rule count = %d, error = %v", genericRuleCount, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_rules(code,name,event_types,enabled,window_seconds,threshold,score,risk_level,action) VALUES
('registration_identity_abuse','old image seed','["registration_attempt"]',TRUE,600,3,80,'critical','reject_candidate'),
('registration_ip_multi_account','old image seed','["registration_success"]',TRUE,600,5,60,'high','review'),
('api_request_observation','old image seed','["api_request"]',FALSE,86400,1,0,'low','observe') ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var retiredRuleCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_rules WHERE code IN ('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation')`).Scan(&retiredRuleCount); err != nil || retiredRuleCount != 0 {
		t.Fatalf("retired rules after legacy image seed = %d, error = %v", retiredRuleCount, err)
	}
	var auditCount, cleanupEvents int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX((metadata->>'events_deleted')::int) FILTER (WHERE action='purge_legacy_v1'),0) FROM risk_audit_logs`).Scan(&auditCount, &cleanupEvents); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 || cleanupEvents != 1 {
		t.Fatalf("audit count = %d, cleanup events = %d", auditCount, cleanupEvents)
	}
	identityRules, err := repo.ListIdentityRules(ctx)
	if err != nil || len(identityRules) != 5 {
		t.Fatalf("identity rules = %#v, error = %v", identityRules, err)
	}
	adminCfg := activation
	adminCfg.AdminEnabled = true
	adminCfg.IPDomainEnabled = true
	adminCfg.DeviceDomainEnabled = true
	adminCfg.CompositeDomainEnabled = true
	serverCfg := Config{InternalSecret: testSecret, Mode: "shadow", Identity: adminCfg}
	server := NewHTTPServer(serverCfg, NewSQLRepository(db))
	request := signedRequest("GET", "/api/v1/admin/identity-rules", nil, testSecret, "nonce-identity-rules", base)
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"v2_registration_composite_accounts"`) {
		t.Fatalf("identity rule response = %d %s", response.Code, response.Body.String())
	}
	if second, err := repo.CleanupLegacyV1(ctx); err != nil || second.Applied {
		t.Fatalf("idempotent cleanup = %+v, error = %v", second, err)
	}
	if _, err := repo.Rebuild(ctx, 7, false, activation); err == nil || !strings.Contains(err.Error(), "Shadow period") {
		t.Fatalf("write rebuild before Shadow deadline error = %v", err)
	}
}

func TestPostCleanupLegacyEventsAreReclassifiedWithoutDeletion(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES ('pre-cleanup-legacy','login_failure',700,'login_failure','pre-cleanup evidence',$1,'legacy_v1')`, base); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var legacyEvents, migrationSix int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_events WHERE identity_version='legacy_v1'`).Scan(&legacyEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_schema_migrations WHERE version=6`).Scan(&migrationSix); err != nil {
		t.Fatal(err)
	}
	if legacyEvents != 1 || migrationSix != 0 {
		t.Fatalf("pre-cleanup state = legacy events:%d migration six:%d", legacyEvents, migrationSix)
	}
	repo := NewSQLIdentityRepository(db)
	cleanup, err := repo.CleanupLegacyV1(ctx)
	if err != nil || !cleanup.Applied || cleanup.EventsDeleted != 1 {
		t.Fatalf("cleanup before reclassification = %+v, error = %v", cleanup, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER risk_normalize_post_cleanup_event_version ON risk_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES
('post-cleanup-mislabeled','login_failure',701,'login_failure','post-cleanup evidence',$1,'legacy_v1'),
('post-cleanup-current','login_success',702,'login_success','current evidence',$1,'event_v2')`, base); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var totalEvents, currentEvents, auditCount, reclassified int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE identity_version='event_v2'),COUNT(*) FILTER(WHERE identity_version='legacy_v1') FROM risk_events`).Scan(&totalEvents, &currentEvents, &legacyEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX((metadata->>'events_reclassified')::int),0) FROM risk_audit_logs WHERE action='reclassify_post_cleanup_v1_events'`).Scan(&auditCount, &reclassified); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_schema_migrations WHERE version=6`).Scan(&migrationSix); err != nil {
		t.Fatal(err)
	}
	if totalEvents != 2 || currentEvents != 2 || legacyEvents != 0 || auditCount != 1 || reclassified != 1 || migrationSix != 1 {
		t.Fatalf("post-cleanup migration = total:%d current:%d legacy:%d audits:%d reclassified:%d version:%d", totalEvents, currentEvents, legacyEvents, auditCount, reclassified, migrationSix)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES ('post-marker-explicit-legacy','login_failure',703,'login_failure','guarded evidence',$1,'legacy_v1')`, base); err != nil {
		t.Fatal(err)
	}
	var guardedVersion string
	if err := db.QueryRowContext(ctx, `SELECT identity_version FROM risk_events WHERE event_key='post-marker-explicit-legacy'`).Scan(&guardedVersion); err != nil {
		t.Fatal(err)
	}
	if guardedVersion != "event_v2" {
		t.Fatalf("post-marker explicit version = %q, want event_v2", guardedVersion)
	}
	if _, err := db.ExecContext(ctx, `UPDATE risk_events SET identity_version='legacy_v1' WHERE event_key='post-marker-explicit-legacy'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT identity_version FROM risk_events WHERE event_key='post-marker-explicit-legacy'`).Scan(&guardedVersion); err != nil {
		t.Fatal(err)
	}
	if guardedVersion != "event_v2" {
		t.Fatalf("post-marker updated version = %q, want event_v2", guardedVersion)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_audit_logs WHERE action='reclassify_post_cleanup_v1_events'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("idempotent post-cleanup audit count = %d, want 1", auditCount)
	}
}

func TestLegacyCleanupSerializesWithUncommittedLegacyWriter(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	applicationName := fmt.Sprintf("risk_cleanup_race_%d", time.Now().UTC().UnixNano())
	cleanupDB := openRiskTestSession(t, schema, applicationName)

	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writerTx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writerTx.Rollback()
	if _, err := writerTx.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES ('cleanup-race-legacy','login_failure',704,'login_failure','concurrent cleanup evidence',NOW(),'legacy_v1')`); err != nil {
		t.Fatal(err)
	}

	type cleanupResult struct {
		result LegacyV1CleanupResult
		err    error
	}
	done := make(chan cleanupResult, 1)
	go func() {
		result, cleanupErr := NewSQLIdentityRepository(cleanupDB).CleanupLegacyV1(ctx)
		done <- cleanupResult{result: result, err: cleanupErr}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case completed := <-done:
			t.Fatalf("cleanup completed before the legacy writer committed: result=%+v error=%v", completed.result, completed.err)
		default:
		}
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM pg_locks l
JOIN pg_stat_activity a ON a.pid=l.pid
WHERE a.application_name=$1
  AND l.relation='risk_events'::regclass
  AND l.mode='ShareRowExclusiveLock'
  AND NOT l.granted)`, applicationName).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not wait for the active legacy writer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := writerTx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case completed := <-done:
		if completed.err != nil || !completed.result.Applied || completed.result.EventsDeleted != 1 {
			t.Fatalf("serialized cleanup = %+v, error = %v", completed.result, completed.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not finish after the writer committed")
	}
	var legacyEvents int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_events WHERE identity_version='legacy_v1'`).Scan(&legacyEvents); err != nil {
		t.Fatal(err)
	}
	if legacyEvents != 0 {
		t.Fatalf("legacy events after serialized cleanup = %d, want 0", legacyEvents)
	}
}

func TestLegacyCleanupBlocksUpdatesBeforeTupleLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at,identity_version) VALUES ('cleanup-update-race','login_failure',705,'login_failure','cleanup update evidence',NOW(),'legacy_v1')`); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	blockerTx, err := blocker.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blockerTx.Rollback()
	if _, err := blockerTx.ExecContext(ctx, `LOCK TABLE risk_schema_migrations IN ROW SHARE MODE`); err != nil {
		t.Fatal(err)
	}

	cleanupApplication := fmt.Sprintf("risk_cleanup_first_%d", time.Now().UTC().UnixNano())
	writerApplication := fmt.Sprintf("risk_cleanup_update_%d", time.Now().UTC().UnixNano())
	cleanupDB := openRiskTestSession(t, schema, cleanupApplication)
	writerDB := openRiskTestSession(t, schema, writerApplication)
	type cleanupResult struct {
		result LegacyV1CleanupResult
		err    error
	}
	cleanupDone := make(chan cleanupResult, 1)
	go func() {
		result, cleanupErr := NewSQLIdentityRepository(cleanupDB).CleanupLegacyV1(ctx)
		cleanupDone <- cleanupResult{result: result, err: cleanupErr}
	}()
	waitForLock := func(application, relation, mode string, granted bool) {
		t.Helper()
		for {
			var found bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM pg_locks l
JOIN pg_stat_activity a ON a.pid=l.pid
WHERE a.application_name=$1
  AND l.relation=$2::regclass
  AND l.mode=$3
  AND l.granted=$4)`, application, relation, mode, granted).Scan(&found); err != nil {
				t.Fatal(err)
			}
			if found {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("lock not observed for %s on %s: %v", application, relation, ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitForLock(cleanupApplication, "risk_events", "ShareRowExclusiveLock", true)
	waitForLock(cleanupApplication, "risk_schema_migrations", "ExclusiveLock", false)

	type writerResult struct {
		rows int64
		err  error
	}
	writerDone := make(chan writerResult, 1)
	go func() {
		result, updateErr := writerDB.ExecContext(ctx, `UPDATE risk_events SET identity_version='legacy_v1' WHERE event_key='cleanup-update-race'`)
		var rows int64
		if updateErr == nil {
			rows, updateErr = result.RowsAffected()
		}
		writerDone <- writerResult{rows: rows, err: updateErr}
	}()
	waitForLock(writerApplication, "risk_events", "RowExclusiveLock", false)
	if err := blockerTx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case completed := <-cleanupDone:
		if completed.err != nil || !completed.result.Applied || completed.result.EventsDeleted != 1 {
			t.Fatalf("cleanup-first update serialization = %+v, error = %v", completed.result, completed.err)
		}
	case <-ctx.Done():
		t.Fatalf("cleanup-first update did not finish: %v", ctx.Err())
	}
	select {
	case completed := <-writerDone:
		if completed.err != nil || completed.rows != 0 {
			t.Fatalf("post-cleanup update = rows:%d error:%v", completed.rows, completed.err)
		}
	case <-ctx.Done():
		t.Fatalf("blocked update did not finish: %v", ctx.Err())
	}
}

func TestReliabilityEventsAreNotProjectedAsUserRisk(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewSQLRepository(db)
	base := time.Now().UTC().Truncate(time.Second)
	insert := func(event EventRecord) {
		t.Helper()
		if _, _, err := repo.InsertEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	loginAt := base.Add(-time.Minute)
	recentObserveAt := base.Add(-30 * time.Second)
	insert(EventRecord{EventKey: "mixed-login", EventType: "login_failure", UserID: 501, RiskType: "login_failure", RiskLevel: "high", Score: 70, Reason: "login failure evidence", Decision: "review", IPHash: "login-ip", DeviceHash: "login-device", OccurredAt: loginAt.Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mixed-observe", EventType: "content_policy", UserID: 501, RiskType: "content_policy", RiskLevel: "low", Score: 10, Reason: "recent low-risk observation", Decision: "observe", OccurredAt: recentObserveAt.Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mixed-api", EventType: "api_request", UserID: 501, RiskType: "api_request", RiskLevel: "low", Score: 0, Reason: "API 请求观察", Decision: "observe", IPHash: "api-ip", DeviceHash: "api-device", OccurredAt: base.Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mixed-api-error", EventType: "api_error", UserID: 501, RiskType: "api_error", RiskLevel: "low", Score: 0, Reason: "gateway request failed", Decision: "observe", IPHash: "api-error-ip", DeviceHash: "api-error-device", OccurredAt: base.Add(time.Second).Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mixed-upstream-error", EventType: "upstream_error", UserID: 501, RiskType: "upstream_error", RiskLevel: "low", Score: 0, Reason: "upstream unavailable", Decision: "observe", IPHash: "upstream-ip", DeviceHash: "upstream-device", OccurredAt: base.Add(2 * time.Second).Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mismatched-risk-type", EventType: "login_failure", UserID: 505, RiskType: "api_error", RiskLevel: "high", Score: 65, Reason: "login failure with stale risk type", Decision: "review", OccurredAt: base.Add(-2 * time.Minute).Format(time.RFC3339Nano)})
	type reliabilityCase struct {
		userID    int64
		eventType string
		reason    string
	}
	reliabilityOnly := []reliabilityCase{
		{userID: 502, eventType: "api_request", reason: "API 请求观察"},
		{userID: 503, eventType: "api_error", reason: "gateway request failed"},
		{userID: 504, eventType: "upstream_error", reason: "upstream unavailable"},
	}
	for _, item := range reliabilityOnly {
		insert(EventRecord{EventKey: fmt.Sprintf("%s-only", item.eventType), EventType: item.eventType, UserID: item.userID, RiskType: item.eventType, RiskLevel: "low", Score: 0, Reason: item.reason, Decision: "observe", OccurredAt: base.Format(time.RFC3339Nano)})
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,event_count,ip_count,device_count,last_action,pending,last_event_at) VALUES(501,'login_failure','high',70,'login failure evidence',5,4,4,'review',FALSE,$1)`, base.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,event_count,last_action,pending,last_event_at) VALUES(505,'api_error','high',65,'login failure with stale risk type',1,'review',TRUE,$1)`, base.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, item := range reliabilityOnly {
		if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,event_count,last_action,last_event_at) VALUES($1,$2,'low',0,$3,1,'observe',$4)`, item.userID, item.eventType, item.reason, base); err != nil {
			t.Fatal(err)
		}
	}
	mixed, found, err := repo.GetSubject(ctx, 501)
	if err != nil || !found || mixed.RiskType != "login_failure" || mixed.Score != 70 || mixed.EventCount != 2 || mixed.IPCount != 1 || mixed.DeviceCount != 1 || mixed.LastAction != "review" || mixed.Pending || mixed.LastEventAt != recentObserveAt.Format("2006-01-02T15:04:05.000Z") {
		t.Fatalf("mixed legacy projection = %+v, found=%v, error=%v", mixed, found, err)
	}
	mismatched, found, err := repo.GetSubject(ctx, 505)
	if err != nil || !found || mismatched.RiskType != "login_failure" || mismatched.Score != 65 || mismatched.Reason != "login failure with stale risk type" {
		t.Fatalf("mismatched risk type projection = %+v, found=%v, error=%v", mismatched, found, err)
	}
	for _, item := range reliabilityOnly {
		subject, found, err := repo.GetSubject(ctx, item.userID)
		if err != nil || !found || subject.RiskType != "" || subject.RiskLevel != "none" || subject.Score != 0 || subject.Reason != "" || subject.EventCount != 0 {
			t.Fatalf("%s-only legacy projection = %+v, found=%v, error=%v", item.eventType, subject, found, err)
		}
	}
	for _, riskType := range []string{"api_request", "api_error", "upstream_error"} {
		items, total, err := repo.ListSubjects(ctx, 20, 0, riskType, "", nil)
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("legacy %s risk filter = total:%d items:%+v error:%v", riskType, total, items, err)
		}
	}
	for offset, wantUserID := range []int64{504, 503, 502} {
		items, total, err := repo.ListSubjects(ctx, 1, offset, "", "none", nil)
		if err != nil || total != 3 || len(items) != 1 || items[0].UserID != wantUserID {
			t.Fatalf("stable reliability pagination offset %d = total:%d items:%+v error:%v", offset, total, items, err)
		}
	}
}
