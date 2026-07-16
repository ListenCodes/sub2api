package accountmonitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const postgresIntegrationSchema = "account_monitor_integration"

func TestPostgresMigrationAggregationRebuildAndRetention(t *testing.T) {
	databaseURL := os.Getenv("ACCOUNT_MONITOR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ACCOUNT_MONITOR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+postgresIntegrationSchema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+postgresIntegrationSchema); err != nil {
		t.Fatal(err)
	}
	defer adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+postgresIntegrationSchema+" CASCADE")

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", postgresIntegrationSchema)
	parsedURL.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "SET TIME ZONE 'Asia/Shanghai'"); err != nil {
		t.Fatal(err)
	}

	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatalf("second schema application failed: %v", err)
	}
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_schema_migrations", 2)

	now := time.Now().UTC()
	currentBucket := now.Truncate(10 * time.Minute)
	completedBucket := currentBucket.Add(-20 * time.Minute)
	completedAt := completedBucket.Add(time.Minute)
	utcBoundary := time.Date(now.Year(), now.Month(), now.Day()-1, 23, 55, 0, 0, time.UTC)
	groupID := int64(7)
	repository := NewRepository(db)
	repository.now = func() time.Time { return now }

	batch := Batch{
		GroupDimensions: []GroupDimension{{ID: groupID, Name: "OpenAI Production", Platform: "openai", Status: "active", SyncedAt: now}},
		Attempts: []AttemptFact{
			integrationAttempt("attempt-success", completedAt, 101, ResultSucceeded),
			integrationAttempt("attempt-failure", completedAt.Add(time.Minute), 101, ResultFailed),
			integrationAttempt("attempt-utc-boundary", utcBoundary, 202, ResultSucceeded),
		},
		Requests: []RequestFact{
			integrationRequest("request-success", completedAt, &groupID, "gpt-5", AttributionExact, ResultSucceeded),
			integrationRequest("request-failure", completedAt.Add(time.Minute), &groupID, "gpt-5", AttributionEstimated, ResultFailed),
			integrationRequest("request-ungrouped", completedAt.Add(2*time.Minute), nil, "gpt-5", AttributionExact, ResultSucceeded),
			integrationRequest("request-current", currentBucket.Add(time.Minute), &groupID, "gpt-5", AttributionExact, ResultSucceeded),
		},
	}
	if err := repository.CommitBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitBatch(ctx, batch); err != nil {
		t.Fatalf("second collection commit failed: %v", err)
	}
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_attempt_facts", 3)
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_request_facts", 4)
	assertGroupAggregate(t, db, completedBucket, groupID, "gpt-5", 2, 1, 1, 1, 1)
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_group_model_10m WHERE bucket_at=$1", 0, currentBucket)
	displayFrom := time.Unix(completedAt.Unix()-completedAt.Unix()%900, 0).UTC()
	displayTo := displayFrom.Add(15 * time.Minute)
	groupBuckets, err := NewAdminService(repository, nil, time.Second).loadGroupBuckets(
		ctx,
		displayFrom,
		displayTo,
		[]int64{groupID},
		900,
	)
	if err != nil {
		t.Fatal(err)
	}
	groupCard := buildGroupCard(batch.GroupDimensions[0], displayFrom, displayTo, groupBuckets[groupID], 900)
	if groupCard.TotalRequests != 2 || groupCard.Successes != 1 || groupCard.Failures != 1 {
		t.Fatalf("cross-zone group card = %+v, want 2 requests split 1/1", groupCard)
	}

	fifteenMinuteFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	thirtyHourCandidate := fifteenMinuteFrom.Add(24 * time.Hour)
	thirtyHourFrom := time.Unix(thirtyHourCandidate.Unix()-thirtyHourCandidate.Unix()%108000, 0).UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO account_monitor_group_dimensions(group_id,name,platform,status,synced_at)
		VALUES (70,'Fifteen Minute','openai','active',$1),(80,'Thirty Hour','openai','active',$1)
		ON CONFLICT (group_id) DO NOTHING`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO account_monitor_request_facts
		(request_key,occurred_at,group_id,platform,actual_model,model_attribution,result,source_kind,source_id)
		SELECT 'fifteen-'||step,$1::timestamptz + step * interval '15 minutes',70,'openai','fifteen-model','exact','succeeded','usage',step
		FROM generate_series(0,23) AS step`, fifteenMinuteFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO account_monitor_request_facts
		(request_key,occurred_at,group_id,platform,actual_model,model_attribution,result,source_kind,source_id)
		SELECT 'thirty-'||step,$1::timestamptz + step * interval '30 hours',80,'openai','thirty-model',
		       CASE WHEN step < 12 THEN 'exact' ELSE 'estimated' END,
		       CASE WHEN step < 12 THEN 'succeeded' ELSE 'failed' END,'usage',step
		FROM generate_series(0,23) AS step`, thirtyHourFrom); err != nil {
		t.Fatal(err)
	}
	adaptiveService := NewAdminService(repository, nil, time.Second)
	fifteenMinuteBuckets, err := adaptiveService.loadGroupBuckets(ctx, fifteenMinuteFrom, fifteenMinuteFrom.Add(6*time.Hour), []int64{70}, 900)
	if err != nil {
		t.Fatal(err)
	}
	fifteenMinuteCard := buildGroupCard(GroupDimension{ID: 70}, fifteenMinuteFrom, fifteenMinuteFrom.Add(6*time.Hour), fifteenMinuteBuckets[70], 900)
	if len(fifteenMinuteCard.Timeline) != 24 || fifteenMinuteCard.TotalRequests != 24 || !fifteenMinuteCard.Timeline[0].BucketAt.Equal(fifteenMinuteFrom) {
		t.Fatalf("fifteen-minute fixed card = %+v", fifteenMinuteCard)
	}
	thirtyHourResult, err := adaptiveService.groupMonitorGroup(ctx, AdminRequest{
		GroupID: 80, From: thirtyHourFrom, To: thirtyHourFrom.Add(30 * 24 * time.Hour), BucketSeconds: 108000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(thirtyHourResult.Models) != 1 || len(thirtyHourResult.Models[0].Timeline) != 24 ||
		thirtyHourResult.Group.TotalRequests != 24 || thirtyHourResult.Models[0].Successes != 12 ||
		thirtyHourResult.Models[0].ExactModelRequests != 12 || thirtyHourResult.Models[0].EstimatedModelRequests != 12 ||
		!thirtyHourResult.Models[0].Timeline[0].BucketAt.Equal(thirtyHourFrom) {
		t.Fatalf("thirty-hour fixed detail = %+v", thirtyHourResult)
	}

	var bucketDate string
	if err := db.QueryRowContext(ctx, "SELECT bucket_date::text FROM account_monitor_account_daily WHERE account_id=202").Scan(&bucketDate); err != nil {
		t.Fatal(err)
	}
	if want := utcBoundary.Format("2006-01-02"); bucketDate != want {
		t.Fatalf("UTC daily bucket = %s, want %s", bucketDate, want)
	}

	lateBatch := Batch{Requests: []RequestFact{
		integrationRequest("request-late", completedAt.Add(3*time.Minute), &groupID, "gpt-5", AttributionExact, ResultSucceeded),
	}}
	if err := repository.CommitRebuildBatch(ctx, lateBatch); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitRebuildBatch(ctx, lateBatch); err != nil {
		t.Fatalf("overlapping rebuild rerun failed: %v", err)
	}
	assertGroupAggregate(t, db, completedBucket, groupID, "gpt-5", 3, 2, 1, 2, 1)

	nextBucket := completedBucket.Add(10 * time.Minute)
	nextBatch := Batch{Requests: []RequestFact{
		integrationRequest("request-next-segment", nextBucket.Add(time.Minute), &groupID, "gpt-5", AttributionExact, ResultSucceeded),
	}}
	if err := repository.CommitRebuildBatch(ctx, nextBatch); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitRebuildBatch(ctx, nextBatch); err != nil {
		t.Fatalf("second rebuild segment rerun failed: %v", err)
	}
	assertGroupAggregate(t, db, nextBucket, groupID, "gpt-5", 1, 1, 0, 1, 0)

	jobFrom := completedBucket.Add(-24 * time.Hour)
	jobTo := completedBucket
	job, err := repository.CreateRebuildJob(ctx, jobFrom, jobTo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateRebuildJob(ctx, jobFrom.Add(time.Hour), jobTo.Add(time.Hour), 1); !errors.Is(err, ErrRebuildOverlap) {
		t.Fatalf("overlapping rebuild error = %v, want %v", err, ErrRebuildOverlap)
	}
	if err := repository.FinishRebuildJob(ctx, job.ID, 5, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateRebuildJob(ctx, jobFrom, jobTo, 1); err != nil {
		t.Fatalf("completed rebuild should not block the same range: %v", err)
	}

	oldAt := currentBucket.Add(-100 * 24 * time.Hour).Add(time.Minute)
	oldGroupID := int64(9)
	oldBatch := Batch{
		Attempts: []AttemptFact{integrationAttempt("attempt-old", oldAt, 303, ResultSucceeded)},
		Requests: []RequestFact{integrationRequest("request-old", oldAt, &oldGroupID, "legacy-model", AttributionExact, ResultSucceeded)},
	}
	if err := repository.CommitRebuildBatch(ctx, oldBatch); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_group_model_10m WHERE group_id=$1", 1, oldGroupID)
	if err := repository.Cleanup(ctx, now, 90*24*time.Hour, 365*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_request_facts WHERE request_key='request-old'", 0)
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_attempt_facts WHERE event_key='attempt-old'", 0)
	assertDatabaseCount(t, db, "SELECT COUNT(*) FROM account_monitor_group_model_10m WHERE group_id=$1", 0, oldGroupID)
}

func TestPostgresSourceViewsAndRestrictedRole(t *testing.T) {
	databaseURL := os.Getenv("ACCOUNT_MONITOR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ACCOUNT_MONITOR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	const databaseName = "account_monitor_source_integration"
	if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+databaseName+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+databaseName); err != nil {
		t.Fatal(err)
	}
	defer adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+databaseName+" WITH (FORCE)")

	ownerURL := integrationDatabaseURL(t, databaseURL, databaseName, "postgres", "")
	ownerDB, err := sql.Open("postgres", ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer ownerDB.Close()
	if _, err := ownerDB.ExecContext(ctx, sourceIntegrationSchemaSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerDB.ExecContext(ctx, sourceIntegrationLegacyViewsSQL); err != nil {
		t.Fatal(err)
	}
	viewSQL, err := os.ReadFile("sql/main_source_views.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerDB.ExecContext(ctx, string(viewSQL)); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerDB.ExecContext(ctx, string(viewSQL)); err != nil {
		t.Fatalf("second source-view installation failed: %v", err)
	}
	if _, err := ownerDB.ExecContext(ctx, sourceIntegrationSeedSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerDB.ExecContext(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='extensions_self_monitor') THEN
			CREATE ROLE extensions_self_monitor LOGIN PASSWORD 'integration-password';
		END IF;
	END $$;
	ALTER ROLE extensions_self_monitor LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD 'integration-password';
	GRANT extensions_self_monitor_ro TO extensions_self_monitor;`); err != nil {
		t.Fatal(err)
	}

	restrictedURL := integrationDatabaseURL(t, databaseURL, databaseName, "extensions_self_monitor", "integration-password")
	restrictedDB, err := sql.Open("postgres", restrictedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer restrictedDB.Close()
	if err := restrictedDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.usage_source", 1)
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.usage_source WHERE group_id=7", 1)
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.error_source WHERE group_id=7", 1)
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.group_dimension WHERE id=7", 1)
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.account_group_dimension WHERE account_id=101 AND group_id=7", 1)
	assertDatabaseCount(t, restrictedDB, "SELECT COUNT(*) FROM extensions_self_ro.account_group_dimension WHERE account_id=101 AND group_rate_multiplier=1.5", 1)

	var maskedPrefix string
	if err := restrictedDB.QueryRowContext(ctx, "SELECT masked_prefix FROM extensions_self_ro.api_key_dimension WHERE id=70").Scan(&maskedPrefix); err != nil {
		t.Fatal(err)
	}
	if maskedPrefix != "secret-a***" {
		t.Fatalf("masked prefix = %q, want %q", maskedPrefix, "secret-a***")
	}
	var errorLength, upstreamLength int
	if err := restrictedDB.QueryRowContext(ctx, `SELECT length(error_message),length(upstream_errors->0->>'message') FROM extensions_self_ro.error_source WHERE id=44`).Scan(&errorLength, &upstreamLength); err != nil {
		t.Fatal(err)
	}
	if errorLength != 512 || upstreamLength != 512 {
		t.Fatalf("safe error lengths = (%d,%d), want (512,512)", errorLength, upstreamLength)
	}
	if _, err := restrictedDB.ExecContext(ctx, "SELECT key FROM public.api_keys"); err == nil {
		t.Fatal("restricted role unexpectedly read public.api_keys.key")
	}
	if _, err := restrictedDB.ExecContext(ctx, "SELECT credentials FROM public.accounts"); err == nil {
		t.Fatal("restricted role unexpectedly read public.accounts.credentials")
	}

	var login, bypassRLS, superuser bool
	if err := ownerDB.QueryRowContext(ctx, "SELECT rolcanlogin,rolbypassrls,rolsuper FROM pg_roles WHERE rolname='extensions_self_monitor'").Scan(&login, &bypassRLS, &superuser); err != nil {
		t.Fatal(err)
	}
	if !login || bypassRLS || superuser {
		t.Fatalf("restricted role flags = login:%t bypassrls:%t superuser:%t", login, bypassRLS, superuser)
	}
	var groupCanLogin bool
	if err := ownerDB.QueryRowContext(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname='extensions_self_monitor_ro'").Scan(&groupCanLogin); err != nil {
		t.Fatal(err)
	}
	if groupCanLogin {
		t.Fatal("extensions_self_monitor_ro must remain NOLOGIN")
	}
}

