package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func identityRuleCode(path, suffix string) (string, bool) {
	code := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/admin/identity-rules/"), suffix)
	code = strings.Trim(code, "/")
	return code, code != "" && !strings.Contains(code, "/")
}

func (s *HTTPServer) handleIdentityRuleDraft(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity rule administration is disabled"))
		return
	}
	code, ok := identityRuleCode(path, "/draft")
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid identity rule code"))
		return
	}
	if r.Method == http.MethodGet {
		draft, err := s.identity.repo.GetIdentityRuleDraft(r.Context(), code)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, draft)
		return
	}
	var input IdentityRuleDraft
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.RuleCode = code
	actor, _ := actorID(r)
	draft, err := s.identity.repo.SaveIdentityRuleDraft(r.Context(), input, actor)
	if errors.Is(err, ErrRuleRevisionConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *HTTPServer) handleIdentityRuleSimulation(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	code, ok := identityRuleCode(path, "/simulations")
	if !ok || s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusBadRequest, errors.New("identity rule simulation is unavailable"))
		return
	}
	var input struct {
		TargetRevision int `json:"target_revision,omitempty"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	result, err := s.identity.repo.SimulateIdentityRule(r.Context(), code, input.TargetRevision, actor, s.cfg.Identity.CompositeEnforcementEnabled)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "simulate_identity_rule", TargetType: "identity_rule", TargetID: code, Result: "success", Reason: result.Draft.Reason, Metadata: map[string]any{"simulation_id": result.ID, "base_revision": result.BaseRevision, "configured_action": result.ConfiguredAction, "effective_action": result.ProjectedEffectiveAction, "affected_accounts": result.AffectedAccountCount}})
	writeJSON(w, http.StatusOK, result)
}

func decodeIdentityRuleApproval(body []byte) (identityRulePublishApproval, int, error) {
	var input struct {
		Reason           string `json:"reason"`
		BaseRevision     int    `json:"base_revision,omitempty"`
		WindowSeconds    int    `json:"window_seconds,omitempty"`
		Threshold        int    `json:"threshold,omitempty"`
		Score            *int   `json:"score,omitempty"`
		ConfiguredAction string `json:"configured_action,omitempty"`
		Enabled          *bool  `json:"enabled,omitempty"`
		SimulationID     int64  `json:"simulation_id,omitempty"`
		Confirmed        bool   `json:"confirmed"`
		Confirmation     string `json:"confirmation"`
		TargetRevision   int    `json:"target_revision,omitempty"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		return identityRulePublishApproval{}, 0, err
	}
	approval := identityRulePublishApproval{Reason: input.Reason, Enabled: input.Enabled}
	directChange := input.BaseRevision > 0 || input.WindowSeconds > 0 || input.Threshold > 0 || input.Score != nil || strings.TrimSpace(input.ConfiguredAction) != "" || input.Enabled != nil
	if directChange {
		if input.BaseRevision <= 0 || input.WindowSeconds <= 0 || input.Threshold <= 0 || input.Score == nil || strings.TrimSpace(input.ConfiguredAction) == "" || input.Enabled == nil {
			return identityRulePublishApproval{}, 0, errors.New("invalid direct identity rule change")
		}
		approval.Draft = &IdentityRuleDraft{BaseRevision: input.BaseRevision, WindowSeconds: input.WindowSeconds, Threshold: input.Threshold, Score: *input.Score, ConfiguredAction: strings.TrimSpace(input.ConfiguredAction), Reason: input.Reason}
	}
	return approval, input.TargetRevision, nil
}

