package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (r *SQLIdentityRepository) CreateReviewCase(ctx context.Context, userID, actorID int64, signalFamily, status, reason string) (RiskReviewCase, error) {
	return r.CreateReviewCaseWithObservation(ctx, userID, actorID, signalFamily, status, reason, time.Time{}, "")
}

func (r *SQLIdentityRepository) CreateReviewCaseWithObservation(ctx context.Context, userID, actorID int64, signalFamily, status, reason string, reviewDueAt time.Time, observationGoal string) (RiskReviewCase, error) {
	signalFamily, status, reason = strings.TrimSpace(signalFamily), strings.TrimSpace(status), strings.TrimSpace(reason)
	observationGoal = strings.TrimSpace(observationGoal)
	if signalFamily == "" {
		signalFamily = "manual_review"
	}
	if status == "" {
		status = "pending"
	}
	if userID <= 0 || actorID <= 0 || (status != "pending" && status != "observing") || reason == "" || len([]rune(reason)) > 500 || len(signalFamily) > 80 || len([]rune(observationGoal)) > 500 {
		return RiskReviewCase{}, errors.New("invalid manual review case")
	}
	if status == "observing" && (reviewDueAt.IsZero() || !reviewDueAt.After(time.Now()) || observationGoal == "") {
		return RiskReviewCase{}, errors.New("observation goal and a future review time are required")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RiskReviewCase{}, err
	}
	defer tx.Rollback()
	var currentScore, historicalScore int
	_ = tx.QueryRowContext(ctx, `SELECT overall_score,GREATEST(overall_score,0) FROM risk_identity_user_summaries WHERE user_id=$1`, userID).Scan(&currentScore, &historicalScore)
	var item RiskReviewCase
	var opened, last, activity time.Time
	var due sql.NullTime
	assigneeID := int64(0)
	if status == "observing" {
		assigneeID = actorID
		due = sql.NullTime{Time: reviewDueAt.UTC(), Valid: true}
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO risk_review_cases(user_id,signal_family,status,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,created_by,review_due_at,observation_goal,opened_at,last_hit_at,last_activity_at)
VALUES($1,$2,$3,$4,$5,'manual_review','observation',$6,$7,$8,$9,NOW(),NOW(),NOW())
RETURNING id,user_id,COALESCE(decision_id,''),signal_family,status,resolution,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,created_by,review_due_at,observation_goal,resolution_reason,revision,opened_at,last_hit_at,last_activity_at`, userID, signalFamily, status, currentScore, historicalScore, assigneeID, actorID, due, observationGoal).Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &item.CreatedBy, &due, &item.ObservationGoal, &item.ResolutionReason, &item.Revision, &opened, &last, &activity)
	if err != nil {
		return RiskReviewCase{}, errors.New("an open manual case already exists for this user and signal family")
	}
	item.OpenedAt, item.LastHitAt, item.LastActivityAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano), activity.UTC().Format(time.RFC3339Nano)
	if due.Valid {
		item.ReviewDueAt = due.Time.UTC().Format(time.RFC3339Nano)
	}
	if err := tx.Commit(); err != nil {
		return RiskReviewCase{}, err
	}
	return item, nil
}

func (r *SQLIdentityRepository) ObserveReviewCase(ctx context.Context, caseID, actorID int64, reason string) (RiskReviewCase, error) {
	return r.ObserveReviewCaseWithReview(ctx, caseID, actorID, reason, time.Time{}, "", 0)
}

func (r *SQLIdentityRepository) ObserveReviewCaseWithReview(ctx context.Context, caseID, actorID int64, reason string, reviewDueAt time.Time, observationGoal string, expectedRevision int) (RiskReviewCase, error) {
	reason = strings.TrimSpace(reason)
	observationGoal = strings.TrimSpace(observationGoal)
	if caseID <= 0 || actorID <= 0 || reason == "" || len([]rune(reason)) > 500 || reviewDueAt.IsZero() || !reviewDueAt.After(time.Now()) || observationGoal == "" || len([]rune(observationGoal)) > 500 || expectedRevision < 0 {
		return RiskReviewCase{}, errors.New("invalid observe request")
	}
	var item RiskReviewCase
	var opened, last, activity, due time.Time
	err := r.db.QueryRowContext(ctx, `UPDATE risk_review_cases SET status='observing',assignee_id=$2,review_due_at=$3,observation_goal=$4,last_activity_at=NOW(),revision=revision+1,updated_at=NOW() WHERE id=$1 AND ((status='in_review' AND assignee_id=$2) OR (status='pending' AND assignee_id=0)) AND ($5=0 OR revision=$5) RETURNING id,user_id,COALESCE(decision_id,''),signal_family,status,resolution,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,created_by,review_due_at,observation_goal,resolution_reason,revision,opened_at,last_hit_at,last_activity_at`, caseID, actorID, reviewDueAt.UTC(), observationGoal, expectedRevision).Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &item.CreatedBy, &due, &item.ObservationGoal, &item.ResolutionReason, &item.Revision, &opened, &last, &activity)
	if errors.Is(err, sql.ErrNoRows) {
		return RiskReviewCase{}, errors.New("review case is unavailable")
	}
	item.OpenedAt, item.LastHitAt, item.LastActivityAt, item.ReviewDueAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano), activity.UTC().Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano)
	return item, err
}

func safeSharedNetworkLabel(label string) bool {
	switch strings.TrimSpace(label) {
	case "home", "company", "school", "trusted_egress", "mobile_cgnat":
		return true
	default:
		return false
	}
}

func validSharedNetworkLabel(label string) bool {
	switch strings.TrimSpace(label) {
	case "home", "company", "school", "public_proxy", "trusted_egress", "mobile_cgnat", "unknown":
		return true
	default:
		return false
	}
}

func (r *SQLIdentityRepository) NetworkLabelImpact(ctx context.Context, networkID int64, proposedLabel string) (NetworkLabelImpact, error) {
	proposedLabel = strings.TrimSpace(proposedLabel)
	if networkID <= 0 || (proposedLabel != "" && !validSharedNetworkLabel(proposedLabel)) {
		return NetworkLabelImpact{}, errors.New("invalid network label preview")
	}
	impact := NetworkLabelImpact{NetworkID: networkID, ProposedLabel: proposedLabel, ResolvedDomains: []string{}}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT label FROM risk_shared_network_labels WHERE network_identity_id=$1),''),
COUNT(signal.id)::bigint,COUNT(DISTINCT NULLIF(signal.user_id,0))::bigint,COUNT(DISTINCT NULLIF(signal.decision_id,''))::bigint
FROM risk_network_identities network LEFT JOIN risk_identity_signals signal ON signal.network_identity_id=network.id AND (signal.status='active' OR signal.resolved_by_shared_network) AND signal.domain IN ('ip','composite') WHERE network.id=$1 GROUP BY network.id`, networkID).Scan(&impact.CurrentLabel, &impact.AffectedSignalCount, &impact.AffectedAccountCount, &impact.AffectedDecisionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return NetworkLabelImpact{}, errors.New("network identity is unavailable")
	}
	if err != nil {
		return NetworkLabelImpact{}, err
	}
	if safeSharedNetworkLabel(proposedLabel) {
		impact.ResolvedDomains = []string{"ip", "composite"}
	}
	impact.RequiresRebuild = safeSharedNetworkLabel(impact.CurrentLabel) && !safeSharedNetworkLabel(proposedLabel)
	return impact, nil
}