const sourceIntegrationSchemaSQL = `
CREATE TABLE public.accounts (id BIGINT PRIMARY KEY,parent_account_id BIGINT,name TEXT,platform TEXT,status TEXT,schedulable BOOLEAN,deleted_at TIMESTAMPTZ,credentials JSONB);
CREATE TABLE public.usage_logs (id BIGINT PRIMARY KEY,created_at TIMESTAMPTZ,user_id BIGINT,api_key_id BIGINT,account_id BIGINT,group_id BIGINT,request_id TEXT,model TEXT,requested_model TEXT,upstream_model TEXT,input_tokens BIGINT,output_tokens BIGINT,cache_creation_tokens BIGINT,cache_read_tokens BIGINT,total_cost NUMERIC,actual_cost NUMERIC,account_rate_multiplier NUMERIC,duration_ms BIGINT,request_type SMALLINT,stream BOOLEAN,image_count INT,image_size TEXT,image_input_size TEXT,image_output_size TEXT,image_size_breakdown JSONB,video_count INT,video_resolution TEXT,video_duration_seconds INT);
CREATE TABLE public.ops_error_logs (id BIGINT PRIMARY KEY,created_at TIMESTAMPTZ,request_id TEXT,client_request_id TEXT,user_id BIGINT,api_key_id BIGINT,account_id BIGINT,group_id BIGINT,platform TEXT,model TEXT,requested_model TEXT,upstream_model TEXT,request_type SMALLINT,stream BOOLEAN,error_phase TEXT,error_type TEXT,error_source TEXT,error_owner TEXT,status_code INT,upstream_status_code INT,provider_error_code TEXT,provider_error_type TEXT,network_error_type TEXT,duration_ms BIGINT,error_message TEXT,upstream_error_message TEXT,upstream_errors JSONB);
CREATE TABLE public.users (id BIGINT PRIMARY KEY,email TEXT,username TEXT,status TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE public.api_keys (id BIGINT PRIMARY KEY,user_id BIGINT,name TEXT,key TEXT,status TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE public.groups (id BIGINT PRIMARY KEY,name TEXT,platform TEXT,status TEXT,rate_multiplier NUMERIC,deleted_at TIMESTAMPTZ);
CREATE TABLE public.account_groups (account_id BIGINT NOT NULL,group_id BIGINT NOT NULL,PRIMARY KEY(account_id,group_id));`

