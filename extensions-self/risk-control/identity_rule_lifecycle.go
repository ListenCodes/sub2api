package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const identityRuleSimulationTTL = 30 * time.Minute

var ErrIdentityRuleNoChanges = errors.New("identity rule configuration is unchanged")

var validIdentityRuleActions = map[string]struct{}{
	"observe": {}, "review": {}, "reject_candidate": {}, "auto_ban": {},
}

func validateIdentityRuleDraft(draft IdentityRuleDraft) error {
	draft.RuleCode = strings.TrimSpace(draft.RuleCode)
	draft.Reason = strings.TrimSpace(draft.Reason)
	if draft.RuleCode == "" || strings.Contains(draft.RuleCode, "/") || draft.BaseRevision <= 0 || draft.WindowSeconds <= 0 || draft.WindowSeconds > 31*24*60*60 || draft.Threshold <= 0 || draft.Threshold > 100000 || draft.Score < 0 || draft.Score > 100 || draft.Reason == "" || len([]rune(draft.Reason)) > 500 {
		return errors.New("invalid identity rule draft")
	}
	if _, ok := validIdentityRuleActions[draft.ConfiguredAction]; !ok {
		return errors.New("invalid identity rule action")
	}
	if draft.ConfiguredAction == "reject_candidate" && draft.RuleCode != "v2_registration_composite_accounts" {
		return errors.New("candidate rejection is only supported by the composite registration rule")
	}
	return nil
}

func (r *SQLIdentityRepository) SaveIdentityRuleDraft(ctx context.Context, draft IdentityRuleDraft, actorID int64) (IdentityRuleDraft, error) {
	draft.RuleCode, draft.Reason, draft.ConfiguredAction = strings.TrimSpace(draft.RuleCode), strings.TrimSpace(draft.Reason), strings.TrimSpace(draft.ConfiguredAction)
	draft.UpdatedBy = actorID
	if actorID <= 0 || validateIdentityRuleDraft(draft) != nil {
		return IdentityRuleDraft{}, errors.New("invalid identity rule draft")
	}
	var currentRevision int
	if err := r.db.QueryRowContext(ctx, `SELECT revision FROM risk_identity_rules WHERE code=$1`, draft.RuleCode).Scan(&currentRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IdentityRuleDraft{}, errors.New("identity rule is unavailable")
		}
		return IdentityRuleDraft{}, err
	}
	if currentRevision != draft.BaseRevision {
		return IdentityRuleDraft{}, ErrRuleRevisionConflict
	}
	var updated time.Time
	err := r.db.QueryRowContext(ctx, `INSERT INTO risk_identity_rule_drafts(rule_code,base_revision,window_seconds,threshold,score,configured_action,reason,updated_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(rule_code) DO UPDATE SET base_revision=EXCLUDED.base_revision,window_seconds=EXCLUDED.window_seconds,threshold=EXCLUDED.threshold,score=EXCLUDED.score,configured_action=EXCLUDED.configured_action,reason=EXCLUDED.reason,updated_by=EXCLUDED.updated_by,updated_at=NOW()
RETURNING updated_at`, draft.RuleCode, draft.BaseRevision, draft.WindowSeconds, draft.Threshold, draft.Score, draft.ConfiguredAction, draft.Reason, actorID).Scan(&updated)
	if err != nil {
		return IdentityRuleDraft{}, err
	}
	draft.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return draft, nil
}

func scanIdentityRuleDraft(scanner interface{ Scan(...any) error }) (IdentityRuleDraft, error) {
	var draft IdentityRuleDraft
	var updated time.Time
	err := scanner.Scan(&draft.RuleCode, &draft.BaseRevision, &draft.WindowSeconds, &draft.Threshold, &draft.Score, &draft.ConfiguredAction, &draft.Reason, &draft.UpdatedBy, &updated)
	if err == nil {
		draft.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	}
	return draft, err
}

