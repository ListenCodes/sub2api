package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (s *HTTPServer) handleReviewCases(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	actor, _ := actorID(r)
	limit, offset := identityPagination(r)
	query := r.URL.Query()
	minScore, err := parseOptionalScore(query.Get("min_score"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	maxScore, err := parseOptionalScore(query.Get("max_score"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.EqualFold(strings.TrimSpace(query.Get("risk_only")), "true") && minScore < 1 {
		minScore = 1
	}
	if level := strings.TrimSpace(query.Get("risk_level")); level != "" {
		levelMin, levelMax, ok := identityRiskLevelRange(level)
		if !ok {
			writeError(w, http.StatusBadRequest, errors.New("invalid risk level filter"))
			return
		}
		if minScore < levelMin {
			minScore = levelMin
		}
		if maxScore < 0 || maxScore > levelMax {
			maxScore = levelMax
		}
	}
	caseStatus := strings.TrimSpace(query.Get("processing_status"))
	if caseStatus != "" && caseStatus != "pending" && caseStatus != "in_review" && caseStatus != "observing" && caseStatus != "resolved" && caseStatus != "data_quality" {
		writeError(w, http.StatusBadRequest, errors.New("invalid case status filter"))
		return
	}
	items, total, err := s.identity.repo.ListReviewCases(r.Context(), strings.TrimSpace(query.Get("view")), actor, uniqueIdentityUserIDs(parseUserIDs(query.Get("user_ids"))), minScore, maxScore, limit, offset, query.Get("sort_by"), query.Get("sort_order"), query.Get("risk_type"), caseStatus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, identityPaged(items, total, limit, offset))
}

func (s *HTTPServer) handleWorkOverview(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	actor, _ := actorID(r)
	result, err := s.identity.repo.WorkOverview(r.Context(), actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) handleReviewCaseGet(w http.ResponseWriter, r *http.Request, path string) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	raw := strings.TrimPrefix(path, "/api/v1/admin/review-cases/")
	caseID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || caseID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid review case id"))
		return
	}
	item, err := s.identity.repo.GetReviewCase(r.Context(), caseID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func identityRiskLevelRange(level string) (int, int, bool) {
	switch level {
	case "none":
		return 0, 0, true
	case "low":
		return 1, 29, true
	case "medium":
		return 30, 59, true
	case "high":
		return 60, 79, true
	case "critical":
		return 80, 100, true
	default:
		return 0, 0, false
	}
}

func parseOptionalScore(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return -1, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > 100 {
		return -1, errors.New("score filter must be an integer between 0 and 100")
	}
	return value, nil
}

func (s *HTTPServer) handleReviewCaseClaim(w http.ResponseWriter, r *http.Request, path string) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	caseID, ok := numericPathID(path, "/api/v1/admin/review-cases/", "/claim")
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid review case id"))
		return
	}
	actor, _ := actorID(r)
	item, err := s.identity.repo.ClaimReviewCase(r.Context(), caseID, actor)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "claim_risk_review_case", TargetType: "risk_review_case", TargetID: strconv.FormatInt(caseID, 10), Result: "success", Metadata: map[string]any{"user_id": item.UserID, "previous_assignee_id": item.PreviousAssigneeID, "assignee_id": item.AssigneeID, "revision": item.Revision}})
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handleReviewCaseFeedback(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	caseID, ok := numericPathID(path, "/api/v1/admin/review-cases/", "/feedback")
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid review case id"))
		return
	}
	var input struct {
		Feedback string `json:"feedback"`
		Reason   string `json:"reason"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	if err := s.identity.repo.AddReviewFeedback(r.Context(), caseID, actor, input.Feedback, input.Reason); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "review_risk_case", TargetType: "risk_review_case", TargetID: strconv.FormatInt(caseID, 10), Result: "success", Reason: input.Reason, Metadata: map[string]any{"feedback": input.Feedback, "enforcement_changed": false}})
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true, "enforcement_changed": false})
}

func (s *HTTPServer) handleReviewCaseResolve(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	caseID, ok := numericPathID(path, "/api/v1/admin/review-cases/", "/resolve")
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid review case id"))
		return
	}
	var input struct {
		UserID           int64  `json:"user_id"`
		Resolution       string `json:"resolution"`
		Reason           string `json:"reason"`
		RequestID        string `json:"request_id"`
		ExpectedRevision int    `json:"expected_revision"`
	}
	if err := decodeStrictJSON(body, &input); err != nil || input.UserID <= 0 || strings.TrimSpace(input.RequestID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("invalid resolve request"))
		return
	}
	current, err := s.identity.repo.GetReviewCase(r.Context(), caseID)
	if err != nil || current.UserID != input.UserID {
		writeError(w, http.StatusConflict, errors.New("review case user does not match"))
		return
	}
	actor, _ := actorID(r)
	item, replayed, err := s.identity.repo.ResolveReviewCase(r.Context(), caseID, actor, input.Resolution, input.Reason, input.RequestID, input.ExpectedRevision)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{AuditKey: "resolve:" + strings.TrimSpace(input.RequestID), ActorID: actor, Action: "resolve_risk_review_case", TargetType: "risk_review_case", TargetID: strconv.FormatInt(caseID, 10), Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"resolution": input.Resolution, "user_id": input.UserID, "revision": item.Revision, "idempotent_replay": replayed}})
	writeJSON(w, http.StatusOK, map[string]any{"case": item, "resolved": true, "idempotent_replay": replayed})
}

func (s *HTTPServer) handleIdentityRuleEffects(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity explanations are disabled"))
		return
	}
	items, err := s.identity.repo.RuleEffects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleIdentityRuleVersions(w http.ResponseWriter, r *http.Request, path string) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity explanations are disabled"))
		return
	}
	code := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/admin/identity-rules/"), "/versions")
	code = strings.Trim(code, "/")
	if code == "" || strings.Contains(code, "/") {
		writeError(w, http.StatusBadRequest, errors.New("invalid identity rule code"))
		return
	}
	items, err := s.identity.repo.ListRuleVersions(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleIdentityRuleDisable(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity rule administration is disabled"))
		return
	}
	code := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/admin/identity-rules/"), "/disable")
	code = strings.Trim(code, "/")
	if code == "" || strings.Contains(code, "/") {
		writeError(w, http.StatusBadRequest, errors.New("invalid identity rule code"))
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	revision, err := s.identity.repo.DisableIdentityRule(r.Context(), code, input.Reason, actor)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "revision": revision, "enabled": false})
}

func (s *HTTPServer) handleNetworkIdentityLabel(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	networkID, ok := numericPathID(path, "/api/v1/admin/network-identities/", "/label")
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid network identity id"))
		return
	}
	var input struct {
		Label  string `json:"label"`
		Reason string `json:"reason"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	impact, err := s.identity.repo.NetworkLabelImpact(r.Context(), networkID, input.Label)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.identity.repo.LabelSharedNetwork(r.Context(), networkID, actor, input.Label, input.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "label_shared_network", TargetType: "network_identity", TargetID: strconv.FormatInt(networkID, 10), Result: "success", Reason: input.Reason, Metadata: map[string]any{"before_label": impact.CurrentLabel, "label": input.Label, "affected_accounts": impact.AffectedAccountCount, "affected_signals": impact.AffectedSignalCount, "resolved_domains": impact.ResolvedDomains}})
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "impact": impact})
}

func numericPathID(path, prefix, suffix string) (int64, bool) {
	raw := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	raw = strings.Trim(raw, "/")
	if raw == "" || strings.Contains(raw, "/") {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value > 0
}

func decodeStrictJSON(body []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request must contain one JSON object")
		}
		return err
	}
	return nil
}