const sourceIntegrationLegacyViewsSQL = `
CREATE SCHEMA extensions_self_ro;
CREATE VIEW extensions_self_ro.usage_source WITH (security_barrier = true) AS
SELECT u.id,u.created_at,u.user_id,u.api_key_id,u.account_id,a.parent_account_id,
       u.request_id,a.platform,u.model,u.requested_model,u.upstream_model,
       u.input_tokens,u.output_tokens,u.cache_creation_tokens,u.cache_read_tokens,
       u.total_cost,u.actual_cost,u.account_rate_multiplier,u.duration_ms,
       u.request_type,u.stream,u.image_count,u.image_size,u.image_input_size,
       u.image_output_size,u.image_size_breakdown,u.video_count,u.video_resolution,
       u.video_duration_seconds
FROM public.usage_logs AS u
JOIN public.accounts AS a ON a.id = u.account_id;

CREATE VIEW extensions_self_ro.error_source WITH (security_barrier = true) AS
SELECT o.id,o.created_at,o.request_id,o.client_request_id,o.user_id,o.api_key_id,
       o.account_id,o.platform,o.model,o.requested_model,o.upstream_model,
       o.request_type,o.stream,o.error_phase,o.error_type,o.error_source,
       o.error_owner,o.status_code,o.upstream_status_code,o.provider_error_code,
       o.provider_error_type,o.network_error_type,o.duration_ms,
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
       ), '[]'::jsonb) AS upstream_errors
FROM public.ops_error_logs AS o;`

