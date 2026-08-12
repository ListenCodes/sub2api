package main

import (
	"context"
	"database/sql"
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
		t.Fatalf("Stage 0 changed V1 rule: enabled=%v action=%q name=%q", enabled, action, name)
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
	if err := repo.ActivateShadowRules(ctx); err != nil {
		t.Fatalf("activate Shadow rules: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled,action FROM risk_rules WHERE code='api_request_observation'`).Scan(&enabled, &action); err != nil {
		t.Fatal(err)
	}
	if enabled || action != "observe" {
		t.Fatalf("V1 rule was not retired at Stage 3: enabled=%v action=%q", enabled, action)
	}
	if _, err := repo.Rebuild(ctx, 7, false, activation); err == nil || !strings.Contains(err.Error(), "Shadow period") {
		t.Fatalf("write rebuild before Shadow deadline error = %v", err)
	}
}
