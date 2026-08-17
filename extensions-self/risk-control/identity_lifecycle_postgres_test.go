package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func enabledIdentityTestConfig() IdentityConfig {
	cfg := testIdentityConfig()
	cfg.IPCollectionEnabled = true
	cfg.DeviceCollectionEnabled = true
	cfg.RulesEnabled = true
	cfg.ShadowUntil = time.Now().UTC().Add(15 * 24 * time.Hour)
	cfg.CurrentScoreEnabled = true
	cfg.CasesEnabled = true
	cfg.ExplainEnabled = true
	cfg.IPDomainEnabled = true
	cfg.DeviceDomainEnabled = true
	cfg.CompositeDomainEnabled = true
	cfg.QualityMinEvents = 1
	cfg.QualityMinCoverage = 1
	cfg.QualityMinUsers = 1000
	cfg.QualityMaxIPShare = 100
	return cfg
}

func registrationIdentityReport(key string, userID int64, occurredAt time.Time, ip, browser string) IdentityEventReport {
	return IdentityEventReport{
		EventKey: key, EventType: "registration_success", EventClass: "registration", Outcome: "success",
		OccurredAt: occurredAt.UTC().Format(time.RFC3339Nano), UserID: userID, ClientIP: ip,
		IPSource: "remote_addr", ProxyChainValid: true, BrowserInstanceID: browser, BrowserCookieStatus: "valid",
	}
}

