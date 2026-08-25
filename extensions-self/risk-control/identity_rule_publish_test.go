package main

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type testSQLStateError string

func (e testSQLStateError) Error() string    { return string(e) }
func (e testSQLStateError) SQLState() string { return string(e) }

func TestDecodeIdentityRuleApprovalAcceptsOneStepPublish(t *testing.T) {
	body := []byte(`{"base_revision":2,"window_seconds":900,"threshold":4,"score":90,"configured_action":"reject_candidate","enabled":true,"simulation_id":10,"confirmed":true,"confirmation":"PUBLISH v2_registration_composite_accounts"}`)
	approval, targetRevision, err := decodeIdentityRuleApproval(body)
	if err != nil {
		t.Fatal(err)
	}
	if targetRevision != 0 || approval.Draft == nil || approval.Draft.BaseRevision != 2 || approval.Draft.WindowSeconds != 900 || approval.Draft.Threshold != 4 || approval.Draft.Score != 90 || approval.Draft.ConfiguredAction != "reject_candidate" || approval.Enabled == nil || !*approval.Enabled {
		t.Fatalf("approval = %+v, target revision = %d", approval, targetRevision)
	}
	if err := validateIdentityRuleApproval("v2_registration_composite_accounts", "reject_candidate", approval); err != nil {
		t.Fatalf("direct publish rejected without simulation or confirmation: %v", err)
	}
}

