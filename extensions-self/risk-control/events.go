package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidEvent = errors.New("invalid risk event")
)

type RiskService struct {
	repo RiskRepository
	cfg  Config
}

func NewRiskService(cfg Config, repo RiskRepository) *RiskService {
	cfg.Mode = normalizeMode(cfg.Mode)
	return &RiskService{repo: repo, cfg: cfg}
}

func (s *RiskService) EvaluateEvent(ctx context.Context, input EventReport) (Decision, error) {
	if s == nil || s.repo == nil {
		return Decision{}, errors.New("risk repository is not configured")
	}
	input.EventKey = strings.TrimSpace(input.EventKey)
	input.EventType = strings.TrimSpace(input.EventType)
	if input.EventKey == "" || input.EventType == "" {
		return Decision{}, ErrInvalidEvent
	}
	if len(input.EventKey) > 240 || len(input.EventType) > 80 {
		return Decision{}, ErrInvalidEvent
	}
	if existing, found, err := s.repo.GetEventByKey(ctx, input.EventKey); err != nil {
		return Decision{}, err
	} else if found {
		if err := s.repairMissingSubject(ctx, existing); err != nil {
			return Decision{}, err
		}
		return Decision{Action: existing.Decision, Score: existing.Score, RiskLevel: existing.RiskLevel, Reason: existing.Reason, EventID: existing.ID, RuleCodes: existing.RuleCodes, Mode: s.cfg.Mode}, nil
	}

	now := time.Now().UTC()
	if strings.TrimSpace(input.OccurredAt) == "" {
		input.OccurredAt = now.Format(time.RFC3339Nano)
	} else if occurred, err := parseTime(input.OccurredAt); err != nil || occurred.After(now.Add(2*time.Minute)) {
		return Decision{}, fmt.Errorf("invalid occurred_at: %w", err)
	}
	userID := input.UserID
	subjectID := strings.TrimSpace(input.SubjectID)
	rules, err := s.repo.ListRules(ctx)
	if err != nil {
		return Decision{}, err
	}
	recentCounts := make(map[string]int, len(rules))
	for _, rule := range rules {
		since := now.Add(-time.Duration(rule.WindowSeconds) * time.Second)
		count := 0
		eventTypes := rule.EventTypes
		if len(eventTypes) == 0 {
			eventTypes = []string{input.EventType}
		}
		for _, eventType := range eventTypes {
			previous, countErr := s.repo.CountRecent(ctx, userID, subjectID, input.IPHash, input.DeviceHash, eventType, rule.CountStrategy, since)
			if countErr != nil {
				return Decision{}, countErr
			}
			count += previous
		}
		if ruleMatchesEvent(rule, input) {
			count++
		}
		recentCounts[rule.Code] = count
	}
	decision := evaluateRulesWithMode(rules, input, func(rule Rule) int {
		return recentCounts[rule.Code]
	}, s.cfg.Mode)
	decision.Mode = s.cfg.Mode
	if decision.Reason == "" {
		decision.Reason = strings.TrimSpace(input.Reason)
	}
	riskType := strings.TrimSpace(input.RiskType)
	if riskType == "" {
		riskType = input.EventType
	}
	reason := strings.TrimSpace(input.Reason)
	if decision.Reason != "" && reason != "" && decision.Reason != reason {
		reason += "; " + decision.Reason
	} else if reason == "" {
		reason = decision.Reason
	}
	record := EventRecord{
		EventKey: input.EventKey, EventType: input.EventType, UserID: input.UserID, SubjectID: input.SubjectID,
		UsernameSnapshot: input.UsernameSnapshot, AccountStatusSnapshot: input.AccountStatus, EmailHash: input.EmailHash,
		IPHash: input.IPHash, DeviceHash: input.DeviceHash, RiskType: riskType, ErrorCode: input.ErrorCode,
		Reason: reason, Endpoint: input.Endpoint, Model: input.Model, HTTPStatus: input.HTTPStatus,
		Evidence: input.Evidence, Decision: decision.Action, Score: decision.Score, RiskLevel: decision.RiskLevel,
		RuleCodes: decision.RuleCodes, OccurredAt: input.OccurredAt,
	}
	stored, duplicate, err := s.repo.InsertEvent(ctx, record)
	if err != nil {
		return Decision{}, err
	}
	if duplicate {
		if err := s.repairMissingSubject(ctx, stored); err != nil {
			return Decision{}, err
		}
		return Decision{Action: stored.Decision, Score: stored.Score, RiskLevel: stored.RiskLevel, Reason: stored.Reason, EventID: stored.ID, RuleCodes: stored.RuleCodes, Mode: s.cfg.Mode}, nil
	}
	if err := s.repo.UpsertSubject(ctx, record); err != nil {
		return Decision{}, err
	}
	decision.EventID = stored.ID
	return decision, nil
}

