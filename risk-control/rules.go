package main

import "fmt"

func evaluateRules(rules []Rule, event EventReport, recentCount func(Rule) int) Decision {
	return evaluateRulesWithMode(rules, event, recentCount, "enforce")
}

func evaluateRulesWithMode(rules []Rule, event EventReport, recentCount func(Rule) int, mode string) Decision {
	decision := Decision{Action: "allow", RiskLevel: "none"}
	if recentCount == nil {
		recentCount = func(Rule) int { return 0 }
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Threshold <= 0 || !ruleMatchesEvent(rule, event) || recentCount(rule) < rule.Threshold {
			continue
		}
		decision.Score += maxInt(rule.Score, 0)
		if riskLevelRank(rule.RiskLevel) > riskLevelRank(decision.RiskLevel) {
			decision.RiskLevel = normalizeRiskLevel(rule.RiskLevel)
		}
		if riskActionRank(rule.Action) > riskActionRank(decision.Action) {
			decision.Action = normalizeRiskAction(rule.Action)
		}
		decision.RuleCodes = append(decision.RuleCodes, rule.Code)
		if decision.Reason == "" {
			decision.Reason = fmt.Sprintf("规则 %s 命中", rule.Code)
		} else {
			decision.Reason += "; " + fmt.Sprintf("规则 %s 命中", rule.Code)
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
