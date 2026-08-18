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

var validRiskFeedback = map[string]struct{}{
	"confirmed_abuse": {}, "legitimate_shared": {}, "insufficient_evidence": {}, "data_error": {}, "business_violation": {},
}

func (r *SQLIdentityRepository) ListReviewCases(ctx context.Context, view string, actorID int64, userIDs []int64, minScore, maxScore, limit, offset int, sortBy, sortOrder, riskType, caseStatus string) ([]RiskReviewCase, int, error) {
	where := []string{"1=1"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	switch view {
	case "my":
		add("case_row.assignee_id=$%d", actorID)
		where = append(where, "case_row.status IN ('pending','in_review')")
	case "observing":
		where = append(where, "case_row.status='observing'")
	case "resolved", "processed":
		where = append(where, "case_row.status='resolved'")
	case "all":
	default:
		where = append(where, "case_row.status='pending'")
	}
	if len(userIDs) > 0 {
		add("case_row.user_id=ANY($%d::bigint[])", pqInt64Array(userIDs))
	}
	if minScore >= 0 {
		add("COALESCE(current.current_score,0)>=$%d", minScore)
	}
	if maxScore >= 0 {
		add("COALESCE(current.current_score,0)<=$%d", maxScore)
	}
	if riskType = strings.TrimSpace(riskType); riskType != "" {
		add("case_row.primary_signal=$%d", riskType)
	}
	if caseStatus = strings.TrimSpace(caseStatus); caseStatus == "data_quality" {
		where = append(where, "EXISTS(SELECT 1 FROM risk_signal_processing_jobs quality_job JOIN risk_identity_events quality_event ON quality_event.id=quality_job.event_id WHERE quality_event.user_id=case_row.user_id AND quality_job.status IN ('retry','failed'))")
	} else if caseStatus != "" {
		add("case_row.status=$%d", caseStatus)
	}
	orderColumn := "case_row.last_hit_at"
	switch sortBy {
	case "risk_score":
		orderColumn = "COALESCE(current.current_score,0)"
	case "case_status":
		orderColumn = "case_row.status"
	case "assignee":
		orderColumn = "case_row.assignee_id"
	}
	direction := "DESC"
	if strings.EqualFold(sortOrder, "asc") {
		direction = "ASC"
	}
	base := `WITH current_scores AS (
 SELECT signal.user_id,signal.signal_family,MAX(signal.score)::int current_score
 FROM risk_identity_signals signal JOIN risk_identity_rules rule ON rule.code=signal.rule_code AND rule.revision=signal.rule_revision JOIN risk_rule_versions version ON version.id=signal.rule_version_id AND version.rule_kind='identity' AND version.rule_code=signal.rule_code AND version.revision=signal.rule_revision
 WHERE signal.score>0 AND signal.status='active' AND signal.active_from<=NOW() AND (signal.active_until IS NULL OR signal.active_until>NOW()) AND rule.enabled AND rule.mode='shadow' AND rule.active_from<=NOW() AND (rule.active_until IS NULL OR rule.active_until>NOW()) AND version.enabled AND version.active_from<=NOW() AND (version.active_until IS NULL OR version.active_until>NOW())
 GROUP BY signal.user_id,signal.signal_family
) `
	condition := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, base+`SELECT COUNT(*) FROM risk_review_cases case_row LEFT JOIN current_scores current ON current.user_id=case_row.user_id AND current.signal_family=case_row.signal_family WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, base+`SELECT case_row.id,case_row.user_id,COALESCE(case_row.decision_id,''),case_row.signal_family,case_row.status,case_row.resolution,COALESCE(current.current_score,0),case_row.historical_max_score,case_row.primary_signal,case_row.evidence_strength,case_row.assignee_id,case_row.opened_at,case_row.last_hit_at,case_row.resolved_at FROM risk_review_cases case_row LEFT JOIN current_scores current ON current.user_id=case_row.user_id AND current.signal_family=case_row.signal_family WHERE `+condition+` ORDER BY `+orderColumn+` `+direction+`,case_row.last_hit_at DESC,case_row.id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]RiskReviewCase, 0, limit)
	for rows.Next() {
		var item RiskReviewCase
		var opened, last time.Time
		var resolved sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &opened, &last, &resolved); err != nil {
			return nil, 0, err
		}
		item.OpenedAt, item.LastHitAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano)
		if resolved.Valid {
			item.ResolvedAt = resolved.Time.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLIdentityRepository) WorkOverview(ctx context.Context, actorID int64) (map[string]int, error) {
	query := riskIndexProjectionCTE() + `, case_counts AS (
 SELECT COUNT(*) FILTER(WHERE status='pending')::int pending,
        COUNT(*) FILTER(WHERE assignee_id=$1 AND status IN ('pending','in_review'))::int mine,
        COUNT(*) FILTER(WHERE status='observing')::int observing
 FROM risk_review_cases
), quality_cases AS (
 SELECT COUNT(*)::int data_quality
 FROM risk_review_cases quality_case
 WHERE EXISTS(SELECT 1 FROM risk_signal_processing_jobs job JOIN risk_identity_events event ON event.id=job.event_id
              WHERE event.user_id=quality_case.user_id AND job.status IN ('retry','failed'))
)
SELECT case_counts.pending,case_counts.mine,case_counts.observing,(SELECT COUNT(*)::int FROM risk_index),quality_cases.data_quality
FROM case_counts CROSS JOIN quality_cases`
	var pending, mine, observing, atRisk, dataQuality int
	if err := r.db.QueryRowContext(ctx, query, actorID).Scan(&pending, &mine, &observing, &atRisk, &dataQuality); err != nil {
		return nil, err
	}
	return map[string]int{"pending": pending, "mine": mine, "observing": observing, "at_risk": atRisk, "data_quality": dataQuality}, nil
}