func (s *RiskService) repairMissingSubject(ctx context.Context, event EventRecord) error {
	if event.UserID <= 0 {
		return nil
	}
	if _, found, err := s.repo.GetSubject(ctx, event.UserID); err != nil {
		return err
	} else if found {
		return nil
	}
	return s.repo.UpsertSubject(ctx, event)
}

func (s *RiskService) RecordAudit(ctx context.Context, report AuditReport) error {
	audit := AuditRecord{AuditKey: strings.TrimSpace(report.AuditKey), ActorID: report.ActorID, Action: report.Action, TargetType: report.TargetType, TargetID: report.TargetID, Result: report.Result, Reason: report.Reason, Metadata: report.Metadata, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.repo.InsertAudit(ctx, audit); err != nil {
		return err
	}
	if audit.TargetType == "user" && (audit.Action == "ban" || audit.Action == "unban" || audit.Action == "auto_ban") {
		if userID, err := strconv.ParseInt(strings.TrimSpace(audit.TargetID), 10, 64); err == nil {
			return s.repo.SetSubjectPending(ctx, userID, false)
		}
	}
	return nil
}

func defaultRules() []Rule {
	return []Rule{
		{ID: 1, Code: "registration_identity_abuse", Name: "同邮箱或设备重复注册", EventTypes: []string{"registration_attempt", "registration_success"}, CountStrategy: countStrategySubjectDeviceEvents, Enabled: true, WindowSeconds: 600, Threshold: 3, Score: 80, RiskLevel: "critical", Action: "reject_candidate", Revision: 1},
		{ID: 2, Code: "registration_ip_multi_account", Name: "同 IP 多账号注册", EventTypes: []string{"registration_success"}, CountStrategy: countStrategyIPDistinctSubjects, Enabled: true, WindowSeconds: 600, Threshold: 5, Score: 60, RiskLevel: "high", Action: "review", Revision: 1},
		{ID: 3, Code: "login_failure_burst", Name: "登录失败爆发", EventTypes: []string{"login_failure"}, CountStrategy: countStrategyAssociatedEvents, Enabled: true, WindowSeconds: 600, Threshold: 5, Score: 70, RiskLevel: "high", Action: "review", Revision: 1},
		{ID: 4, Code: "api_error_burst", Name: "API 错误爆发", EventTypes: []string{"api_error"}, CountStrategy: countStrategyAssociatedEvents, Enabled: true, WindowSeconds: 300, Threshold: 10, Score: 35, RiskLevel: "medium", Action: "observe", Revision: 1},
		{ID: 5, Code: "content_risk", Name: "内容风险", EventTypes: []string{"content_risk"}, CountStrategy: countStrategyAssociatedEvents, Enabled: true, WindowSeconds: 86400, Threshold: 1, Score: 85, RiskLevel: "high", Action: "review", Revision: 1},
		{ID: 6, Code: "quota_abuse", Name: "配额滥用", EventTypes: []string{"quota_exceeded"}, CountStrategy: countStrategyAssociatedEvents, Enabled: true, WindowSeconds: 3600, Threshold: 5, Score: 55, RiskLevel: "medium", Action: "review", Revision: 1},
		{ID: 7, Code: "upstream_error", Name: "上游错误", EventTypes: []string{"upstream_error"}, CountStrategy: countStrategyAssociatedEvents, Enabled: true, WindowSeconds: 600, Threshold: 8, Score: 25, RiskLevel: "low", Action: "observe", Revision: 1},
	}
}
