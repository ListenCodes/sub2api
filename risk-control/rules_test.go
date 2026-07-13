package main

import (
	"context"
	"errors"
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

func TestEvaluateRulesBuildsReadableChineseReason(t *testing.T) {
	rule := Rule{Code: "login_failure_burst", Name: "登录失败爆发", EventTypes: []string{"login_failure"}, Enabled: true, WindowSeconds: 300, Threshold: 5, Score: 70, RiskLevel: "high", Action: "review"}
	decision := evaluateRules([]Rule{rule}, EventReport{EventType: "login_failure"}, func(Rule) int { return 5 })
	if decision.Reason != "命中规则：登录失败爆发（5 分钟内失败 5 次）" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateRulesShadowModeDowngradesBlockingAction(t *testing.T) {
	rule := Rule{Code: "registration_abuse", EventTypes: []string{"registration_attempt"}, Enabled: true, WindowSeconds: 600, Threshold: 1, Score: 90, RiskLevel: "critical", Action: "reject_candidate"}
	decision := evaluateRulesWithMode([]Rule{rule}, EventReport{EventType: "registration_attempt"}, func(Rule) int { return 1 }, "shadow")
	if decision.Action != "observe" || decision.Score != 90 || decision.RiskLevel != "critical" {
		t.Fatalf("decision = %+v", decision)
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

func TestMemoryRepositoryClearsPendingAfterNormalObservation(t *testing.T) {
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
	if subject.Pending {
		t.Fatalf("subject remains pending after normal observation: %+v", subject)
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
