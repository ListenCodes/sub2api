package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.service.repo.Overview(r.Context(), time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	overview.Mode = s.cfg.Mode
	writeJSON(w, http.StatusOK, overview)
}

func (s *HTTPServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	riskType := strings.TrimSpace(r.URL.Query().Get("risk_type"))
	riskLevel := strings.TrimSpace(r.URL.Query().Get("risk_level"))
	userIDs := parseUserIDs(r.URL.Query().Get("user_ids"))
	items, total, err := s.service.repo.ListSubjects(r.Context(), limit, offset, riskType, riskLevel, userIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, subject := range items {
		result = append(result, map[string]any{
			"id": subject.UserID, "username": subject.Username, "account_status": subject.AccountStatus,
			"risk_type": subject.RiskType, "risk_level": subject.RiskLevel, "score": subject.Score,
			"reason": subject.Reason, "event_count": subject.EventCount, "ip_count": subject.IPCount,
			"device_count": subject.DeviceCount, "last_action": subject.LastAction, "pending": subject.Pending, "last_event_at": subject.LastEventAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result, "total": total, "page": offset/limit + 1, "page_size": limit})
}

func parseUserIDs(raw string) []int64 {
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *HTTPServer) handleUser(w http.ResponseWriter, r *http.Request, userID int64) {
	subject, found, err := s.service.repo.GetSubject(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, errors.New("risk subject not found"))
		return
	}
	events, _, err := s.service.repo.ListEvents(r.Context(), 100, 0, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": subject.UserID, "username": subject.Username, "account_status": subject.AccountStatus,
		"risk_type": subject.RiskType, "risk_level": subject.RiskLevel, "score": subject.Score,
		"reason": subject.Reason, "event_count": subject.EventCount, "ip_count": subject.IPCount,
		"device_count": subject.DeviceCount, "last_action": subject.LastAction, "pending": subject.Pending, "last_event_at": subject.LastEventAt,
		"timeline": events,
	})
}