func pqInt64Array(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value > 0 {
			parts = append(parts, fmt.Sprint(value))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (r *SQLIdentityRepository) ClaimReviewCase(ctx context.Context, caseID, actorID int64) (RiskReviewCase, error) {
	if caseID <= 0 || actorID <= 0 {
		return RiskReviewCase{}, errors.New("invalid case claim")
	}
	var item RiskReviewCase
	var opened, last time.Time
	err := r.db.QueryRowContext(ctx, `UPDATE risk_review_cases SET assignee_id=$2,status='in_review',updated_at=NOW() WHERE id=$1 AND status IN ('pending','in_review') AND (assignee_id=0 OR assignee_id=$2) RETURNING id,user_id,COALESCE(decision_id,''),signal_family,status,resolution,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,opened_at,last_hit_at`, caseID, actorID).Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &opened, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return item, errors.New("case is unavailable or assigned to another administrator")
	}
	item.OpenedAt, item.LastHitAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano)
	return item, err
}

func (r *SQLIdentityRepository) AddReviewFeedback(ctx context.Context, caseID, actorID int64, feedback, reason string) error {
	feedback, reason = strings.TrimSpace(feedback), strings.TrimSpace(reason)
	if _, ok := validRiskFeedback[feedback]; !ok || caseID <= 0 || actorID <= 0 || reason == "" || len([]rune(reason)) > 500 {
		return errors.New("invalid review feedback")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedCaseID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM risk_review_cases WHERE id=$1 AND status='in_review' AND assignee_id=$2 FOR UPDATE`, caseID, actorID).Scan(&lockedCaseID); err != nil {
		return errors.New("review case is unavailable")
	}
	result, err := tx.ExecContext(ctx, `UPDATE risk_review_cases SET status='resolved',resolution=$3,resolved_at=NOW(),updated_at=NOW() WHERE id=$1 AND status='in_review' AND assignee_id=$2`, lockedCaseID, actorID, feedback)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("review case is already resolved or unavailable")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_review_feedback(case_id,actor_id,feedback,reason) VALUES($1,$2,$3,$4)`, lockedCaseID, actorID, feedback, reason); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *SQLIdentityRepository) ListRuleVersions(ctx context.Context, code string) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT revision,signal_family,domain,enabled,rule_snapshot,active_from,active_until FROM risk_rule_versions WHERE rule_kind='identity' AND ($1='' OR rule_code=$1) ORDER BY rule_code,revision DESC`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var revision int
		var family, domain string
		var enabled bool
		var snapshot []byte
		var activeFrom time.Time
		var activeUntil sql.NullTime
		if err := rows.Scan(&revision, &family, &domain, &enabled, &snapshot, &activeFrom, &activeUntil); err != nil {
			return nil, err
		}
		var rule map[string]any
		_ = json.Unmarshal(snapshot, &rule)
		item := map[string]any{"revision": revision, "signal_family": family, "domain": domain, "enabled": enabled, "rule_snapshot": rule, "active_from": activeFrom.UTC().Format(time.RFC3339Nano)}
		if activeUntil.Valid {
			item["active_until"] = activeUntil.Time.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SQLIdentityRepository) DisableIdentityRule(ctx context.Context, code, reason string, actorID int64) (int, error) {
	code, reason = strings.TrimSpace(code), strings.TrimSpace(reason)
	if code == "" || strings.Contains(code, "/") || reason == "" || len([]rune(reason)) > 500 || actorID <= 0 {
		return 0, errors.New("invalid identity rule disable request")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT revision,enabled FROM risk_identity_rules WHERE code=$1 FOR UPDATE`, code).Scan(&revision, &enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("identity rule is unavailable")
		}
		return 0, err
	}
	if !enabled {
		return revision, tx.Commit()
	}
	userRows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM risk_identity_signals WHERE rule_code=$1 AND status='active' AND user_id>0`, code)
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
	if err := userRows.Close(); err != nil {
		return 0, err
	}
	nextRevision := revision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE risk_rule_versions SET active_until=COALESCE(active_until,NOW()) WHERE rule_kind='identity' AND rule_code=$1 AND revision=$2`, code, revision); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_rules SET enabled=FALSE,revision=$2,active_from=NOW(),active_until=NULL,updated_at=NOW() WHERE code=$1`, code, nextRevision); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_rule_versions(rule_kind,rule_code,revision,signal_family,domain,active_from,enabled,rule_snapshot)
SELECT 'identity',code,revision,signal_family,domain,active_from,FALSE,jsonb_build_object('code',code,'domain',domain,'window_seconds',window_seconds,'threshold',threshold,'score',score,'mode',mode,'revision',revision,'signal_family',signal_family,'subject_kind',subject_kind,'enabled',FALSE)
FROM risk_identity_rules WHERE code=$1 ON CONFLICT(rule_kind,rule_code,revision) DO NOTHING`, code); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_signals SET status='superseded' WHERE rule_code=$1 AND status='active'`, code); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_decisions decision SET status='superseded',current_score=0
	WHERE decision.status='active' AND EXISTS(SELECT 1 FROM risk_identity_signals signal WHERE signal.decision_id=decision.decision_id AND signal.rule_code=$1)`, code); err != nil {
		return 0, err
	}
	for _, userID := range userIDs {
		if err := refreshIdentityUserSummary(ctx, tx, userID); err != nil {
			return 0, err
		}
	}
	metadata, _ := json.Marshal(map[string]any{"rule_code": code, "previous_revision": revision, "revision": nextRevision, "enabled": false})
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason,metadata) VALUES($1,'disable_identity_rule','identity_rule',$2,'success',$3,$4)`, actorID, code, reason, metadata); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return nextRevision, nil
}

func (r *SQLIdentityRepository) RuleEffects(ctx context.Context) ([]RiskRuleEffect, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT rule.code,rule.revision,rule.window_seconds,rule.threshold,COUNT(signal.id)::bigint,COUNT(DISTINCT NULLIF(signal.user_id,0))::bigint,
COALESCE((SELECT COUNT(*) FILTER(WHERE feedback='confirmed_abuse')::float/NULLIF(COUNT(*),0) FROM risk_review_feedback feedback_row JOIN risk_review_cases case_row ON case_row.id=feedback_row.case_id WHERE case_row.primary_signal=rule.code),0),
COALESCE((SELECT COUNT(*) FILTER(WHERE feedback='legitimate_shared')::float/NULLIF(COUNT(*),0) FROM risk_review_feedback feedback_row JOIN risk_review_cases case_row ON case_row.id=feedback_row.case_id WHERE case_row.primary_signal=rule.code),0),
COALESCE((SELECT COUNT(*) FILTER(WHERE status IN ('retry','failed'))::float/NULLIF(COUNT(*),0) FROM risk_signal_processing_jobs),0)
FROM risk_identity_rules rule LEFT JOIN risk_identity_signals signal ON signal.rule_code=rule.code AND signal.rule_revision=rule.revision
GROUP BY rule.code,rule.revision,rule.window_seconds,rule.threshold ORDER BY rule.code`)
	if err != nil {
		return nil, err
	}
	items := []RiskRuleEffect{}
	type effectRule struct{ window, threshold int }
	rules := []effectRule{}
	for rows.Next() {
		var item RiskRuleEffect
		var rule effectRule
		if err := rows.Scan(&item.RuleCode, &item.Revision, &rule.window, &rule.threshold, &item.HitEvents, &item.UniqueSubjects, &item.ConfirmedRate, &item.LegitimateSharedRate, &item.MissingSignalRate); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range items {
		item, rule := &items[index], rules[index]
		var sampleRows *sql.Rows
		var sampleErr error
		if item.RuleCode == "v2_api_client_accounts" {
			qualified := `WITH qualified AS (
 SELECT device_identity_id FROM risk_identity_activity_daily
 WHERE client_kind='api_client' AND event_class=$1 AND success_count>0 AND last_seen_at>=NOW()-($2*interval '1 second')
 GROUP BY device_identity_id HAVING COUNT(DISTINCT user_id)>=$3
) `
			if err := r.db.QueryRowContext(ctx, qualified+`SELECT COALESCE(SUM(activity.success_count),0)::bigint,COUNT(DISTINCT activity.user_id)::bigint
FROM risk_identity_activity_daily activity JOIN qualified USING(device_identity_id)
WHERE activity.client_kind='api_client' AND activity.event_class=$1 AND activity.success_count>0 AND activity.last_seen_at>=NOW()-($2*interval '1 second')`, identityEventAPI, rule.window, rule.threshold).Scan(&item.HitEvents, &item.UniqueSubjects); err != nil {
				return nil, err
			}
			sampleRows, sampleErr = r.db.QueryContext(ctx, qualified+`SELECT DISTINCT activity.user_id FROM risk_identity_activity_daily activity JOIN qualified USING(device_identity_id)
WHERE activity.client_kind='api_client' AND activity.event_class=$1 AND activity.success_count>0 AND activity.last_seen_at>=NOW()-($2*interval '1 second') ORDER BY activity.user_id LIMIT 10`, identityEventAPI, rule.window, rule.threshold)
		} else {
			sampleRows, sampleErr = r.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM risk_identity_signals WHERE rule_code=$1 AND rule_revision=$2 AND user_id>0 ORDER BY user_id LIMIT 10`, item.RuleCode, item.Revision)
		}
		if sampleErr != nil {
			return nil, sampleErr
		}
		for sampleRows.Next() {
			var userID int64
			if err := sampleRows.Scan(&userID); err != nil {
				sampleRows.Close()
				return nil, err
			}
			item.SampleUserIDs = append(item.SampleUserIDs, userID)
		}
		if err := sampleRows.Close(); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *SQLIdentityRepository) LabelSharedNetwork(ctx context.Context, networkID, actorID int64, label, reason string) error {
	label, reason = strings.TrimSpace(label), strings.TrimSpace(reason)
	validLabels := map[string]struct{}{"home": {}, "company": {}, "school": {}, "public_proxy": {}, "trusted_egress": {}, "mobile_cgnat": {}, "unknown": {}}
	if _, ok := validLabels[label]; !ok || networkID <= 0 || actorID <= 0 || reason == "" || len([]rune(reason)) > 500 {
		return errors.New("invalid network label")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize label changes with signal inserts so an in-flight IP evaluation
	// cannot recreate current risk after a network is classified as shared.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_identity_signals IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_shared_network_labels(network_identity_id,label,reason,actor_id) VALUES($1,$2,$3,$4) ON CONFLICT(network_identity_id) DO UPDATE SET label=EXCLUDED.label,reason=EXCLUDED.reason,actor_id=EXCLUDED.actor_id,updated_at=NOW()`, networkID, label, reason, actorID); err != nil {
		return err
	}
	safeShared := label == "home" || label == "company" || label == "school" || label == "trusted_egress" || label == "mobile_cgnat"
	if !safeShared {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM risk_identity_signals WHERE network_identity_id=$1 AND domain='ip' AND status='active' AND user_id>0`, networkID)
	if err != nil {
		return err
	}
	userIDs := []int64{}
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_identity_signals SET status='resolved' WHERE network_identity_id=$1 AND domain='ip' AND status='active'`, networkID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE risk_decisions decision SET status='resolved',current_score=0 WHERE decision.status='active' AND EXISTS(SELECT 1 FROM risk_identity_signals signal WHERE signal.decision_id=decision.decision_id AND signal.network_identity_id=$1 AND signal.domain='ip' AND signal.status='resolved')`, networkID); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if err := refreshIdentityUserSummary(ctx, tx, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
