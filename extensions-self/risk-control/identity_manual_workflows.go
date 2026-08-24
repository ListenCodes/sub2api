package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (r *SQLIdentityRepository) CreateReviewCase(ctx context.Context, userID, actorID int64, signalFamily, status, reason string) (RiskReviewCase, error) {
	signalFamily, status, reason = strings.TrimSpace(signalFamily), strings.TrimSpace(status), strings.TrimSpace(reason)
	if signalFamily == "" {
		signalFamily = "manual_review"
	}
	if status == "" {
		status = "pending"
	}
	if userID <= 0 || actorID <= 0 || (status != "pending" && status != "observing") || reason == "" || len([]rune(reason)) > 500 || len(signalFamily) > 80 {
		return RiskReviewCase{}, errors.New("invalid manual review case")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RiskReviewCase{}, err
	}
	defer tx.Rollback()
	var currentScore, historicalScore int
	_ = tx.QueryRowContext(ctx, `SELECT overall_score,GREATEST(overall_score,0) FROM risk_identity_user_summaries WHERE user_id=$1`, userID).Scan(&currentScore, &historicalScore)
	var item RiskReviewCase
	var opened, last time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO risk_review_cases(user_id,signal_family,status,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,opened_at,last_hit_at)
VALUES($1,$2,$3,$4,$5,'manual_review','observation',$6,NOW(),NOW())
RETURNING id,user_id,COALESCE(decision_id,''),signal_family,status,resolution,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,opened_at,last_hit_at`, userID, signalFamily, status, currentScore, historicalScore, actorID).Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &opened, &last)
	if err != nil {
		return RiskReviewCase{}, errors.New("an open manual case already exists for this user and signal family")
	}
	item.OpenedAt, item.LastHitAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano)
	if err := tx.Commit(); err != nil {
		return RiskReviewCase{}, err
	}
	return item, nil
}

func (r *SQLIdentityRepository) ObserveReviewCase(ctx context.Context, caseID, actorID int64, reason string) (RiskReviewCase, error) {
	reason = strings.TrimSpace(reason)
	if caseID <= 0 || actorID <= 0 || reason == "" || len([]rune(reason)) > 500 {
		return RiskReviewCase{}, errors.New("invalid observe request")
	}
	var item RiskReviewCase
	var opened, last time.Time
	err := r.db.QueryRowContext(ctx, `UPDATE risk_review_cases SET status='observing',assignee_id=$2,updated_at=NOW() WHERE id=$1 AND status IN ('pending','in_review','observing') RETURNING id,user_id,COALESCE(decision_id,''),signal_family,status,resolution,current_score,historical_max_score,primary_signal,evidence_strength,assignee_id,opened_at,last_hit_at`, caseID, actorID).Scan(&item.ID, &item.UserID, &item.DecisionID, &item.SignalFamily, &item.Status, &item.Resolution, &item.CurrentScore, &item.HistoricalMaxScore, &item.PrimarySignal, &item.EvidenceStrength, &item.AssigneeID, &opened, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return RiskReviewCase{}, errors.New("review case is unavailable")
	}
	item.OpenedAt, item.LastHitAt = opened.UTC().Format(time.RFC3339Nano), last.UTC().Format(time.RFC3339Nano)
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
FROM risk_network_identities network LEFT JOIN risk_identity_signals signal ON signal.network_identity_id=network.id AND signal.status='active' AND signal.domain IN ('ip','composite') WHERE network.id=$1 GROUP BY network.id`, networkID).Scan(&impact.CurrentLabel, &impact.AffectedSignalCount, &impact.AffectedAccountCount, &impact.AffectedDecisionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return NetworkLabelImpact{}, errors.New("network identity is unavailable")
	}
	if err != nil {
		return NetworkLabelImpact{}, err
	}
	if safeSharedNetworkLabel(proposedLabel) {
		impact.ResolvedDomains = []string{"ip", "composite"}
	}
	if proposedLabel == "" && impact.CurrentLabel != "" {
		impact.RequiresRebuild = true
	}
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM risk_shared_network_labels WHERE network_identity_id=$1`, networkID); err != nil {
		return NetworkLabelImpact{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_shared_network_label_history(network_identity_id,action,label,reason,actor_id) VALUES($1,'revoke',$2,$3,$4)`, networkID, impact.CurrentLabel, reason, actorID); err != nil {
		return NetworkLabelImpact{}, err
	}
	if err := tx.Commit(); err != nil {
		return NetworkLabelImpact{}, err
	}
	impact.ProposedLabel = ""
	impact.RequiresRebuild = true
	return impact, nil
}
