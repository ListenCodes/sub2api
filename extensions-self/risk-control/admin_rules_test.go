package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func validTestRule() Rule {
	return Rule{
		Code:          "custom_login_failure",
		Name:          "自定义登录失败",
		Description:   "短时间内连续登录失败",
		EventTypes:    []string{"login_failure"},
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
		{name: "invalid score", rule: Rule{Code: "safe_code", Name: "名称", EventTypes: []string{"login_failure"}, WindowSeconds: 1, Threshold: 1, Score: 101, RiskLevel: "low", Action: "observe"}, want: "score"},
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
	body := []byte(`{"code":"custom_login_failure","name":"自定义登录失败","description":"短时间内连续登录失败","event_types":["login_failure"],"enabled":true,"window_seconds":300,"threshold":5,"score":80,"risk_level":"high","action":"review","reason":"上线前验证"}`)
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

func TestAdminRuleTestWritesAuditRecord(t *testing.T) {
	repo := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"event_type":"login_failure","count":5,"rule":{"code":"login_failure_test","name":"登录失败测试","threshold":5,"score":80,"risk_level":"high","action":"review"}}`)
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