func (r *SQLIdentityRepository) GetIdentityRuleDraft(ctx context.Context, code string) (IdentityRuleDraft, error) {
	draft, err := scanIdentityRuleDraft(r.db.QueryRowContext(ctx, `SELECT rule_code,base_revision,window_seconds,threshold,score,configured_action,reason,updated_by,updated_at FROM risk_identity_rule_drafts WHERE rule_code=$1`, strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return IdentityRuleDraft{}, errors.New("identity rule draft is unavailable")
	}
	return draft, err
}

func (r *SQLIdentityRepository) identityRuleDraftForSimulation(ctx context.Context, code string, targetRevision int) (IdentityRuleDraft, error) {
	code = strings.TrimSpace(code)
	if targetRevision > 0 {
		var currentRevision int
		if err := r.db.QueryRowContext(ctx, `SELECT revision FROM risk_identity_rules WHERE code=$1`, code).Scan(&currentRevision); err != nil {
			return IdentityRuleDraft{}, err
		}
		var raw []byte
		if err := r.db.QueryRowContext(ctx, `SELECT rule_snapshot FROM risk_rule_versions WHERE rule_kind='identity' AND rule_code=$1 AND revision=$2`, code, targetRevision).Scan(&raw); err != nil {
			return IdentityRuleDraft{}, errors.New("rollback target is unavailable")
		}
		var snapshot struct {
			WindowSeconds    int    `json:"window_seconds"`
			Threshold        int    `json:"threshold"`
			Score            int    `json:"score"`
			ConfiguredAction string `json:"configured_action"`
		}
		if json.Unmarshal(raw, &snapshot) != nil {
			return IdentityRuleDraft{}, errors.New("rollback target is invalid")
		}
		if snapshot.ConfiguredAction == "" {
			snapshot.ConfiguredAction = "observe"
		}
		return IdentityRuleDraft{RuleCode: code, BaseRevision: currentRevision, WindowSeconds: snapshot.WindowSeconds, Threshold: snapshot.Threshold, Score: snapshot.Score, ConfiguredAction: snapshot.ConfiguredAction, Reason: fmt.Sprintf("回滚到第 %d 版", targetRevision)}, nil
	}
	if draft, err := r.GetIdentityRuleDraft(ctx, code); err == nil {
		return draft, nil
	}
	var draft IdentityRuleDraft
	err := r.db.QueryRowContext(ctx, `SELECT code,revision,window_seconds,threshold,score,configured_action FROM risk_identity_rules WHERE code=$1`, code).Scan(&draft.RuleCode, &draft.BaseRevision, &draft.WindowSeconds, &draft.Threshold, &draft.Score, &draft.ConfiguredAction)
	draft.Reason = "模拟当前已发布配置"
	return draft, err
}

func (r *SQLIdentityRepository) SimulateIdentityRule(ctx context.Context, code string, targetRevision int, actorID int64, compositeEnforcement bool) (IdentityRuleSimulation, error) {
	if actorID <= 0 {
		return IdentityRuleSimulation{}, errors.New("invalid identity rule simulation")
	}
	draft, err := r.identityRuleDraftForSimulation(ctx, code, targetRevision)
	if err != nil {
		return IdentityRuleSimulation{}, err
	}
	if err := validateIdentityRuleDraft(draft); err != nil {
		return IdentityRuleSimulation{}, err
	}
	result := IdentityRuleSimulation{RuleCode: draft.RuleCode, BaseRevision: draft.BaseRevision, Draft: draft, ConfiguredAction: draft.ConfiguredAction, ExistingAccountsChanged: false, CandidateAccountEffect: "none", Warnings: []string{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*)::bigint,COUNT(DISTINCT NULLIF(user_id,0))::bigint FROM risk_identity_signals WHERE rule_code=$1 AND status='active'`, draft.RuleCode).Scan(&result.AffectedSignalCount, &result.AffectedAccountCount); err != nil {
		return result, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*)::bigint FROM risk_review_cases WHERE primary_signal=$1 AND status IN ('pending','in_review','observing')`, draft.RuleCode).Scan(&result.OpenCaseCount); err != nil {
		return result, err
	}
	switch draft.ConfiguredAction {
	case "reject_candidate":
		if draft.RuleCode == "v2_registration_composite_accounts" && compositeEnforcement {
			result.ProjectedEffectiveAction = "reject_candidate"
			result.CandidateAccountEffect = fmt.Sprintf("已有成功账号达到 %d 个时，仅拒绝第 %d 个候选账号；不会封禁已有账号", draft.Threshold-1, draft.Threshold)
		} else {
			result.ProjectedEffectiveAction = "review"
			result.Warnings = append(result.Warnings, "当前运行配置不允许自动拒绝，实际动作降级为人工复核")
		}
	case "auto_ban":
		result.ProjectedEffectiveAction = "review"
		result.Warnings = append(result.Warnings, "身份规则不自动封禁已有账号，实际动作固定降级为人工复核")
	default:
		result.ProjectedEffectiveAction = draft.ConfiguredAction
	}
	created, expires := time.Now().UTC(), time.Now().UTC().Add(identityRuleSimulationTTL)
	draftJSON, _ := json.Marshal(identityRuleDraftConfig(draft))
	resultJSON, _ := json.Marshal(result)
	err = r.db.QueryRowContext(ctx, `INSERT INTO risk_identity_rule_simulations(rule_code,base_revision,draft_snapshot,result_snapshot,requested_by,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, draft.RuleCode, draft.BaseRevision, draftJSON, resultJSON, actorID, created, expires).Scan(&result.ID)
	if err != nil {
		return IdentityRuleSimulation{}, err
	}
	result.CreatedAt, result.ExpiresAt = created.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano)
	return result, nil
}

type identityRulePublishApproval struct {
	Reason  string
	Draft   *IdentityRuleDraft
	Enabled *bool
}

func validateIdentityRuleApproval(_, _ string, approval identityRulePublishApproval) error {
	approval.Reason = strings.TrimSpace(approval.Reason)
	if len([]rune(approval.Reason)) > 500 {
		return errors.New("reason is too long")
	}
	return nil
}

func identityRuleChangeReason(action, reason string) string {
	if reason = strings.TrimSpace(reason); reason != "" {
		return reason
	}
	switch action {
	case "enable_identity_rule":
		return "管理员直接启用规则"
	case "disable_identity_rule":
		return "管理员直接停用规则"
	case "rollback_identity_rule":
		return "管理员直接回滚规则"
	default:
		return "管理员直接发布规则"
	}
}

func identityRuleDraftConfig(draft IdentityRuleDraft) map[string]any {
	return map[string]any{"rule_code": draft.RuleCode, "base_revision": draft.BaseRevision, "window_seconds": draft.WindowSeconds, "threshold": draft.Threshold, "score": draft.Score, "configured_action": draft.ConfiguredAction}
}

func identityRuleSnapshot(rule IdentityRuleDraft, revision int, enabled bool, domain, family, subjectKind string) map[string]any {
	return map[string]any{"code": rule.RuleCode, "domain": domain, "window_seconds": rule.WindowSeconds, "threshold": rule.Threshold, "score": rule.Score, "mode": "shadow", "configured_action": rule.ConfiguredAction, "revision": revision, "signal_family": family, "subject_kind": subjectKind, "enabled": enabled}
}

func (r *SQLIdentityRepository) applyIdentityRuleRevision(ctx context.Context, code string, draft IdentityRuleDraft, enabled bool, actorID int64, action string, approval identityRulePublishApproval) (int, error) {
	requestedReason := strings.TrimSpace(approval.Reason)
	approval.Reason = identityRuleChangeReason(action, approval.Reason)
	draft.Reason = approval.Reason
	if actorID <= 0 || validateIdentityRuleDraft(draft) != nil || validateIdentityRuleApproval(code, draft.ConfiguredAction, approval) != nil {
		return 0, errors.New("invalid identity rule change")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('risk_identity_rule_lifecycle:' || signal_family,0)) FROM risk_identity_rules WHERE code=$1`, code); err != nil {
		return 0, err
	}
	var revision int
	var currentEnabled bool
	var domain, family, subjectKind string
	var currentWindow, currentThreshold, currentScore int
	var currentAction string
	err = tx.QueryRowContext(ctx, `SELECT revision,enabled,domain,signal_family,subject_kind,window_seconds,threshold,score,configured_action FROM risk_identity_rules WHERE code=$1 FOR UPDATE`, code).Scan(&revision, &currentEnabled, &domain, &family, &subjectKind, &currentWindow, &currentThreshold, &currentScore, &currentAction)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("identity rule is unavailable")
	}
	if err != nil {
		return 0, err
	}
	if revision != draft.BaseRevision {
		return 0, ErrRuleRevisionConflict
	}
	configChanged := currentWindow != draft.WindowSeconds || currentThreshold != draft.Threshold || currentScore != draft.Score || currentAction != draft.ConfiguredAction
	enabledChanged := currentEnabled != enabled
	if !configChanged && !enabledChanged {
		return 0, ErrIdentityRuleNoChanges
	}
	if !configChanged && enabledChanged && action != "rollback_identity_rule" {
		if enabled {
			action = "enable_identity_rule"
		} else {
			action = "disable_identity_rule"
		}
		if requestedReason == "" {
			approval.Reason = identityRuleChangeReason(action, "")
			draft.Reason = approval.Reason
		}
	}
	nextRevision := revision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE risk_rule_versions SET active_until=COALESCE(active_until,NOW()) WHERE rule_kind='identity' AND rule_code=$1 AND revision=$2`, code, revision); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_rules SET enabled=$2,window_seconds=$3,threshold=$4,score=$5,configured_action=$6,revision=$7,active_from=NOW(),active_until=NULL,updated_at=NOW() WHERE code=$1`, code, enabled, draft.WindowSeconds, draft.Threshold, draft.Score, draft.ConfiguredAction, nextRevision); err != nil {
		return 0, err
	}
	snapshot := identityRuleSnapshot(draft, nextRevision, enabled, domain, family, subjectKind)
	snapshotJSON, _ := json.Marshal(snapshot)
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_rule_versions(rule_kind,rule_code,revision,signal_family,domain,active_from,enabled,rule_snapshot) VALUES('identity',$1,$2,$3,$4,NOW(),$5,$6)`, code, nextRevision, family, domain, enabled, snapshotJSON); err != nil {
		return 0, err
	}
	userRows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM risk_identity_signals WHERE rule_code=$1 AND status='active' AND user_id>0 ORDER BY user_id`, code)
	if err != nil {
		return 0, err
	}
	var userIDs []int64
	for userRows.Next() {
		var userID int64
		if err := userRows.Scan(&userID); err != nil {
			userRows.Close()
			return 0, err
		}
		userIDs = append(userIDs, userID)
	}
	if err := userRows.Err(); err != nil {
		userRows.Close()
		return 0, err
	}
	if err := userRows.Close(); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_signals SET status='superseded' WHERE rule_code=$1 AND status='active'`, code); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_decisions decision SET status='superseded',current_score=0 WHERE decision.status='active' AND EXISTS(SELECT 1 FROM risk_identity_signals signal WHERE signal.decision_id=decision.decision_id AND signal.rule_code=$1)`, code); err != nil {
		return 0, err
	}
	if err := refreshIdentityReviewCases(ctx, tx, userIDs, family); err != nil {
		return 0, err
	}
	for _, userID := range userIDs {
		if err := refreshIdentityUserSummary(ctx, tx, userID); err != nil {
			return 0, err
		}
	}
	before := map[string]any{"revision": revision, "enabled": currentEnabled, "window_seconds": currentWindow, "threshold": currentThreshold, "score": currentScore, "configured_action": currentAction}
	after := map[string]any{"revision": nextRevision, "enabled": enabled, "window_seconds": draft.WindowSeconds, "threshold": draft.Threshold, "score": draft.Score, "configured_action": draft.ConfiguredAction}
	metadata, _ := json.Marshal(map[string]any{"before": before, "after": after, "diff": ruleFieldDiff(before, after)})
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason,metadata) VALUES($1,$2,'identity_rule',$3,'success',$4,$5)`, actorID, action, code, strings.TrimSpace(approval.Reason), metadata); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM risk_identity_rule_drafts WHERE rule_code=$1`, code); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return nextRevision, nil
}

