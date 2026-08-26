package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func validTestRule() Rule {
	return Rule{
		Code:          "custom_login_failure",
		Name:          "自定义登录失败",
		Description:   "短时间内连续登录失败",
		EventTypes:    []string{"login_failure"},
		CountStrategy: countStrategyUserEvents,
		Enabled:       true,
		WindowSeconds: 300,
		Threshold:     5,
		Score:         80,
		RiskLevel:     "high",
		Action:        "review",
	}
}

func TestValidateRuleConfigRejectsUnsafeAndInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		want string
	}{
		{name: "unsafe code", rule: Rule{Code: "../rules", Name: "名称", EventTypes: []string{"login_failure"}, WindowSeconds: 1, Threshold: 1, Score: 1, RiskLevel: "low", Action: "observe"}, want: "code"},
		{name: "missing name", rule: Rule{Code: "safe_code", EventTypes: []string{"login_failure"}, WindowSeconds: 1, Threshold: 1, Score: 1, RiskLevel: "low", Action: "observe"}, want: "name"},
		{name: "unknown event", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"unknown"}, WindowSeconds: 1, Threshold: 1, Score: 1, RiskLevel: "low", Action: "observe"}, want: "event type"},
		{name: "normal api observation", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"api_request"}, WindowSeconds: 1, Threshold: 1, Score: 0, RiskLevel: "low", Action: "observe"}, want: "event type"},
		{name: "unknown count strategy", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"login_failure"}, CountStrategy: "global_magic", WindowSeconds: 1, Threshold: 1, Score: 1, RiskLevel: "low", Action: "observe"}, want: "count strategy"},
		{name: "missing count strategy", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"login_failure"}, WindowSeconds: 1, Threshold: 1, Score: 1, RiskLevel: "low", Action: "observe"}, want: "count strategy"},
		{name: "invalid score", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"login_failure"}, CountStrategy: countStrategyUserEvents, WindowSeconds: 1, Threshold: 1, Score: 101, RiskLevel: "low", Action: "observe"}, want: "score"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRuleConfig(tc.rule)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateRuleConfig() error = %v, want text %q", err, tc.want)
			}
		})
	}
}

func TestIsRetiredV1IdentityRule(t *testing.T) {
	for _, code := range []string{
		"registration_abuse",
		"registration_identity_abuse",
		"registration_ip_multi_account",
		"api_request_observation",
	} {
		if !isRetiredV1IdentityRule(code) {
			t.Fatalf("isRetiredV1IdentityRule(%q) = false", code)
		}
	}
	if isRetiredV1IdentityRule("login_failure_burst") {
		t.Fatal("login_failure_burst must remain available as a non-identity rule")
	}
}

