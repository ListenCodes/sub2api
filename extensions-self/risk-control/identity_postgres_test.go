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
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_events(event_key,event_type,user_id,risk_type,reason,occurred_at) VALUES ('legacy-cleanup','login_failure',909,'login_failure','legacy evidence',$1)`, base); err != nil {
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

func TestLegacyAPIObservationIsNotProjectedAsUserRisk(t *testing.T) {
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
	insert(EventRecord{EventKey: "mixed-login", EventType: "login_failure", UserID: 501, RiskType: "login_failure", RiskLevel: "high", Score: 70, Reason: "login failure evidence", Decision: "review", OccurredAt: base.Add(-time.Minute).Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "mixed-api", EventType: "api_request", UserID: 501, RiskType: "api_request", RiskLevel: "low", Score: 0, Reason: "API 请求观察", Decision: "observe", OccurredAt: base.Format(time.RFC3339Nano)})
	insert(EventRecord{EventKey: "api-only", EventType: "api_request", UserID: 502, RiskType: "api_request", RiskLevel: "low", Score: 0, Reason: "API 请求观察", Decision: "observe", OccurredAt: base.Format(time.RFC3339Nano)})
	for _, userID := range []int64{501, 502} {
		if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,event_count,last_action,last_event_at) VALUES($1,'api_request','low',0,'命中规则：API 请求观察（24 小时内1 次事件）',1,'observe',$2)`, userID, base); err != nil {
			t.Fatal(err)
		}
	}
	mixed, found, err := repo.GetSubject(ctx, 501)
	if err != nil || !found || mixed.RiskType != "login_failure" || mixed.Score != 70 || mixed.EventCount != 1 {
		t.Fatalf("mixed legacy projection = %+v, found=%v, error=%v", mixed, found, err)
	}
	apiOnly, found, err := repo.GetSubject(ctx, 502)
	if err != nil || !found || apiOnly.RiskType != "" || apiOnly.RiskLevel != "none" || apiOnly.Score != 0 || apiOnly.Reason != "" || apiOnly.EventCount != 0 {
		t.Fatalf("API-only legacy projection = %+v, found=%v, error=%v", apiOnly, found, err)
	}
	items, total, err := repo.ListSubjects(ctx, 20, 0, "api_request", "", nil)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("legacy API risk filter = total:%d items:%+v error:%v", total, items, err)
	}
}