type processUserRequest struct {
	Reason    string `json:"reason"`
	BatchID   string `json:"batch_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func (s *HTTPServer) handleMarkUserProcessed(w http.ResponseWriter, r *http.Request, userID int64) {
	var input processUserRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, errors.New("reason is required"))
		return
	}
	actor, _ := actorID(r)
	metadata := map[string]any{}
	if input.BatchID = strings.TrimSpace(input.BatchID); input.BatchID != "" {
		metadata["batch_id"] = input.BatchID
	}
	if input.RequestID = strings.TrimSpace(input.RequestID); input.RequestID != "" {
		metadata["request_id"] = input.RequestID
	}
	auditKey := ""
	if input.BatchID != "" {
		auditKey = input.BatchID + ":" + strconv.FormatInt(userID, 10)
	}
	if err := s.service.repo.SetSubjectPending(r.Context(), userID, false); err != nil {
		metadata["failure_reason"] = err.Error()
		_ = s.service.RecordAudit(r.Context(), AuditReport{AuditKey: auditKey, ActorID: actor, Action: "mark_processed", TargetType: "user", TargetID: formatUserID(userID), Result: "failed", Reason: input.Reason, Metadata: metadata})
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.service.RecordAudit(r.Context(), AuditReport{AuditKey: auditKey, ActorID: actor, Action: "mark_processed", TargetType: "user", TargetID: formatUserID(userID), Result: "success", Reason: input.Reason, Metadata: metadata}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": userID, "processed": true})
}

func (s *HTTPServer) handleRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.service.repo.ListRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

type ruleMutationRequest struct {
	Rule
	Reason string `json:"reason"`
}

func (s *HTTPServer) handleRuleCreate(w http.ResponseWriter, r *http.Request) {
	var input ruleMutationRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	for index := range input.EventTypes {
		input.EventTypes[index] = strings.TrimSpace(input.EventTypes[index])
	}
	if err := validateRuleConfig(input.Rule); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if isRetiredV1IdentityRule(input.Code) {
		writeError(w, http.StatusBadRequest, errors.New("retired V1 rule code is reserved"))
		return
	}
	created, err := s.service.repo.CreateRule(r.Context(), input.Rule)
	if errors.Is(err, ErrRuleCodeConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	actor, _ := actorID(r)
	if err := s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "create_rule", TargetType: "rule", TargetID: created.Code, Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"revision": created.Revision}}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (s *HTTPServer) handleRuleUpdate(w http.ResponseWriter, r *http.Request, code string) {
	var input ruleMutationRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Revision <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("revision is required"))
		return
	}
	for index := range input.EventTypes {
		input.EventTypes[index] = strings.TrimSpace(input.EventTypes[index])
	}
	if strings.TrimSpace(input.CountStrategy) == "" {
		rules, err := s.service.repo.ListRules(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, current := range rules {
			if current.Code == code {
				input.CountStrategy = current.CountStrategy
				break
			}
		}
	}
	if err := validateRuleFields(input.Rule); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid rule configuration"))
		return
	}
	legacyAPIObservation := strings.TrimSpace(code) == "api_request_observation"
	if legacyAPIObservation && input.Enabled {
		writeError(w, http.StatusBadRequest, errors.New("api request observation cannot be enabled"))
		return
	}
	if s.cfg.Identity.Enabled && input.Enabled && isRetiredV1IdentityRule(code) {
		writeError(w, http.StatusBadRequest, errors.New("V1 identity rule cannot be enabled while identity V2 is active"))
		return
	}
	allowLegacyAPIObservation := legacyAPIObservation && !input.Enabled
	if err := validateRuleEventTypes(input.EventTypes, allowLegacyAPIObservation); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid rule configuration"))
		return
	}
	if _, ok := validRuleCountStrategies[strings.TrimSpace(input.CountStrategy)]; !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid rule configuration"))
		return
	}
	if !allowLegacyAPIObservation {
		if err := validateRuleStrategy(input.Rule); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid rule configuration"))
			return
		}
	}
	updated, err := s.service.repo.UpdateRule(r.Context(), code, input.Revision, input.Rule)
	if errors.Is(err, ErrRuleRevisionConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	actor, _ := actorID(r)
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "update_rule", TargetType: "rule", TargetID: code, Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"revision": updated.Revision}})
	writeJSON(w, http.StatusOK, map[string]any{"id": updated.ID, "revision": updated.Revision})
}

func isRetiredV1IdentityRule(code string) bool {
	switch strings.TrimSpace(code) {
	case "registration_abuse", "registration_identity_abuse", "registration_ip_multi_account", "api_request_observation":
		return true
	default:
		return false
	}
}

func (s *HTTPServer) handleRuleTest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		EventType string `json:"event_type"`
		Count     int    `json:"count"`
		Rule      Rule   `json:"rule"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Rule.Enabled = true
	input.Rule.EventTypes = []string{strings.TrimSpace(input.EventType)}
	testEvent := EventReport{EventType: input.EventType, UserID: 1, SubjectID: "rule-test-subject", IPHash: "rule-test-ip", DeviceHash: "rule-test-device"}
	decision := evaluateRulesWithMode([]Rule{input.Rule}, testEvent, func(Rule) int { return input.Count }, "enforce")
	matched := len(decision.RuleCodes) > 0
	actor, _ := actorID(r)
	reason := decision.Reason
	if reason == "" {
		reason = "规则测试"
	}
	if err := s.service.RecordAudit(r.Context(), AuditReport{
		ActorID: actor, Action: "rule_test", TargetType: "rule", TargetID: strings.TrimSpace(input.Rule.Code), Result: "success", Reason: reason,
		Metadata: map[string]any{"matched": matched, "score": decision.Score, "risk_level": decision.RiskLevel, "action": decision.Action, "rule_codes": decision.RuleCodes},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matched": matched, "score": decision.Score, "decision": decision})
}

func (s *HTTPServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		category = "security"
	}
	if _, ok := auditCategoryActions[category]; !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid audit category"))
		return
	}
	filter := AuditFilter{Category: category, Target: strings.TrimSpace(r.URL.Query().Get("target")), Action: strings.TrimSpace(r.URL.Query().Get("action")), Result: strings.TrimSpace(r.URL.Query().Get("result")), SortBy: strings.TrimSpace(r.URL.Query().Get("sort_by")), SortOrder: strings.TrimSpace(r.URL.Query().Get("sort_order"))}
	if raw := strings.TrimSpace(r.URL.Query().Get("target_user_id")); raw != "" {
		filter.TargetUserID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("actor_id")); raw != "" {
		filter.ActorID, _ = strconv.ParseInt(raw, 10, 64)
	}
	var err error
	if filter.From, err = parseAuditTime(r.URL.Query().Get("from"), false); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if filter.To, err = parseAuditTime(r.URL.Query().Get("to"), true); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, total, err := s.service.repo.ListAuditFiltered(r.Context(), limit, offset, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": offset/limit + 1, "page_size": limit})
}

func parseAuditTime(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, errors.New("invalid audit time")
	}
	if endOfDay {
		return parsed.Add(24*time.Hour - time.Nanosecond), nil
	}
	return parsed, nil
}

func pagination(r *http.Request) (int, int) {
	limit := 50
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 {
		limit = value
	}
	if limit > 1000 {
		limit = 1000
	}
	page := 1
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value >= 0 {
		return limit, value
	}
	return limit, (page - 1) * limit
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256*1024))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