func (s *HTTPServer) handleIdentityRuleLifecycle(w http.ResponseWriter, r *http.Request, path string, body []byte, operation string) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.ExplainEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity rule administration is disabled"))
		return
	}
	code, ok := identityRuleCode(path, "/"+operation)
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid identity rule code"))
		return
	}
	approval, targetRevision, err := decodeIdentityRuleApproval(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	var revision int
	switch operation {
	case "publish":
		revision, err = s.identity.repo.PublishIdentityRule(r.Context(), code, actor, approval)
	case "enable":
		revision, err = s.identity.repo.EnableIdentityRule(r.Context(), code, actor, approval)
	case "rollback":
		revision, err = s.identity.repo.RollbackIdentityRule(r.Context(), code, targetRevision, actor, approval)
	default:
		err = errors.New("invalid identity rule operation")
	}
	if errors.Is(err, ErrRuleRevisionConflict) || errors.Is(err, ErrIdentityRuleNoChanges) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "revision": revision, "operation": operation})
}

func (s *HTTPServer) handleReviewCaseCreate(w http.ResponseWriter, r *http.Request, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity cases are disabled"))
		return
	}
	var input struct {
		UserID       int64  `json:"user_id"`
		SignalFamily string `json:"signal_family"`
		Status       string `json:"status"`
		Reason       string `json:"reason"`
		ReviewDueAt  string `json:"review_due_at"`
		Goal         string `json:"observation_goal"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	var reviewDue time.Time
	if strings.TrimSpace(input.ReviewDueAt) != "" {
		parsedReviewDue, parseErr := time.Parse(time.RFC3339, input.ReviewDueAt)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid review due time"))
			return
		}
		reviewDue = parsedReviewDue
	}
	item, err := s.identity.repo.CreateReviewCaseWithObservation(r.Context(), input.UserID, actor, input.SignalFamily, input.Status, input.Reason, reviewDue, input.Goal)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "create_risk_review_case", TargetType: "user", TargetID: strconv.FormatInt(input.UserID, 10), Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"case_id": item.ID, "case_status": item.Status, "signal_family": item.SignalFamily}})
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handleReviewCaseObserve(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	caseID, ok := numericPathID(path, "/api/v1/admin/review-cases/", "/observe")
	if !ok || s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusBadRequest, errors.New("invalid review case"))
		return
	}
	var input struct {
		Reason           string `json:"reason"`
		ReviewDueAt      string `json:"review_due_at"`
		ObservationGoal  string `json:"observation_goal"`
		ExpectedRevision int    `json:"expected_revision"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor, _ := actorID(r)
	reviewDue, err := time.Parse(time.RFC3339, strings.TrimSpace(input.ReviewDueAt))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid review due time"))
		return
	}
	item, err := s.identity.repo.ObserveReviewCaseWithReview(r.Context(), caseID, actor, input.Reason, reviewDue, input.ObservationGoal, input.ExpectedRevision)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "observe_risk_review_case", TargetType: "risk_review_case", TargetID: strconv.FormatInt(caseID, 10), Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"case_status": item.Status, "user_id": item.UserID, "review_due_at": item.ReviewDueAt, "observation_goal": item.ObservationGoal, "revision": item.Revision}})
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handleNetworkIdentityLabelPreview(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	networkID, ok := numericPathID(path, "/api/v1/admin/network-identities/", "/label-preview")
	if !ok || s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusBadRequest, errors.New("invalid network identity"))
		return
	}
	var input struct {
		Label string `json:"label"`
	}
	if err := decodeStrictJSON(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	impact, err := s.identity.repo.NetworkLabelImpact(r.Context(), networkID, input.Label)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, impact)
}

func (s *HTTPServer) handleNetworkIdentityLabelRevoke(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	networkID, ok := numericPathID(path, "/api/v1/admin/network-identities/", "/label-revoke")
	if !ok || s.identity == nil || !s.cfg.Identity.AdminEnabled || !s.cfg.Identity.CasesEnabled {
		writeError(w, http.StatusBadRequest, errors.New("invalid network identity"))
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
	impact, err := s.identity.repo.RevokeSharedNetworkLabel(r.Context(), networkID, actor, input.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: "revoke_shared_network_label", TargetType: "network_identity", TargetID: strconv.FormatInt(networkID, 10), Result: "success", Reason: strings.TrimSpace(input.Reason), Metadata: map[string]any{"before_label": impact.CurrentLabel, "requires_rebuild": impact.RequiresRebuild}})
	writeJSON(w, http.StatusOK, impact)
}
