package main

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type identityAuditMetadataMatcher struct{}

func (identityAuditMetadataMatcher) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	text := string(raw)
	return strings.Contains(text, `"rule_code":"v2_registration_composite_accounts"`) &&
		strings.Contains(text, `"candidate_account_count":3`) &&
		!strings.Contains(text, "user@example.com") &&
		!strings.Contains(text, "8.8.8.8")
}

func compositeEnforcementTestConfig() IdentityConfig {
	cfg := testIdentityConfig()
	cfg.IPCollectionEnabled = true
	cfg.DeviceCollectionEnabled = true
	cfg.AdminEnabled = true
	cfg.RulesEnabled = true
	cfg.IPDomainEnabled = true
	cfg.DeviceDomainEnabled = true
	cfg.CompositeDomainEnabled = true
	cfg.CompositeEnforcementEnabled = true
	cfg.CurrentScoreEnabled = true
	cfg.CasesEnabled = true
	cfg.ExplainEnabled = true
	cfg.DeliveryEnabled = true
	cfg.GeoSource = "cloudflare_verified"
	cfg.ShadowUntil = time.Now().UTC().Add(24 * time.Hour)
	cfg.QualityMinEvents = 10
	cfg.QualityMinCoverage = 80
	return cfg
}

func expectHealthyCompositeQuality(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT COUNT\(\*\),COUNT\(\*\) FILTER\(WHERE ip_quality_valid\),COUNT\(\*\) FILTER\(WHERE device_quality_valid\) FROM risk_identity_events`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "valid_ip", "valid_device"}).AddRow(100, 100, 100))
	mock.ExpectQuery(`SELECT COALESCE\(\(SELECT COUNT\(DISTINCT user_id\).*risk_shared_network_labels`).
		WillReturnRows(sqlmock.NewRows([]string{"linked_users", "max_network_users"}).AddRow(20, 2))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FILTER\(WHERE status IN \('pending','processing'\)\).*risk_signal_processing_jobs`).
		WillReturnRows(sqlmock.NewRows([]string{"pending", "retry", "failed"}).AddRow(0, 0, 0))
	mock.ExpectQuery(`SELECT COUNT\(\*\),COUNT\(\*\) FILTER\(WHERE gap_until>NOW\(\)\).*risk_delivery_watermarks`).
		WillReturnRows(sqlmock.NewRows([]string{"sources", "gap", "stale", "queue", "dropped", "failed"}).AddRow(1, 0, 0, 0, 0, 0))
}

func TestCompositeRegistrationEnforcementRejectsThresholdCandidateAndAuditsWithoutPII(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := NewIdentityService(compositeEnforcementTestConfig(), NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	expectHealthyCompositeQuality(mock)
	mock.ExpectQuery(`(?s)WITH active_rule AS .*v2_registration_composite_accounts.*risk_shared_network_labels.*mobile_cgnat.*COUNT\(DISTINCT event.user_id\)`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"code", "window_seconds", "threshold", "score", "revision", "account_count", "configured_action"}).
			AddRow("v2_registration_composite_accounts", 600, 3, 90, 2, 2, "reject_candidate"))
	eventKey := "registration-attempt-test"
	auditKey := fmt.Sprintf("identity-enforcement:%x", sha256.Sum256([]byte(eventKey)))
	mock.ExpectExec(`INSERT INTO risk_audit_logs`).
		WithArgs(auditKey, identityAuditMetadataMatcher{}).
		WillReturnResult(sqlmock.NewResult(1, 1))

	decision, err := service.RegistrationDecision(context.Background(), IdentityEventReport{
		EventKey: eventKey, EventType: "registration_attempt", EventClass: "registration", Outcome: "attempt",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Email: "user@example.com",
		ClientIP: "8.8.8.8", IPSource: "remote_addr", ProxyChainValid: true,
		BrowserInstanceID: "browser-instance-test", BrowserCookieStatus: "valid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "reject_candidate" || decision.Mode != "enforce" || decision.Score != 90 || len(decision.RuleCodes) != 1 {
		t.Fatalf("decision = %+v", decision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeRegistrationEnforcementStaysFailOpenWhenQualityIsNotHealthy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := NewIdentityService(compositeEnforcementTestConfig(), NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT COUNT\(\*\),COUNT\(\*\) FILTER\(WHERE ip_quality_valid\),COUNT\(\*\) FILTER\(WHERE device_quality_valid\) FROM risk_identity_events`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "valid_ip", "valid_device"}).AddRow(5, 5, 5))
	mock.ExpectQuery(`SELECT COALESCE\(\(SELECT COUNT\(DISTINCT user_id\).*risk_shared_network_labels`).
		WillReturnRows(sqlmock.NewRows([]string{"linked_users", "max_network_users"}).AddRow(2, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FILTER\(WHERE status IN \('pending','processing'\)\).*risk_signal_processing_jobs`).
		WillReturnRows(sqlmock.NewRows([]string{"pending", "retry", "failed"}).AddRow(0, 0, 0))
	mock.ExpectQuery(`SELECT COUNT\(\*\),COUNT\(\*\) FILTER\(WHERE gap_until>NOW\(\)\).*risk_delivery_watermarks`).
		WillReturnRows(sqlmock.NewRows([]string{"sources", "gap", "stale", "queue", "dropped", "failed"}).AddRow(1, 0, 0, 0, 0, 0))

	decision, err := service.RegistrationDecision(context.Background(), IdentityEventReport{
		EventKey: "quality-fail-open", EventType: "registration_attempt", EventClass: "registration", Outcome: "attempt",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), ClientIP: "8.8.4.4", IPSource: "remote_addr",
		ProxyChainValid: true, BrowserInstanceID: "browser-quality-test", BrowserCookieStatus: "valid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "allow" || decision.Mode != "enforce" {
		t.Fatalf("decision = %+v", decision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeRegistrationEnforcementHonorsConfiguredObserveAction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := NewIdentityService(compositeEnforcementTestConfig(), NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	expectHealthyCompositeQuality(mock)
	mock.ExpectQuery(`(?s)WITH active_rule AS .*v2_registration_composite_accounts.*COUNT\(DISTINCT event.user_id\)`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"code", "window_seconds", "threshold", "score", "revision", "account_count", "configured_action"}).
			AddRow("v2_registration_composite_accounts", 600, 3, 90, 2, 2, "observe"))

	decision, err := service.RegistrationDecision(context.Background(), IdentityEventReport{
		EventKey: "configured-observe", EventType: "registration_attempt", EventClass: "registration", Outcome: "attempt",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), ClientIP: "8.8.4.4", IPSource: "remote_addr",
		ProxyChainValid: true, BrowserInstanceID: "browser-observe-test", BrowserCookieStatus: "valid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "allow" || decision.Mode != "enforce" || decision.Score != 0 {
		t.Fatalf("decision = %+v", decision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityRuleApprovalDoesNotRequireSimulationOrConfirmation(t *testing.T) {
	for _, approval := range []identityRulePublishApproval{
		{},
		{Reason: "reduce false positives"},
		{Reason: "block third candidate"},
	} {
		if err := validateIdentityRuleApproval("v2_registration_composite_accounts", "reject_candidate", approval); err != nil {
			t.Fatalf("direct administrator publish rejected: %v", err)
		}
	}
	if err := validateIdentityRuleApproval("v2_registration_ip_accounts", "review", identityRulePublishApproval{Reason: strings.Repeat("x", 501)}); err == nil {
		t.Fatal("overlong reason was accepted")
	}
}
