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

func (s *HTTPServer) handleRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.service.repo.ListRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

func (s *HTTPServer) handleRuleUpdate(w http.ResponseWriter, r *http.Request, code string) {
	var input Rule
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Revision <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("revision is required"))
		return
	}
	if !validRiskAction(input.Action) || riskLevelRank(input.RiskLevel) == 0 || input.WindowSeconds <= 0 || input.Threshold <= 0 || input.Score < 0 || input.Score > 100 {
		writeError(w, http.StatusBadRequest, errors.New("invalid rule configuration"))
		return
	}
	updated, err := s.service.repo.UpdateRule(r.Context(), code, input.Revision, input)
	if errors.Is(err, ErrRuleRevisionConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	actor, _ := actorID(r)
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "update_rule", TargetType: "rule", TargetID: code, Result: "success", Metadata: map[string]any{"revision": updated.Revision}})
	writeJSON(w, http.StatusOK, map[string]any{"id": updated.ID, "revision": updated.Revision})
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
	decision := evaluateRulesWithMode([]Rule{input.Rule}, EventReport{EventType: input.EventType}, func(Rule) int { return input.Count }, "enforce")
	matched := len(decision.RuleCodes) > 0
	writeJSON(w, http.StatusOK, map[string]any{"matched": matched, "score": decision.Score, "decision": decision})
}

func (s *HTTPServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	userID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("target_user_id")); raw != "" {
		userID, _ = strconv.ParseInt(raw, 10, 64)
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	result := strings.TrimSpace(r.URL.Query().Get("result"))
	items, total, err := s.service.repo.ListAudit(r.Context(), limit, offset, action, userID, result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": offset/limit + 1, "page_size": limit})
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
