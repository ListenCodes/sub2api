package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type EventReport struct {
	EventKey         string         `json:"event_key"`
	EventType        string         `json:"event_type"`
	UserID           int64          `json:"user_id,omitempty"`
	SubjectID        string         `json:"subject_id,omitempty"`
	UsernameSnapshot string         `json:"username,omitempty"`
	AccountStatus    string         `json:"account_status,omitempty"`
	EmailHash        string         `json:"email_hash,omitempty"`
	IPHash           string         `json:"ip_hash,omitempty"`
	DeviceHash       string         `json:"device_hash,omitempty"`
	RiskType         string         `json:"risk_type,omitempty"`
	ErrorCode        string         `json:"error_code,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	Endpoint         string         `json:"endpoint,omitempty"`
	Model            string         `json:"model,omitempty"`
	HTTPStatus       int            `json:"http_status,omitempty"`
	OccurredAt       string         `json:"occurred_at,omitempty"`
	Evidence         map[string]any `json:"evidence,omitempty"`

	// These fields are accepted only by tests and are deliberately excluded from JSON.
	Password    string `json:"-"`
	RequestBody string `json:"-"`
	RawDeviceID string `json:"-"`
}

type AuditReport struct {
	AuditKey   string         `json:"audit_key,omitempty"`
	ActorID    int64          `json:"actor_id"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Decision struct {
	Action    string   `json:"decision"`
	Score     int      `json:"score"`
	RiskLevel string   `json:"risk_level"`
	Reason    string   `json:"reason"`
	EventID   int64    `json:"event_id,omitempty"`
	RuleCodes []string `json:"rule_codes,omitempty"`
	Mode      string   `json:"mode,omitempty"`
}

func validRiskAction(action string) bool {
	switch action {
	case "allow", "observe", "review", "ban", "reject_candidate", "auto_ban":
		return true
	default:
		return false
	}
}

var safeRuleCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,79}$`)

var validRiskEventTypes = map[string]struct{}{
	"registration_attempt": {},
	"registration_success": {},
	"login_attempt":        {},
	"login_failure":        {},
	"api_error":            {},
	"content_risk":         {},
	"quota_exceeded":       {},
	"upstream_error":       {},
	"api_request":          {},
}

var validRuleCountStrategies = map[string]struct{}{
	countStrategyAssociatedEvents:    {},
	countStrategySubjectDeviceEvents: {},
	countStrategyIPDistinctSubjects:  {},
}

func validRuleAction(action string) bool {
	switch action {
	case "observe", "review", "ban", "reject_candidate", "auto_ban":
		return true
	default:
		return false
	}
}

func validateRuleConfig(rule Rule) error {
	code := strings.TrimSpace(rule.Code)
	if !safeRuleCodePattern.MatchString(code) {
		return errors.New("invalid rule code")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is required")
	}
	if len(rule.EventTypes) == 0 {
		return errors.New("event type is required")
	}
	for _, eventType := range rule.EventTypes {
		if _, ok := validRiskEventTypes[strings.TrimSpace(eventType)]; !ok {
			return fmt.Errorf("invalid event type: %s", eventType)
		}
	}
	if strategy := strings.TrimSpace(rule.CountStrategy); strategy != "" {
		if _, ok := validRuleCountStrategies[strategy]; !ok {
			return fmt.Errorf("invalid count strategy: %s", strategy)
		}
	}
	return validateRuleFields(rule)
}

func validateRuleFields(rule Rule) error {
	if rule.WindowSeconds <= 0 {
		return errors.New("window seconds must be positive")
	}
	if rule.Threshold <= 0 {
		return errors.New("threshold must be positive")
	}
	if rule.Score < 0 || rule.Score > 100 {
		return errors.New("score must be between 0 and 100")
	}
	if riskLevelRank(rule.RiskLevel) == 0 {
		return errors.New("invalid risk level")
	}
	if !validRuleAction(rule.Action) {
		return errors.New("invalid rule action")
	}
	return nil
}