func (r *SQLIdentityRepository) PublishIdentityRule(ctx context.Context, code string, actorID int64, approval identityRulePublishApproval) (int, error) {
	var draft IdentityRuleDraft
	var err error
	if approval.Draft != nil {
		draft = *approval.Draft
		draft.RuleCode = strings.TrimSpace(code)
	} else {
		draft, err = r.GetIdentityRuleDraft(ctx, code)
		if err != nil {
			return 0, err
		}
	}
	var enabled bool
	if approval.Enabled != nil {
		enabled = *approval.Enabled
	} else if err := r.db.QueryRowContext(ctx, `SELECT enabled FROM risk_identity_rules WHERE code=$1`, code).Scan(&enabled); err != nil {
		return 0, err
	}
	return r.applyIdentityRuleRevision(ctx, code, draft, enabled, actorID, "publish_identity_rule", approval)
}

func (r *SQLIdentityRepository) EnableIdentityRule(ctx context.Context, code string, actorID int64, approval identityRulePublishApproval) (int, error) {
	draft, err := r.identityRuleDraftForSimulation(ctx, code, 0)
	if err != nil {
		return 0, err
	}
	draft.Reason = strings.TrimSpace(approval.Reason)
	return r.applyIdentityRuleRevision(ctx, code, draft, true, actorID, "enable_identity_rule", approval)
}

func (r *SQLIdentityRepository) RollbackIdentityRule(ctx context.Context, code string, targetRevision int, actorID int64, approval identityRulePublishApproval) (int, error) {
	if targetRevision <= 0 {
		return 0, errors.New("invalid rollback revision")
	}
	draft, err := r.identityRuleDraftForSimulation(ctx, code, targetRevision)
	if err != nil {
		return 0, err
	}
	draft.Reason = strings.TrimSpace(approval.Reason)
	var enabled bool
	if err := r.db.QueryRowContext(ctx, `SELECT enabled FROM risk_rule_versions WHERE rule_kind='identity' AND rule_code=$1 AND revision=$2`, code, targetRevision).Scan(&enabled); err != nil {
		return 0, err
	}
	return r.applyIdentityRuleRevision(ctx, code, draft, enabled, actorID, "rollback_identity_rule", approval)
}