const sourceIntegrationSeedSQL = `
INSERT INTO public.accounts VALUES (101,NULL,'OpenAI Primary','openai','active',TRUE,NULL,'{"access_token":"secret"}'::jsonb);
INSERT INTO public.usage_logs (id,created_at,user_id,api_key_id,account_id,group_id,request_id,model,requested_model,upstream_model) VALUES (1,NOW(),7,70,101,7,'request-1','gpt-5','gpt-5','gpt-5');
INSERT INTO public.ops_error_logs (id,created_at,request_id,user_id,api_key_id,account_id,group_id,platform,error_message,upstream_error_message,upstream_errors) VALUES (44,NOW(),'request-1',7,70,101,7,'openai',repeat('e',600),repeat('u',600),jsonb_build_array(jsonb_build_object('message',repeat('m',600),'detail',repeat('d',600))));
INSERT INTO public.users VALUES (7,'alice@example.test','alice','active',NULL);
INSERT INTO public.api_keys VALUES (70,7,'QA Key','secret-abcdef','active',NULL);
INSERT INTO public.groups VALUES (7,'OpenAI Production','openai','active',1.5,NULL);
INSERT INTO public.account_groups VALUES (101,7);`

func integrationDatabaseURL(t *testing.T, databaseURL, databaseName, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	if password == "" {
		if parsed.User != nil && parsed.User.Username() == username {
			if existingPassword, ok := parsed.User.Password(); ok {
				parsed.User = url.UserPassword(username, existingPassword)
				return parsed.String()
			}
		}
		parsed.User = url.User(username)
	} else {
		parsed.User = url.UserPassword(username, password)
	}
	return parsed.String()
}