func (r *SQLIdentityRepository) RevokeSharedNetworkLabel(ctx context.Context, networkID, actorID int64, reason string) (NetworkLabelImpact, error) {
	reason = strings.TrimSpace(reason)
	if networkID <= 0 || actorID <= 0 || reason == "" || len([]rune(reason)) > 500 {
		return NetworkLabelImpact{}, errors.New("invalid network label revoke request")
	}
	impact, err := r.NetworkLabelImpact(ctx, networkID, "")
	if err != nil {
		return NetworkLabelImpact{}, err
	}
	if impact.CurrentLabel == "" {
		return NetworkLabelImpact{}, errors.New("network identity has no label to revoke")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return NetworkLabelImpact{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `LOCK TABLE risk_identity_signals IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return NetworkLabelImpact{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT label FROM risk_shared_network_labels WHERE network_identity_id=$1 FOR UPDATE`, networkID).Scan(&impact.CurrentLabel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NetworkLabelImpact{}, errors.New("network identity has no label to revoke")
		}
		return NetworkLabelImpact{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM risk_shared_network_labels WHERE network_identity_id=$1`, networkID); err != nil {
		return NetworkLabelImpact{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_shared_network_label_history(network_identity_id,action,label,reason,actor_id) VALUES($1,'revoke',$2,$3,$4)`, networkID, impact.CurrentLabel, reason, actorID); err != nil {
		return NetworkLabelImpact{}, err
	}
	if safeSharedNetworkLabel(impact.CurrentLabel) {
		if err := restoreSharedNetworkSignals(ctx, tx, networkID); err != nil {
			return NetworkLabelImpact{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return NetworkLabelImpact{}, err
	}
	impact.ProposedLabel = ""
	impact.RequiresRebuild = safeSharedNetworkLabel(impact.CurrentLabel)
	return impact, nil
}
