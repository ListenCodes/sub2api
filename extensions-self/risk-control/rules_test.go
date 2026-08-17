package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestEvaluateRulesUsesRecentEventWindowAndHighestAction(t *testing.T) {
	rules := []Rule{
		{Code: "login_failure_burst", EventTypes: []string{"login_failure"}, Enabled: true, WindowSeconds: 600, Threshold: 3, Score: 45, RiskLevel: "high", Action: "review"},
		{Code: "registration_abuse", EventTypes: []string{"registration_attempt"}, Enabled: true, WindowSeconds: 600, Threshold: 2, Score: 80, RiskLevel: "critical", Action: "ban"},
	}
	event := EventReport{EventKey: "login-42-4", EventType: "login_failure", UserID: 42, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	decision := evaluateRules(rules, event, func(rule Rule) int {
		if rule.Code == "login_failure_burst" {
			return 3
		}
		return 0
	})
	if decision.Action != "review" || decision.Score != 45 || decision.RiskLevel != "high" {
		t.Fatalf("decision = %+v", decision)
	}
	if len(decision.RuleCodes) != 1 || decision.RuleCodes[0] != "login_failure_burst" {
		t.Fatalf("rule codes = %#v", decision.RuleCodes)
	}
}

func TestEvaluateRulesReturnsAllowWhenThresholdIsNotReached(t *testing.T) {
	rule := Rule{Code: "api_error_burst", EventTypes: []string{"api_error"}, Enabled: true, WindowSeconds: 60, Threshold: 4, Score: 50, RiskLevel: "medium", Action: "review"}
	decision := evaluateRules([]Rule{rule}, EventReport{EventType: "api_error"}, func(Rule) int { return 3 })
	if decision.Action != "allow" || decision.Score != 0 || decision.RiskLevel != "none" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestReliabilityEventCannotProduceUserRiskWhenLegacyRuleIsEnabled(t *testing.T) {
	repo := NewMemoryRepository([]Rule{{
		Code: "api_error_burst", Name: "API error", EventTypes: []string{"api_error"},
		CountStrategy: countStrategyUserEvents, Enabled: true, WindowSeconds: 60,
		Threshold: 1, Score: 90, RiskLevel: "critical", Action: "ban",
	}})
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	decision, err := service.EvaluateEvent(context.Background(), EventReport{
		EventKey: "api-error-runtime-boundary", EventType: "api_error", UserID: 42,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "allow" || decision.Score != 0 || len(decision.RuleCodes) != 0 {
		t.Fatalf("reliability decision = %+v", decision)
	}
	if _, found, err := repo.GetSubject(context.Background(), 42); err != nil || found {
		t.Fatalf("reliability event projected as user risk: found=%v error=%v", found, err)
	}
}

func TestEvaluateRulesBuildsReadableChineseReason(t *testing.T) {
	rule := Rule{Code: "login_failure_burst", Name: "登录失败爆发", EventTypes: []string{"login_failure"}, Enabled: true, WindowSeconds: 300, Threshold: 5, Score: 70, RiskLevel: "high", Action: "review"}
	decision := evaluateRules([]Rule{rule}, EventReport{EventType: "login_failure", UserID: 42}, func(Rule) int { return 5 })
	if decision.Reason != "命中规则：登录失败爆发（5 分钟内失败 5 次）" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateEventSeparatesIdentityAndSharedIPRegistrationRules(t *testing.T) {
	now := time.Now().UTC()
	rules := []Rule{
		{ID: 1, Code: "registration_email_observation", Name: "同邮箱重复注册观察", EventTypes: []string{"registration_attempt", "registration_success"}, CountStrategy: countStrategyEmailSubjectEvents, Enabled: true, WindowSeconds: 600, Threshold: 3, Score: 0, RiskLevel: "low", Action: "observe", Revision: 1},
		{ID: 2, Code: "registration_ip_multi_account", Name: "同 IP 多账号注册", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 2, Score: 60, RiskLevel: "high", Action: "review", Revision: 1},
	}
	repo := NewMemoryRepository(rules)
	for index, subjectID := range []string{"subject-a", "subject-b"} {
		_, _, err := repo.InsertEvent(context.Background(), EventRecord{
			EventKey: "prior-success-" + subjectID, EventType: "registration_success", UserID: int64(index + 1),
			SubjectID: subjectID, IPHash: "shared-ip", DeviceHash: "device-" + subjectID,
			OccurredAt: now.Add(-time.Duration(index+1) * time.Minute).Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}
	_, _, err := repo.InsertEvent(context.Background(), EventRecord{
		EventKey: "prior-success-current-subject", EventType: "registration_success", UserID: 3,
		SubjectID: "subject-c", IPHash: "shared-ip", DeviceHash: "device-c",
		OccurredAt: now.Add(-3 * time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("seed current subject event: %v", err)
	}
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	decision, err := service.EvaluateEvent(context.Background(), EventReport{
		EventKey: "current-success", EventType: "registration_success", UserID: 3,
		SubjectID: "subject-c", IPHash: "shared-ip", DeviceHash: "device-c", OccurredAt: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if len(decision.RuleCodes) != 1 || decision.RuleCodes[0] != "registration_ip_multi_account" {
		t.Fatalf("rule codes = %#v, want only shared-IP rule", decision.RuleCodes)
	}
	if decision.Reason != "命中规则：同 IP 多账号注册（10 分钟内同 IP 注册 3 个账号）" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateEventCountsBrowserInstancesSeparatelyFromEmailAndIP(t *testing.T) {
	now := time.Now().UTC()
	rules := []Rule{
		{ID: 1, Code: "registration_browser_multi_account", Name: "同浏览器实例多账号注册", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyBrowserDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 3, Score: 70, RiskLevel: "high", Action: "review", Revision: 1},
		{ID: 2, Code: "registration_ip_multi_account", Name: "同 IP 多账号注册", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 3, Score: 60, RiskLevel: "high", Action: "review", Revision: 1},
	}
	repo := NewMemoryRepository(rules)
	for index := range 3 {
		_, _, err := repo.InsertEvent(context.Background(), EventRecord{
			EventKey: "prior-browser-event-" + string(rune('a'+index)), EventType: "registration_success", UserID: int64(index + 1),
			SubjectID: "different-email-" + string(rune('a'+index)), IPHash: "different-ip-" + string(rune('a'+index)), DeviceHash: "shared-browser-instance",
			OccurredAt: now.Add(-time.Duration(index+1) * time.Minute).Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	decision, err := service.EvaluateEvent(context.Background(), EventReport{
		EventKey: "current-success", EventType: "registration_success", UserID: 4, SubjectID: "new-email",
		IPHash: "new-ip", DeviceHash: "shared-browser-instance", OccurredAt: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if len(decision.RuleCodes) != 1 || decision.RuleCodes[0] != "registration_browser_multi_account" {
		t.Fatalf("rule codes = %#v, want only browser-instance rule", decision.RuleCodes)
	}
	if decision.Reason != "命中规则：同浏览器实例多账号注册（10 分钟内同浏览器实例注册 4 个账号）" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateRulesShadowModeDowngradesBlockingAction(t *testing.T) {
	rule := Rule{Code: "registration_abuse", EventTypes: []string{"registration_attempt"}, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 90, RiskLevel: "critical", Action: "reject_candidate"}
	decision := evaluateRulesWithMode([]Rule{rule}, EventReport{EventType: "registration_attempt", UserID: 42}, func(Rule) int { return 1 }, "shadow")
	if decision.Action != "observe" || decision.Score != 90 || decision.RiskLevel != "critical" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestEvaluateRulesDoesNotCountWithoutRequiredEvidence(t *testing.T) {
	rule := Rule{Code: "registration_ip_multi_account", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 60, RiskLevel: "high", Action: "review"}
	counted := false
	decision := evaluateRules([]Rule{rule}, EventReport{EventType: "registration_success", UserID: 42}, func(Rule) int {
		counted = true
		return 99
	})
	if counted || decision.Action != "allow" || decision.Score != 0 {
		t.Fatalf("missing IP evidence was evaluated: counted=%v decision=%+v", counted, decision)
	}
}

func TestEvaluateRulesScoresOnlyStrongestRegistrationIdentityFamily(t *testing.T) {
	rules := []Rule{
		{Code: "registration_ip", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 60, RiskLevel: "high", Action: "review"},
		{Code: "registration_browser", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyBrowserDistinctSuccessUsers, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 70, RiskLevel: "high", Action: "review"},
		{Code: "registration_composite", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPBrowserCooccurrence, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 90, RiskLevel: "critical", Action: "review"},
	}
	event := EventReport{EventType: "registration_success", UserID: 42, IPHash: "ip", DeviceHash: "browser"}
	decision := evaluateRules(rules, event, func(Rule) int { return 3 })
	if decision.Score != 90 || len(decision.RuleCodes) != 1 || decision.RuleCodes[0] != "registration_composite" {
		t.Fatalf("registration identity family stacked scores: %+v", decision)
	}
}

func TestValidateRuleConfigRejectsAmbiguousStrategySemantics(t *testing.T) {
	base := Rule{Code: "test_rule", Name: "Test rule", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSuccessUsers, WindowSeconds: 60, Threshold: 1, Score: 10, RiskLevel: "low", Action: "observe"}
	tests := []Rule{
		func() Rule {
			rule := base
			rule.EventTypes = []string{"registration_success", "registration_success"}
			return rule
		}(),
		func() Rule { rule := base; rule.EventTypes = []string{"login_failure"}; return rule }(),
		func() Rule {
			rule := base
			rule.CountStrategy = countStrategyAPIClientDistinctUsers
			rule.EventTypes = []string{"api_error"}
			rule.Score = 10
			return rule
		}(),
		func() Rule {
			rule := base
			rule.CountStrategy = countStrategyEmailSubjectEvents
			rule.EventTypes = []string{"registration_attempt", "registration_success"}
			rule.Score = 10
			return rule
		}(),
	}
	for _, rule := range tests {
		if err := validateRuleConfig(rule); err == nil {
			t.Fatalf("ambiguous strategy was accepted: %+v", rule)
		}
	}
}

func TestEvaluateEventSkipsLegacyRuleWithInflatingDistinctStrategy(t *testing.T) {
	rule := Rule{
		Code: "legacy_ip_rule", Name: "legacy", EventTypes: []string{"registration_success", "login_failure"},
		CountStrategy: countStrategyIPDistinctSuccessUsers, Enabled: true, WindowSeconds: 600,
		Threshold: 3, Score: 80, RiskLevel: "high", Action: "review", Revision: 1,
	}
	repo := NewMemoryRepository([]Rule{rule})
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	for index, userID := range []int64{10, 11} {
		_, _, err := repo.InsertEvent(context.Background(), EventRecord{
			EventKey: fmt.Sprintf("prior-%d", index), EventType: "registration_success", UserID: userID,
			IPHash: "shared-ip", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	decision, err := service.EvaluateEvent(context.Background(), EventReport{
		EventKey: "current", EventType: "registration_success", UserID: 12, IPHash: "shared-ip",
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if decision.Action != "allow" || decision.Score != 0 || len(decision.RuleCodes) != 0 {
		t.Fatalf("invalid legacy rule was evaluated: %+v", decision)
	}
}

func TestUpdateRuleRejectsRevisionConflict(t *testing.T) {
	store := newMemoryRuleStore([]Rule{{ID: 1, Code: "login_failure_burst", Revision: 2}})
	_, err := store.UpdateRule("login_failure_burst", 1, Rule{Code: "login_failure_burst", Revision: 1})
	if !errors.Is(err, ErrRuleRevisionConflict) {
		t.Fatalf("error = %v, want revision conflict", err)
	}
}

func TestEvaluateEventBackfillsMissingRiskTypeFromEventType(t *testing.T) {
	repo := NewMemoryRepository(nil)
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	_, err := service.EvaluateEvent(context.Background(), EventReport{
		EventKey: "login-42-1", EventType: "login_failure", UserID: 42,
		Reason: "invalid credentials", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	events, _, err := repo.ListEvents(context.Background(), 10, 0, 42)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].RiskType != "login_failure" {
		t.Fatalf("events = %+v, want risk_type login_failure", events)
	}
}

func TestMemoryRepositoryKeepsStrongestRiskSignalAfterNormalObservation(t *testing.T) {
	repo := NewMemoryRepository(nil)
	if err := repo.UpsertSubject(context.Background(), EventRecord{UserID: 42, RiskType: "login_failure", RiskLevel: "high", Score: 80, Reason: "Repeated failures", Decision: "review", OccurredAt: "2026-07-12T00:00:00Z"}); err != nil {
		t.Fatalf("seed subject: %v", err)
	}
	if err := repo.UpsertSubject(context.Background(), EventRecord{UserID: 42, RiskType: "api_request", RiskLevel: "low", Score: 0, Reason: "gateway request completed", Decision: "observe", OccurredAt: "2026-07-12T00:01:00Z"}); err != nil {
		t.Fatalf("observe subject: %v", err)
	}
	subject, found, err := repo.GetSubject(context.Background(), 42)
	if err != nil || !found {
		t.Fatalf("GetSubject() = %+v, %v, found=%v", subject, err, found)
	}
	if subject.RiskType != "login_failure" || subject.RiskLevel != "high" || subject.Score != 80 || subject.Reason != "Repeated failures" {
		t.Fatalf("subject signal was downgraded: %+v", subject)
	}
	if subject.EventCount != 2 || subject.LastEventAt != "2026-07-12T00:01:00Z" {
		t.Fatalf("subject counters/timestamp = %+v", subject)
	}
}

type failOnceSubjectRepository struct {
	*MemoryRepository
	failed bool
}

func (r *failOnceSubjectRepository) UpsertSubject(ctx context.Context, event EventRecord) error {
	if !r.failed {
		r.failed = true
		return errors.New("subject aggregate unavailable")
	}
	return r.MemoryRepository.UpsertSubject(ctx, event)
}

func TestEvaluateEventRepairsSubjectAggregationOnIdempotentRetry(t *testing.T) {
	repo := &failOnceSubjectRepository{MemoryRepository: NewMemoryRepository(defaultRules())}
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	input := EventReport{
		EventKey: "login-42-retry", EventType: "login_failure", UserID: 42,
		Reason: "invalid credentials", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := service.EvaluateEvent(context.Background(), input); err == nil {
		t.Fatal("first EvaluateEvent() must expose aggregation failure")
	}
	decision, err := service.EvaluateEvent(context.Background(), input)
	if err != nil {
		t.Fatalf("retry EvaluateEvent() error = %v", err)
	}
	if decision.EventID == 0 {
		t.Fatalf("retry decision = %+v, want persisted event id", decision)
	}
	subject, found, err := repo.GetSubject(context.Background(), 42)
	if err != nil || !found {
		t.Fatalf("GetSubject() = %+v, %v, found=%v", subject, err, found)
	}
	if subject.EventCount != 1 {
		t.Fatalf("subject event count = %d, want 1", subject.EventCount)
	}
}

func TestMemoryRepositoryKeepsPendingAfterNormalObservation(t *testing.T) {
	repo := NewMemoryRepository(nil)
	if err := repo.UpsertSubject(context.Background(), EventRecord{EventKey: "review-1", UserID: 42, RiskType: "login_failure", RiskLevel: "high", Score: 80, Reason: "Repeated failures", Decision: "review", OccurredAt: "2026-07-12T00:00:00Z"}); err != nil {
		t.Fatalf("seed subject: %v", err)
	}
	if err := repo.UpsertSubject(context.Background(), EventRecord{EventKey: "observe-1", UserID: 42, RiskType: "api_request", RiskLevel: "low", Score: 0, Reason: "normal request", Decision: "observe", OccurredAt: "2026-07-12T00:01:00Z"}); err != nil {
		t.Fatalf("observe subject: %v", err)
	}
	subject, found, err := repo.GetSubject(context.Background(), 42)
	if err != nil || !found {
		t.Fatalf("GetSubject() = %+v, %v, found=%v", subject, err, found)
	}
	if !subject.Pending {
		t.Fatalf("normal observation closed a pending review: %+v", subject)
	}
}

func TestIdempotentEventRetryDoesNotReopenManuallyResolvedSubject(t *testing.T) {
	repo := NewMemoryRepository(nil)
	service := NewRiskService(Config{Mode: "enforce"}, repo)
	input := EventReport{EventKey: "review-retry", EventType: "login_failure", UserID: 42, Reason: "manual review", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := service.EvaluateEvent(context.Background(), input); err != nil {
		t.Fatalf("first EvaluateEvent() error = %v", err)
	}
	if err := repo.SetSubjectPending(context.Background(), 42, false); err != nil {
		t.Fatalf("SetSubjectPending() error = %v", err)
	}
	if _, err := service.EvaluateEvent(context.Background(), input); err != nil {
		t.Fatalf("retry EvaluateEvent() error = %v", err)
	}
	subject, _, err := repo.GetSubject(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetSubject() error = %v", err)
	}
	if subject.Pending {
		t.Fatalf("idempotent retry reopened subject: %+v", subject)
	}
}