func TestIntegrationDatabaseURLPreservesOwnerPassword(t *testing.T) {
	result := integrationDatabaseURL(t, "postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable", "monitor", "postgres", "")
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := parsed.User.Password()
	if !ok || password != "secret" {
		t.Fatalf("owner password = %q, present = %t; want preserved password", password, ok)
	}
	if parsed.Path != "/monitor" {
		t.Fatalf("database path = %q, want /monitor", parsed.Path)
	}
}

func integrationAttempt(key string, at time.Time, accountID int64, result Result) AttemptFact {
	return AttemptFact{
		EventKey: key, RequestKey: key, AttemptedAt: at, AccountID: accountID,
		Platform: "openai", ActualModel: "gpt-5", ModelAttribution: AttributionExact,
		UserID: 1, APIKeyID: 2, RequestType: 1, Result: result,
		ErrorCategory: ErrorCategory("upstream"), StatusCode: 200, UpstreamStatusCode: 200,
		InputTokens: 10, OutputTokens: 5, UserCost: 0.1, AccountCost: 0.08,
		DurationMS: 100, IdentityQuality: IdentityExact, SourceKind: "usage", SourceID: accountID,
	}
}

func integrationRequest(key string, at time.Time, groupID *int64, model string, attribution AttributionQuality, result Result) RequestFact {
	return RequestFact{
		RequestKey: key, OccurredAt: at, UserID: 1, APIKeyID: 2, AccountID: 101,
		GroupID: groupID, Platform: "openai", ActualModel: model, ModelAttribution: attribution,
		RequestType: 1, Result: result, ErrorCategory: ErrorCategory("upstream"), StatusCode: 200,
		InputTokens: 10, OutputTokens: 5, UserCost: 0.1, AccountCost: 0.08,
		DurationMS: 100, IdentityQuality: IdentityExact, SourceKind: "usage", SourceID: int64(len(key)),
	}
}

func assertGroupAggregate(t *testing.T, db *sql.DB, bucket time.Time, groupID int64, model string, total, successes, failures, exact, estimated int64) {
	t.Helper()
	var gotTotal, gotSuccesses, gotFailures, gotExact, gotEstimated int64
	err := db.QueryRow(`SELECT total_requests,successes,failures,exact_model_requests,estimated_model_requests
		FROM account_monitor_group_model_10m WHERE bucket_at=$1 AND group_id=$2 AND actual_model=$3`, bucket, groupID, model).
		Scan(&gotTotal, &gotSuccesses, &gotFailures, &gotExact, &gotEstimated)
	if err != nil {
		t.Fatal(err)
	}
	if gotTotal != total || gotSuccesses != successes || gotFailures != failures || gotExact != exact || gotEstimated != estimated {
		t.Fatalf("aggregate = (%d,%d,%d,%d,%d), want (%d,%d,%d,%d,%d)", gotTotal, gotSuccesses, gotFailures, gotExact, gotEstimated, total, successes, failures, exact, estimated)
	}
	if gotTotal != gotSuccesses+gotFailures {
		t.Fatalf("total %d != successes %d + failures %d", gotTotal, gotSuccesses, gotFailures)
	}
}

func assertDatabaseCount(t *testing.T, db *sql.DB, query string, want int64, args ...any) {
	t.Helper()
	var got int64
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal(fmt.Sprintf("count for %q = %d, want %d", query, got, want))
	}
}
