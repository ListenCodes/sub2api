package main

import (
	"fmt"
	"strings"
)

const (
	countStrategyUserEvents                  = "user_events"
	countStrategyEmailSubjectEvents          = "email_subject_events"
	countStrategyIPDistinctSuccessUsers      = "ip_distinct_success_users"
	countStrategyBrowserDistinctSuccessUsers = "browser_instance_distinct_success_users"
	countStrategyAPIClientDistinctUsers      = "api_client_distinct_users"
	countStrategyIPBrowserCooccurrence       = "ip_browser_cooccurrence"
)

func normalizeCountStrategy(strategy string) string {
	switch strings.TrimSpace(strategy) {
	case countStrategyEmailSubjectEvents, countStrategyIPDistinctSuccessUsers, countStrategyBrowserDistinctSuccessUsers, countStrategyAPIClientDistinctUsers, countStrategyIPBrowserCooccurrence:
		return strings.TrimSpace(strategy)
	default:
		return countStrategyUserEvents
	}
}

func isDistinctSubjectStrategy(strategy string) bool {
	switch normalizeCountStrategy(strategy) {
	case countStrategyIPDistinctSuccessUsers, countStrategyBrowserDistinctSuccessUsers, countStrategyAPIClientDistinctUsers, countStrategyIPBrowserCooccurrence:
		return true
	default:
		return false
	}
}

func evaluateRules(rules []Rule, event EventReport, recentCount func(Rule) int) Decision {
	return evaluateRulesWithMode(rules, event, recentCount, "enforce")
}

func evaluateRulesWithMode(rules []Rule, event EventReport, recentCount func(Rule) int, mode string) Decision {
	decision := Decision{Action: "allow", RiskLevel: "none"}
	if recentCount == nil {
		recentCount = func(Rule) int { return 0 }
	}
	type matchedRule struct {
		rule  Rule
		count int
	}
	winners := map[string]matchedRule{}
	order := []string{}
	for _, rule := range rules {
		if !rule.Enabled || rule.Threshold <= 0 || !ruleMatchesEvent(rule, event) || !ruleHasRequiredEvidence(rule, event) {
			continue
		}
		count := recentCount(rule)
		if count < rule.Threshold {
			continue
		}
		family := ruleSignalFamily(rule, event)
		current, exists := winners[family]
		if exists && (current.rule.Score > rule.Score || (current.rule.Score == rule.Score && riskActionRank(current.rule.Action) >= riskActionRank(rule.Action))) {
			continue
		}
		if !exists {
			order = append(order, family)
		}
		winners[family] = matchedRule{rule: rule, count: count}
	}
	for _, family := range order {
		match := winners[family]
		rule, count := match.rule, match.count
		decision.Score += maxInt(rule.Score, 0)
		if riskLevelRank(rule.RiskLevel) > riskLevelRank(decision.RiskLevel) {
			decision.RiskLevel = normalizeRiskLevel(rule.RiskLevel)
		}
		if riskActionRank(rule.Action) > riskActionRank(decision.Action) {
			decision.Action = normalizeRiskAction(rule.Action)
		}
		decision.RuleCodes = append(decision.RuleCodes, rule.Code)
		reason := formatRuleReason(rule, event, count)
		if decision.Reason == "" {
			decision.Reason = reason
		} else {
			decision.Reason += "；" + reason
		}
	}
	if decision.Score > 100 {
		decision.Score = 100
	}
	if decision.Action != "allow" {
		switch mode {
		case "shadow":
			decision.Action = "observe"
		case "review":
			if decision.Action == "ban" || decision.Action == "reject_candidate" {
				decision.Action = "review"
			}
		}
	}
	return decision
}

func ruleSignalFamily(rule Rule, event EventReport) string {
	switch normalizeCountStrategy(rule.CountStrategy) {
	case countStrategyEmailSubjectEvents:
		return "registration_email_flow"
	case countStrategyIPDistinctSuccessUsers, countStrategyBrowserDistinctSuccessUsers, countStrategyIPBrowserCooccurrence:
		return "registration_identity"
	case countStrategyAPIClientDistinctUsers:
		return "api_client_observation"
	default:
		return "user_event:" + event.EventType
	}
}

func formatRuleReason(rule Rule, event EventReport, count int) string {
	name := strings.TrimSpace(rule.Name)
	if name == "" {
		name = rule.Code
	}
	if count <= 0 || rule.WindowSeconds <= 0 {
		return fmt.Sprintf("命中规则：%s", name)
	}
	window := fmt.Sprintf("%d 秒", rule.WindowSeconds)
	if rule.WindowSeconds >= 3600 && rule.WindowSeconds%3600 == 0 {
		window = fmt.Sprintf("%d 小时", rule.WindowSeconds/3600)
	} else if rule.WindowSeconds >= 60 && rule.WindowSeconds%60 == 0 {
		window = fmt.Sprintf("%d 分钟", rule.WindowSeconds/60)
	}
	occurrence := fmt.Sprintf("%d 次事件", count)
	switch normalizeCountStrategy(rule.CountStrategy) {
	case countStrategyEmailSubjectEvents:
		occurrence = fmt.Sprintf("同一账号标识事件 %d 次", count)
	case countStrategyIPDistinctSuccessUsers:
		occurrence = fmt.Sprintf("同 IP 注册 %d 个账号", count)
	case countStrategyBrowserDistinctSuccessUsers:
		occurrence = fmt.Sprintf("同浏览器实例注册 %d 个账号", count)
	case countStrategyAPIClientDistinctUsers:
		occurrence = fmt.Sprintf("同 API 客户端关联 %d 个账号", count)
	case countStrategyIPBrowserCooccurrence:
		occurrence = fmt.Sprintf("同 IP 与浏览器实例共同注册 %d 个账号", count)
	default:
		switch event.EventType {
		case "login_failure":
			occurrence = fmt.Sprintf("失败 %d 次", count)
		case "api_error":
			occurrence = fmt.Sprintf("API 错误 %d 次", count)
		case "registration_attempt", "registration_success":
			occurrence = fmt.Sprintf("注册尝试 %d 次", count)
		}
	}
	return fmt.Sprintf("命中规则：%s（%s内%s）", name, window, occurrence)
}

func ruleMatchesEvent(rule Rule, event EventReport) bool {
	if len(rule.EventTypes) == 0 {
		return rule.Code == event.EventType
	}
	for _, eventType := range rule.EventTypes {
		if eventType == event.EventType {
			return true
		}
	}
	return false
}

func normalizeRiskAction(action string) string {
	if validRiskAction(action) {
		return action
	}
	return "observe"
}

func normalizeRiskLevel(level string) string {
	if riskLevelRank(level) == 0 {
		return "low"
	}
	return level
}

func riskActionRank(action string) int {
	switch action {
	case "auto_ban":
		return 5
	case "ban":
		return 4
	case "reject_candidate":
		return 3
	case "review":
		return 2
	case "observe":
		return 1
	default:
		return 0
	}
}

func riskLevelRank(level string) int {
	switch level {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