func TestAdminRejectsRecreatingRetiredV1Rule(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "shadow", Identity: IdentityConfig{Enabled: true}}, NewMemoryRepository(nil))
	body := []byte(`{"code":"api_request_observation","name":"retired","event_types":["login_failure"],"enabled":false,"window_seconds":600,"threshold":1,"score":0,"risk_level":"low","action":"observe"}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules", body, testSecret, "nonce-recreate-v1-rule", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdminRejectsEnablingRetiredV1IdentityRuleWhenV2IsActive(t *testing.T) {
	repo := NewMemoryRepository(defaultRules())
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "shadow", Identity: IdentityConfig{Enabled: true}}, repo)
	body := []byte(`{"code":"registration_identity_abuse","name":"V1 identity","event_types":["registration_attempt"],"enabled":true,"window_seconds":600,"threshold":3,"score":80,"risk_level":"critical","action":"observe","revision":1}`)
	request := signedRequest(http.MethodPut, "/api/v1/admin/rules/registration_identity_abuse", body, testSecret, "nonce-retired-v1-rule", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMemoryRepositoryCreatesUniqueRuleWithInitialRevision(t *testing.T) {
	repo := NewMemoryRepository(defaultRules())
	created, err := repo.CreateRule(context.Background(), validTestRule())
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if created.ID == 0 || created.Revision != 1 || created.Code != "custom_login_failure" {
		t.Fatalf("created rule = %+v", created)
	}
	if _, err := repo.CreateRule(context.Background(), validTestRule()); err != ErrRuleCodeConflict {
		t.Fatalf("duplicate CreateRule() error = %v, want conflict", err)
	}
}

func TestSQLRepositoryCreatesRule(t *testing.T) {
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(context.Background(), db); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}
	repo := NewSQLRepository(db)
	rule := validTestRule()
	rule.Code = "sql_test_rule"
	created, err := repo.CreateRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if created.ID <= 0 || created.Revision != 1 || len(created.EventTypes) != 1 || created.EventTypes[0] != "login_failure" {
		t.Fatalf("created rule = %+v", created)
	}
}

func TestAdminRuleCreateWritesAuditRecord(t *testing.T) {
	repo := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"code":"custom_login_failure","name":"自定义登录失败","description":"短时间内连续登录失败","event_types":["login_failure"],"count_strategy":"user_events","enabled":true,"window_seconds":300,"threshold":5,"score":80,"risk_level":"high","action":"review","reason":"上线前验证"}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules", body, testSecret, "nonce-create-rule", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	items, total, err := repo.ListAudit(context.Background(), 10, 0, "create_rule", 0, "")
	if err != nil || total != 1 || len(items) != 1 || items[0].ActorID != 7 || items[0].Reason != "上线前验证" {
		t.Fatalf("audit = total %d items %+v error=%v", total, items, err)
	}
}

type nonAuditedRuleRepository struct{ RiskRepository }

func TestAdminRuleCreateRefusesRepositoryWithoutAtomicAudit(t *testing.T) {
	base := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, nonAuditedRuleRepository{RiskRepository: base})
	body := []byte(`{"code":"custom_login_failure","name":"自定义登录失败","event_types":["login_failure"],"count_strategy":"user_events","enabled":true,"window_seconds":300,"threshold":5,"score":80,"risk_level":"high","action":"review","reason":"上线前验证"}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules", body, testSecret, "nonce-reject-non-atomic-rule", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	rules, err := base.ListRules(context.Background())
	if err != nil || len(rules) != 0 {
		t.Fatalf("rule mutation escaped audit guard: rules=%+v error=%v", rules, err)
	}
}

func TestSQLRuleCreateRollsBackWhenAuditCannotBeWritten(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rule := validTestRule()
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO risk_rules`).
		WithArgs(rule.Code, rule.Name, rule.Description, sqlmock.AnyArg(), rule.CountStrategy, rule.Enabled, rule.WindowSeconds, rule.Threshold, rule.Score, rule.RiskLevel, rule.Action, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "description", "event_types", "count_strategy", "enabled", "window_seconds", "threshold", "score", "risk_level", "action", "revision"}).
			AddRow(17, rule.Code, rule.Name, rule.Description, `["login_failure"]`, rule.CountStrategy, true, 300, 5, 80, "high", "review", 1))
	mock.ExpectExec(`INSERT INTO risk_audit_logs`).WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()

	if _, err := NewSQLRepository(db).CreateRuleWithAudit(context.Background(), rule, 7, "上线前验证"); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRuleUpdateRollsBackWhenAuditCannotBeWritten(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	before := validTestRule()
	before.ID, before.Revision = 17, 1
	update := before
	update.Threshold = 8
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE risk_rules`).
		WithArgs(before.Code, update.Name, update.Description, sqlmock.AnyArg(), update.CountStrategy, update.Enabled, update.WindowSeconds, update.Threshold, update.Score, update.RiskLevel, update.Action, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "description", "event_types", "count_strategy", "enabled", "window_seconds", "threshold", "score", "risk_level", "action", "revision"}).
			AddRow(17, before.Code, update.Name, update.Description, `["login_failure"]`, update.CountStrategy, true, 300, 8, 80, "high", "review", 2))
	mock.ExpectExec(`INSERT INTO risk_audit_logs`).WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()

	if _, err := NewSQLRepository(db).UpdateRuleWithAudit(context.Background(), before.Code, 1, update, before, 7, "调整阈值"); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminRuleTestWritesAuditRecord(t *testing.T) {
	repo := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"sample":{"event_type":"login_failure","observed_count":5,"user_id":42},"rule":{"code":"login_failure_test","name":"登录失败测试","event_types":["login_failure"],"count_strategy":"user_events","window_seconds":300,"threshold":5,"score":80,"risk_level":"high","action":"review"}}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules/test", body, testSecret, "nonce-rule-test-audit", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	items, total, err := repo.ListAudit(context.Background(), 10, 0, "rule_test", 0, "")
	if err != nil || total != 1 || len(items) != 1 || items[0].ActorID != 7 || items[0].TargetID != "login_failure_test" {
		t.Fatalf("audit = total %d items %+v error=%v", total, items, err)
	}
}