func TestApplyIdentityRuleRevisionPublishesAndAuditsInOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	code := "v2_registration_composite_accounts"
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT revision,enabled,domain,signal_family,subject_kind,window_seconds,threshold,score,configured_action FROM risk_identity_rules WHERE code=$1 FOR UPDATE`)).
		WithArgs(code).
		WillReturnRows(sqlmock.NewRows([]string{"revision", "enabled", "domain", "signal_family", "subject_kind", "window_seconds", "threshold", "score", "configured_action"}).
			AddRow(2, true, "composite", "registration_identity", "user", 600, 3, 90, "reject_candidate"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE risk_rule_versions SET active_until=COALESCE(active_until,NOW()) WHERE rule_kind='identity' AND rule_code=$1 AND revision=$2`)).
		WithArgs(code, 2).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE risk_identity_rules SET enabled=$2,window_seconds=$3,threshold=$4,score=$5,configured_action=$6,revision=$7,active_from=NOW(),active_until=NULL,updated_at=NOW() WHERE code=$1`)).
		WithArgs(code, true, 900, 4, 90, "reject_candidate", 3).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO risk_rule_versions(rule_kind,rule_code,revision,signal_family,domain,active_from,enabled,rule_snapshot) VALUES('identity',$1,$2,$3,$4,NOW(),$5,$6)`)).
		WithArgs(code, 3, "registration_identity", "composite", true, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT user_id FROM risk_identity_signals WHERE rule_code=$1 AND status='active' AND user_id>0`)).
		WithArgs(code).WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE risk_identity_signals SET status='superseded' WHERE rule_code=$1 AND status='active'`)).
		WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE risk_decisions decision SET status='superseded',current_score=0 WHERE decision.status='active' AND EXISTS(SELECT 1 FROM risk_identity_signals signal WHERE signal.decision_id=decision.decision_id AND signal.rule_code=$1)`)).
		WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO risk_audit_logs(actor_id,action,target_type,target_id,result,reason,metadata) VALUES($1,$2,'identity_rule',$3,'success',$4,$5)`)).
		WithArgs(int64(7), "publish_identity_rule", code, "管理员直接发布规则", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM risk_identity_rule_drafts WHERE rule_code=$1`)).
		WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	draft := IdentityRuleDraft{RuleCode: code, BaseRevision: 2, WindowSeconds: 900, Threshold: 4, Score: 90, ConfiguredAction: "reject_candidate"}
	revision, err := NewSQLIdentityRepository(db).applyIdentityRuleRevision(context.Background(), code, draft, true, 7, "publish_identity_rule", identityRulePublishApproval{})
	if err != nil || revision != 3 {
		t.Fatalf("revision = %d, error = %v", revision, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyIdentityRuleRevisionRejectsStaleRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	code := "v2_registration_composite_accounts"
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT revision,enabled,domain,signal_family,subject_kind,window_seconds,threshold,score,configured_action FROM risk_identity_rules WHERE code=$1 FOR UPDATE`)).
		WithArgs(code).
		WillReturnRows(sqlmock.NewRows([]string{"revision", "enabled", "domain", "signal_family", "subject_kind", "window_seconds", "threshold", "score", "configured_action"}).
			AddRow(3, true, "composite", "registration_identity", "user", 600, 3, 90, "reject_candidate"))
	mock.ExpectRollback()

	draft := IdentityRuleDraft{RuleCode: code, BaseRevision: 2, WindowSeconds: 900, Threshold: 4, Score: 90, ConfiguredAction: "reject_candidate"}
	_, err = NewSQLIdentityRepository(db).applyIdentityRuleRevision(context.Background(), code, draft, true, 7, "publish_identity_rule", identityRulePublishApproval{})
	if !errors.Is(err, ErrRuleRevisionConflict) {
		t.Fatalf("error = %v, want ErrRuleRevisionConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeIdentityRuleApprovalRejectsPartialDirectChange(t *testing.T) {
	if _, _, err := decodeIdentityRuleApproval([]byte(`{"base_revision":2,"threshold":4}`)); err == nil {
		t.Fatal("partial direct publish payload was accepted")
	}
}

func TestApplyIdentityRuleRevisionRejectsNoOpBeforeClearingSignals(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("v2_registration_composite_accounts").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT revision,enabled,domain,signal_family,subject_kind,window_seconds,threshold,score,configured_action FROM risk_identity_rules WHERE code=\$1 FOR UPDATE`).
		WithArgs("v2_registration_composite_accounts").
		WillReturnRows(sqlmock.NewRows([]string{"revision", "enabled", "domain", "signal_family", "subject_kind", "window_seconds", "threshold", "score", "configured_action"}).
			AddRow(2, true, "composite", "registration_identity", "user", 600, 3, 90, "reject_candidate"))
	mock.ExpectRollback()

	draft := IdentityRuleDraft{RuleCode: "v2_registration_composite_accounts", BaseRevision: 2, WindowSeconds: 600, Threshold: 3, Score: 90, ConfiguredAction: "reject_candidate"}
	_, err = NewSQLIdentityRepository(db).applyIdentityRuleRevision(context.Background(), draft.RuleCode, draft, true, 7, "publish_identity_rule", identityRulePublishApproval{})
	if !errors.Is(err, ErrIdentityRuleNoChanges) {
		t.Fatalf("error = %v, want ErrIdentityRuleNoChanges", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackIdentityRuleRestoresTargetEnabledState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	code := "v2_registration_composite_accounts"
	mock.ExpectQuery(`SELECT revision FROM risk_identity_rules`).WithArgs(code).
		WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(5))
	mock.ExpectQuery(`SELECT rule_snapshot FROM risk_rule_versions`).WithArgs(code, 2).
		WillReturnRows(sqlmock.NewRows([]string{"rule_snapshot"}).AddRow([]byte(`{"window_seconds":600,"threshold":3,"score":90,"configured_action":"reject_candidate"}`)))
	mock.ExpectQuery(`SELECT enabled FROM risk_rule_versions`).WithArgs(code, 2).
		WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT revision,enabled,domain,signal_family,subject_kind,window_seconds,threshold,score,configured_action FROM risk_identity_rules`).WithArgs(code).
		WillReturnRows(sqlmock.NewRows([]string{"revision", "enabled", "domain", "signal_family", "subject_kind", "window_seconds", "threshold", "score", "configured_action"}).
			AddRow(5, true, "composite", "registration_identity", "user", 600, 3, 90, "reject_candidate"))
	mock.ExpectExec(`UPDATE risk_rule_versions SET active_until`).WithArgs(code, 5).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE risk_identity_rules SET enabled=\$2`).WithArgs(code, false, 600, 3, 90, "reject_candidate", 6).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO risk_rule_versions`).WithArgs(code, 6, "registration_identity", "composite", false, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT DISTINCT user_id FROM risk_identity_signals`).WithArgs(code).WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	mock.ExpectExec(`UPDATE risk_identity_signals SET status='superseded'`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`UPDATE risk_decisions decision SET status='superseded'`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO risk_audit_logs`).WithArgs(int64(7), "rollback_identity_rule", code, "管理员直接回滚规则", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`DELETE FROM risk_identity_rule_drafts`).WithArgs(code).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	revision, err := NewSQLIdentityRepository(db).RollbackIdentityRule(context.Background(), code, 2, 7, identityRulePublishApproval{})
	if err != nil || revision != 6 {
		t.Fatalf("revision = %d, error = %v", revision, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityRuleSerializationErrorsAreRevisionConflicts(t *testing.T) {
	for _, state := range []testSQLStateError{"40001", "40P01"} {
		if !isIdentityRuleRevisionConflict(state) {
			t.Fatalf("SQLSTATE %s must be treated as a revision conflict", state)
		}
	}
	if isIdentityRuleRevisionConflict(testSQLStateError("23505")) {
		t.Fatal("unrelated SQLSTATE was treated as a revision conflict")
	}
}

func TestRefreshIdentityReviewCasesKeepsMissingDecisionNullable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(`SELECT case_row.id,COALESCE\(best.score,0\) current_score,COALESCE\(best.rule_code,''\) primary_signal,best.decision_id`).
		WithArgs(sqlmock.AnyArg(), "registration_identity").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := refreshIdentityReviewCases(context.Background(), db, []int64{42}, "registration_identity"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