func TestIdentitySignalLifecycleCasesAndSharedNetworkPostgres(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := enabledIdentityTestConfig()
	service, err := NewIdentityService(cfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	for index := range 5 {
		if _, err := service.Ingest(ctx, registrationIdentityReport(fmt.Sprintf("lifecycle-registration-%d", index), int64(1000+index), base, "8.8.4.4", "shared-browser")); err != nil {
			t.Fatalf("registration %d: %v", index, err)
		}
	}

	var activeSignals, versionedSignals, snapshottedDecisions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE rule_version_id IS NOT NULL) FROM risk_identity_signals WHERE user_id=1004 AND status='active'`).Scan(&activeSignals, &versionedSignals); err != nil {
		t.Fatal(err)
	}
	if activeSignals != 3 || versionedSignals != activeSignals {
		t.Fatalf("active/versioned signals = %d/%d, want 3/3", activeSignals, versionedSignals)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_decisions WHERE user_id=1004 AND status='active' AND evidence_snapshot ? 'rule_snapshot'`).Scan(&snapshottedDecisions); err != nil {
		t.Fatal(err)
	}
	if snapshottedDecisions != 3 {
		t.Fatalf("immutable decision snapshots = %d, want 3", snapshottedDecisions)
	}
	var score, historical int
	if err := db.QueryRowContext(ctx, `SELECT overall_score FROM risk_identity_user_summaries WHERE user_id=1004`).Scan(&score); err != nil || score != 90 {
		t.Fatalf("current score = %d, error=%v", score, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT historical_max_score FROM risk_review_cases WHERE user_id=1004 AND status='pending'`).Scan(&historical); err != nil || historical != 90 {
		t.Fatalf("pending case historical score = %d, error=%v", historical, err)
	}
	summary, err := service.Summary(ctx, 1004)
	if err != nil {
		t.Fatalf("identity summary: %v", err)
	}
	if summary.OverallScore != 90 || summary.HistoricalMaxScore != 90 || summary.HistoricalSignalCount != 3 {
		t.Fatalf("identity summary = current:%d historical:%d signals:%d, want 90/90/3", summary.OverallScore, summary.HistoricalMaxScore, summary.HistoricalSignalCount)
	}

	var firstCaseID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM risk_review_cases WHERE user_id=1004 AND status='pending'`).Scan(&firstCaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.repo.ClaimReviewCase(ctx, firstCaseID, 7); err != nil {
		t.Fatal(err)
	}
	if err := service.repo.AddReviewFeedback(ctx, firstCaseID, 7, "confirmed_abuse", "fixture review confirmed abuse without enforcing an account action"); err != nil {
		t.Fatal(err)
	}
	var feedbackActiveSignals, feedbackActiveDecisions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_signals WHERE user_id=1004 AND status='active'`).Scan(&feedbackActiveSignals); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_decisions WHERE user_id=1004 AND status='active'`).Scan(&feedbackActiveDecisions); err != nil {
		t.Fatal(err)
	}
	if feedbackActiveSignals != 3 || feedbackActiveDecisions != 3 {
		t.Fatalf("review feedback changed evidence lifecycle: signals=%d decisions=%d", feedbackActiveSignals, feedbackActiveDecisions)
	}
	if _, err := service.Ingest(ctx, registrationIdentityReport("lifecycle-registration-reopen", 1004, base, "8.8.4.4", "shared-browser")); err != nil {
		t.Fatal(err)
	}
	var resolvedCases, pendingCases int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE status='resolved'),COUNT(*) FILTER(WHERE status='pending') FROM risk_review_cases WHERE user_id=1004 AND signal_family='registration_identity'`).Scan(&resolvedCases, &pendingCases); err != nil {
		t.Fatal(err)
	}
	if resolvedCases != 1 || pendingCases != 1 {
		t.Fatalf("resolved/pending cases after new evidence = %d/%d", resolvedCases, pendingCases)
	}

	var networkID int64
	if err := db.QueryRowContext(ctx, `SELECT network_identity_id FROM risk_identity_events WHERE event_key='lifecycle-registration-reopen'`).Scan(&networkID); err != nil {
		t.Fatal(err)
	}
	if err := service.repo.LabelSharedNetwork(ctx, networkID, 7, "company", "known shared office egress"); err != nil {
		t.Fatal(err)
	}
	var activeLabeledIPSignals, activeLabeledIPDecisions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_signals WHERE network_identity_id=$1 AND domain='ip' AND status='active'`, networkID).Scan(&activeLabeledIPSignals); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_decisions decision JOIN risk_identity_signals signal ON signal.decision_id=decision.decision_id WHERE signal.network_identity_id=$1 AND signal.domain='ip' AND decision.status='active'`, networkID).Scan(&activeLabeledIPDecisions); err != nil {
		t.Fatal(err)
	}
	if activeLabeledIPSignals != 0 || activeLabeledIPDecisions != 0 {
		t.Fatalf("shared-network label retained current IP risk: signals=%d decisions=%d", activeLabeledIPSignals, activeLabeledIPDecisions)
	}
	if _, err := service.Ingest(ctx, registrationIdentityReport("lifecycle-registration-labeled", 1006, base, "8.8.4.4", "shared-browser")); err != nil {
		t.Fatal(err)
	}
	var labeledIPSignals int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_signals signal JOIN risk_identity_events event ON event.id=signal.event_id WHERE event.event_key='lifecycle-registration-labeled' AND signal.domain='ip'`).Scan(&labeledIPSignals); err != nil || labeledIPSignals != 0 {
		t.Fatalf("safe shared-network IP signals = %d, error=%v", labeledIPSignals, err)
	}

	if _, err := service.repo.DisableIdentityRule(ctx, "v2_registration_composite_accounts", "release fixture disable", 7); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var reactivated int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_signals WHERE rule_code='v2_registration_composite_accounts' AND status='active'`).Scan(&reactivated); err != nil || reactivated != 0 {
		t.Fatalf("disabled signals reactivated after schema replay = %d, error=%v", reactivated, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE risk_identity_signals SET active_until=NOW()-interval '1 second' WHERE status='active'`); err != nil {
		t.Fatal(err)
	}
	if err := service.repo.ExpireSignals(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT overall_score FROM risk_identity_user_summaries WHERE user_id=1004`).Scan(&score); err != nil || score != 0 {
		t.Fatalf("expired current score = %d, error=%v", score, err)
	}
	var caseStatus string
	if err := db.QueryRowContext(ctx, `SELECT status,current_score,historical_max_score FROM risk_review_cases WHERE user_id=1004 AND status='pending'`).Scan(&caseStatus, &score, &historical); err != nil {
		t.Fatal(err)
	}
	if caseStatus != "pending" || score != 0 || historical != 90 {
		t.Fatalf("expired case = status:%s current:%d historical:%d", caseStatus, score, historical)
	}
}

func TestSchemaV4MakesUnversionedV2SignalsHistoricalPostgres(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO risk_identity_events(event_key,event_type,event_class,outcome,user_id,occurred_at) VALUES('pre-v4-signal','registration_success','registration','success',3001,NOW()) RETURNING id`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM risk_schema_migrations WHERE version=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE risk_identity_signals DROP CONSTRAINT IF EXISTS risk_identity_signals_active_version_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_identity_signals(event_id,user_id,domain,rule_code,rule_revision,signal_family,score,evidence_count,evidence,evidence_snapshot,status,occurred_at) VALUES($1,3001,'ip','v2_registration_ip_accounts',1,'registration_identity',60,5,'{}','{}','active',NOW())`, eventID); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var status string
	var ruleVersionID sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT status,rule_version_id FROM risk_identity_signals WHERE event_id=$1`, eventID).Scan(&status, &ruleVersionID); err != nil {
		t.Fatal(err)
	}
	if status != "superseded" || !ruleVersionID.Valid {
		t.Fatalf("pre-v4 signal migrated as status=%q rule_version_id=%v", status, ruleVersionID)
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_signals signal JOIN risk_identity_rules rule ON rule.code=signal.rule_code AND rule.revision=signal.rule_revision WHERE signal.user_id=3001 AND signal.status='active'`).Scan(&current); err != nil || current != 0 {
		t.Fatalf("pre-v4 current signal count = %d, error=%v", current, err)
	}
}

func TestIdentityAggregatesOAuthAndPersistentRetryPostgres(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := enabledIdentityTestConfig()
	service, err := NewIdentityService(cfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	for index, userID := range []int64{202, 203} {
		_, err := service.Ingest(ctx, IdentityEventReport{
			EventKey: fmt.Sprintf("api-observation-%d", userID), EventType: "api_request", EventClass: identityEventAPI, Outcome: "success",
			OccurredAt: base.Format(time.RFC3339Nano), UserID: userID,
			ClientIP: []string{"1.1.1.1", "9.9.9.9"}[index], IPSource: "remote_addr", ProxyChainValid: true, APIKeyID: 77,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	effects, err := service.repo.RuleEffects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var apiEffect RiskRuleEffect
	for _, effect := range effects {
		if effect.RuleCode == "v2_api_client_accounts" {
			apiEffect = effect
		}
	}
	if apiEffect.HitEvents != 2 || apiEffect.UniqueSubjects != 2 || len(apiEffect.SampleUserIDs) != 2 {
		t.Fatalf("API observation effect = %+v", apiEffect)
	}
	associated, total, err := service.repo.ListAssociatedUsers(ctx, 202, 20, 0)
	if err != nil || total != 1 || len(associated) != 1 || associated[0].UserID != 203 || associated[0].SharedAPIClientCount != 1 || associated[0].Concurrent || len(associated[0].SourceEventIDs) != 0 {
		t.Fatalf("API association = total:%d items:%+v error:%v", total, associated, err)
	}

	if _, err := service.Ingest(ctx, IdentityEventReport{
		EventKey: "oauth-login-count", EventType: "oauth_success", EventClass: "oauth", Outcome: "success",
		OccurredAt: base.Format(time.RFC3339Nano), UserID: 204, ClientIP: "4.2.2.2", IPSource: "remote_addr",
		ProxyChainValid: true, BrowserInstanceID: "oauth-browser", BrowserCookieStatus: "valid",
	}); err != nil {
		t.Fatal(err)
	}
	var oauthLogins int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(login_success_count),0) FROM risk_user_ip_links WHERE user_id=204`).Scan(&oauthLogins); err != nil || oauthLogins != 1 {
		t.Fatalf("OAuth login count = %d, error=%v", oauthLogins, err)
	}

	for index := range 5 {
		if _, err := service.Ingest(ctx, IdentityEventReport{
			EventKey: fmt.Sprintf("email-retry-%d", index), EventType: "registration_attempt", EventClass: "registration", Outcome: "attempt",
			OccurredAt: base.Format(time.RFC3339Nano), Email: "same@example.test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	var emailSignals, emailScore, emailSummaries int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(score),0) FROM risk_identity_signals WHERE rule_code='v2_registration_email_retries'`).Scan(&emailSignals, &emailScore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_identity_user_summaries WHERE user_id=0`).Scan(&emailSummaries); err != nil {
		t.Fatal(err)
	}
	if emailSignals != 1 || emailScore != 0 || emailSummaries != 0 {
		t.Fatalf("email observation = signals:%d score:%d summaries:%d", emailSignals, emailScore, emailSummaries)
	}

	retryCfg := cfg
	retryCfg.DeliveryEnabled = true
	retryService, err := NewIdentityService(retryCfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	_, ingestErr := retryService.Ingest(ctx, registrationIdentityReport("delivery-retry", 205, base, "208.67.222.222", "delivery-browser"))
	if !errors.Is(ingestErr, ErrIdentitySignalProcessing) {
		t.Fatalf("delivery-gap ingest error = %v", ingestErr)
	}
	_, secondIngestErr := retryService.Ingest(ctx, registrationIdentityReport("delivery-retry-peer", 206, base, "208.67.220.220", "delivery-browser-peer"))
	if !errors.Is(secondIngestErr, ErrIdentitySignalProcessing) {
		t.Fatalf("second delivery-gap ingest error = %v", secondIngestErr)
	}
	var jobStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM risk_signal_processing_jobs job JOIN risk_identity_events event ON event.id=job.event_id WHERE event.event_key='delivery-retry'`).Scan(&jobStatus); err != nil || jobStatus != "retry" {
		t.Fatalf("persisted retry status = %q, error=%v", jobStatus, err)
	}
	now := time.Now().UTC()
	startedAt := now.Add(-time.Minute).Format(time.RFC3339Nano)
	accepted, err := retryService.repo.RecordDeliveryHeartbeat(ctx, IdentityDeliveryReport{Source: "main-backend", Generation: "generation-a", StartedAt: startedAt, Sequence: 1, Enqueued: 1, Succeeded: 1, GeneratedAt: now.Format(time.RFC3339Nano)})
	if err != nil || !accepted {
		t.Fatalf("healthy heartbeat = accepted:%v error:%v", accepted, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE risk_signal_processing_jobs SET next_attempt_at=NOW() WHERE status='retry'`); err != nil {
		t.Fatal(err)
	}
	if err := retryService.repo.ProcessSignalJob(ctx, "delivery-retry", retryCfg); err != nil {
		t.Fatal(err)
	}
	if err := retryService.repo.ProcessSignalJob(ctx, "delivery-retry-peer", retryCfg); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM risk_signal_processing_jobs job JOIN risk_identity_events event ON event.id=job.event_id WHERE event.event_key='delivery-retry'`).Scan(&jobStatus); err != nil || jobStatus != "completed" {
		t.Fatalf("recovered job status = %q, error=%v", jobStatus, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM risk_signal_processing_jobs job JOIN risk_identity_events event ON event.id=job.event_id WHERE event.event_key='delivery-retry-peer'`).Scan(&jobStatus); err != nil || jobStatus != "completed" {
		t.Fatalf("recovered peer job status = %q, error=%v", jobStatus, err)
	}
	accepted, err = retryService.repo.RecordDeliveryHeartbeat(ctx, IdentityDeliveryReport{Source: "main-backend", Generation: "generation-a", StartedAt: startedAt, Sequence: 2, Enqueued: 2, Succeeded: 1, Failed: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || !accepted {
		t.Fatalf("gap heartbeat = accepted:%v error:%v", accepted, err)
	}
	health, err := retryService.Health(ctx)
	if err != nil || health.Domains["ip"] != "not_evaluable" || health.Delivery["gap_sources"] != int64(1) {
		t.Fatalf("delivery gap health = %+v, error=%v", health, err)
	}
	accepted, err = retryService.repo.RecordDeliveryHeartbeat(ctx, IdentityDeliveryReport{Source: "main-backend", Generation: "generation-old", StartedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), Sequence: 99, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || accepted {
		t.Fatalf("older generation heartbeat = accepted:%v error:%v", accepted, err)
	}
	accepted, err = retryService.repo.RecordDeliveryHeartbeat(ctx, IdentityDeliveryReport{Source: "main-backend", Generation: "generation-b", StartedAt: now.Add(time.Second).Format(time.RFC3339Nano), Sequence: 1, Enqueued: 1, Succeeded: 1, GeneratedAt: now.Add(time.Second).Format(time.RFC3339Nano)})
	if err != nil || !accepted {
		t.Fatalf("new generation heartbeat = accepted:%v error:%v", accepted, err)
	}
}

func TestIdentityRebuildKeepsVersionedEvidenceAndCasesPostgres(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := enabledIdentityTestConfig()
	service, err := NewIdentityService(cfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	for index := range 3 {
		if _, err := service.Ingest(ctx, registrationIdentityReport(fmt.Sprintf("rebuild-registration-%d", index), int64(3000+index), base, "8.8.8.8", "rebuild-browser")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_identity_shadow_activation(singleton,started_at,shadow_until) VALUES(1,NOW()-interval '15 days',NOW()-interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	dryRun, err := service.repo.Rebuild(ctx, 7, true, cfg)
	if err != nil || dryRun.V2Signals == 0 {
		t.Fatalf("rebuild Dry Run = %+v, error=%v", dryRun, err)
	}
	if _, err := service.repo.Rebuild(ctx, 7, false, cfg); err == nil || !strings.Contains(err.Error(), cfg.ShadowUntil.UTC().Format(time.RFC3339)) {
		t.Fatalf("rebuild before configured Shadow deadline error = %v", err)
	}
	cfg.ShadowUntil = base.Add(-time.Hour)
	result, err := service.repo.Rebuild(ctx, 7, false, cfg)
	if err != nil || result.ApprovedDryRunID != dryRun.ID {
		t.Fatalf("rebuild apply = %+v, error=%v", result, err)
	}
	var missingVersions, missingRuleSnapshots, activeCases, caseEvidence int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE rule_version_id IS NULL),COUNT(*) FILTER(WHERE NOT (evidence_snapshot ? 'rule_snapshot')) FROM risk_identity_signals`).Scan(&missingVersions, &missingRuleSnapshots); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_review_cases WHERE status IN ('pending','in_review')`).Scan(&activeCases); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_case_evidence WHERE signal_id IS NOT NULL`).Scan(&caseEvidence); err != nil {
		t.Fatal(err)
	}
	if missingVersions != 0 || missingRuleSnapshots != 0 || activeCases == 0 || caseEvidence == 0 {
		t.Fatalf("rebuild version/evidence/cases = missing_version:%d missing_snapshot:%d cases:%d evidence:%d", missingVersions, missingRuleSnapshots, activeCases, caseEvidence)
	}
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var tablesRemain int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('risk_decisions','risk_review_cases','risk_case_evidence','risk_review_feedback','risk_rule_versions','risk_shared_network_labels','risk_signal_processing_jobs','risk_delivery_watermarks')`).Scan(&tablesRemain); err != nil || tablesRemain != 8 {
		t.Fatalf("additive rollback tables = %d, error=%v", tablesRemain, err)
	}
}
