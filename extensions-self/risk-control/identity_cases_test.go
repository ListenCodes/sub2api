package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReviewCaseSchemaEnforcesOneUnresolvedCasePerFamily(t *testing.T) {
	raw, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	if !strings.Contains(schema, "idx_risk_review_cases_unresolved_family") || !strings.Contains(schema, "WHERE status IN ('pending','in_review','observing')") {
		t.Fatal("schema must use one partial unique index for every unresolved case status")
	}
	if strings.Contains(schema, "CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_review_cases_observing_family") {
		t.Fatal("observing cases must not use a separate uniqueness domain")
	}
	if !strings.Contains(schema, "version=9") || !strings.Contains(schema, "Review whether the weak signal persists or escalates") {
		t.Fatal("V9 must reconcile duplicate open cases and backfill observation review context")
	}
	if !strings.Contains(schema, "version=10") || !strings.Contains(schema, "resolved_by_shared_network") {
		t.Fatal("V10 must preserve provenance for signals resolved by a shared-network label")
	}
}

func TestRefreshAllIdentityReviewCasesClearsVanishedSignalContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(`(?s)WITH case_state AS .*LEFT JOIN LATERAL.*signal.status='active'.*UPDATE risk_review_cases.*current_score=case_state.current_score.*primary_signal=case_state.primary_signal`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := refreshAllIdentityReviewCases(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertIdentityReviewCasePromotesWithinOneOpenCase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewSQLIdentityRepository(db)
	event := PersistedIdentityEvent{UserID: 42, OccurredAt: time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)}

	mock.ExpectQuery(`(?s)INSERT INTO risk_review_cases.*review_due_at,observation_goal.*VALUES\(\$1,\$2,\$3,\$4,\$5,\$5,\$6,\$7,\$9,\$10,\$8,\$8\).*ON CONFLICT\(user_id,signal_family\) WHERE status IN \('pending','in_review','observing'\) DO UPDATE SET.*status=CASE.*assignee_id=CASE WHEN risk_review_cases.status IN \('observing','in_review'\).*review_due_at=CASE`).
		WithArgs(int64(42), "decision-1", "registration_identity", "observing", 40, "identity_ip_reuse", "weak", event.OccurredAt, sqlmock.AnyArg(), "Review whether the weak signal persists or escalates").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(17))
	mock.ExpectExec(`INSERT INTO risk_case_evidence`).
		WithArgs(int64(17), int64(91), []byte(`{"signal":"ip"}`), event.OccurredAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.upsertIdentityReviewCase(context.Background(), event, 91, "decision-1", "registration_identity", "identity_ip_reuse", 40, []byte(`{"signal":"ip"}`)); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkOverviewAggregatesActionableQueuesInOneQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH legacy_api_subjects AS .*risk_identity_signals.*risk_index AS .*case_counts AS .*assignee_id=\$1.*quality_cases AS .*FROM risk_review_cases quality_case.*status IN \('retry','failed'\).*SELECT case_counts.unassigned`).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{"unassigned", "mine", "due", "open", "at_risk", "data_quality"}).AddRow(4, 2, 3, 6, 9, 1))

	got, err := NewSQLIdentityRepository(db).WorkOverview(context.Background(), 17)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"unassigned_pending": 4, "my_in_review": 2, "review_due": 3, "all_open": 6, "pending": 4, "mine": 2, "observing": 3, "at_risk": 9, "data_quality": 1}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %d, want %d", key, got[key], value)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimReviewCaseAllowsAnotherAdministratorToTakeOverDueObservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH claimable AS .*status='observing' AND review_due_at IS NOT NULL AND review_due_at<=NOW\(\).*FOR UPDATE.*UPDATE risk_review_cases.*claimable.previous_assignee_id`).
		WithArgs(int64(9), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "decision_id", "signal_family", "status", "resolution", "current_score", "historical_max_score", "primary_signal", "evidence_strength", "assignee_id", "created_by", "observation_goal", "resolution_reason", "revision", "opened_at", "last_hit_at", "last_activity_at", "previous_assignee_id"}).
			AddRow(9, 42, "decision-9", "registration_identity", "in_review", "", 45, 60, "v2_registration_ip_accounts", "weak", 11, 7, "", "", 4, time.Now(), time.Now(), time.Now(), 7))

	item, err := NewSQLIdentityRepository(db).ClaimReviewCase(context.Background(), 9, 11)
	if err != nil || item.Status != "in_review" || item.AssigneeID != 11 || item.PreviousAssigneeID != 7 || item.Revision != 4 {
		t.Fatalf("item=%+v error=%v", item, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLabelSharedNetworkRestoresSuppressedSignalsWhenSafetyLabelIsRemoved(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE risk_identity_signals`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COALESCE.*risk_shared_network_labels`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"label"}).AddRow("company"))
	mock.ExpectExec(`INSERT INTO risk_shared_network_labels`).
		WithArgs(int64(5), "public_proxy", "verified proxy exit", int64(8)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO risk_shared_network_label_history`).
		WithArgs(int64(5), "public_proxy", "verified proxy exit", int64(8)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT DISTINCT user_id FROM risk_identity_signals.*resolved_by_shared_network`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(42))
	mock.ExpectExec(`(?s)UPDATE risk_identity_signals signal SET.*resolved_by_shared_network=FALSE`).
		WithArgs(int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)WITH affected AS .*UPDATE risk_decisions`).
		WithArgs(int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)WITH case_state AS .*UPDATE risk_review_cases`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO risk_identity_user_summaries`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewSQLIdentityRepository(db).LabelSharedNetwork(context.Background(), 5, 8, "public_proxy", "verified proxy exit"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveReviewCaseCommitsFeedbackAndSupportsIdempotentReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewSQLIdentityRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id,user_id,status,resolution,resolution_reason,resolution_request_id,revision,assignee_id FROM risk_review_cases WHERE id=\$1 FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "resolution", "resolution_reason", "resolution_request_id", "revision", "assignee_id"}).AddRow(9, 42, "in_review", "", "", "", 3, 7))
	mock.ExpectExec(`UPDATE risk_review_cases SET status='resolved'`).
		WithArgs(int64(9), int64(7), "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO risk_review_feedback`).
		WithArgs(int64(9), int64(7), "confirmed_abuse", "multiple signals confirm abuse").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	resolved, replayed, err := repo.ResolveReviewCase(context.Background(), 9, 7, "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1", 3)
	if err != nil || replayed || resolved.Status != "resolved" || resolved.ResolutionRequestID != "resolve-9-attempt-1" || resolved.Revision != 4 {
		t.Fatalf("resolved=%+v replayed=%v error=%v", resolved, replayed, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id,user_id,status,resolution,resolution_reason,resolution_request_id,revision,assignee_id FROM risk_review_cases WHERE id=\$1 FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "resolution", "resolution_reason", "resolution_request_id", "revision", "assignee_id"}).AddRow(9, 42, "resolved", "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1", 4, 7))
	mock.ExpectCommit()

	replayedCase, replayed, err := repo.ResolveReviewCase(context.Background(), 9, 7, "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1", 3)
	if err != nil || !replayed || replayedCase.Status != "resolved" || replayedCase.ResolutionRequestID != "resolve-9-attempt-1" || replayedCase.Revision != 4 {
		t.Fatalf("resolved=%+v replayed=%v error=%v", replayedCase, replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestObserveReviewCaseRequiresFutureDueTimeAndGoal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewSQLIdentityRepository(db)

	for _, test := range []struct {
		name string
		due  time.Time
		goal string
	}{
		{name: "missing due", goal: "verify the next registration"},
		{name: "past due", due: time.Now().Add(-time.Minute), goal: "verify the next registration"},
		{name: "missing goal", due: time.Now().Add(time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := repo.ObserveReviewCaseWithReview(context.Background(), 9, 7, "needs observation", test.due, test.goal, 3)
			if err == nil {
				t.Fatal("expected observe validation error")
			}
		})
	}
	mock.ExpectQuery(`WHERE id=\$1 AND \(\(status='in_review' AND assignee_id=\$2\) OR \(status='pending' AND assignee_id=0\)\)`).
		WithArgs(int64(9), int64(7), sqlmock.AnyArg(), "verify the next registration", 3).
		WillReturnError(sql.ErrNoRows)
	if _, err := repo.ObserveReviewCaseWithReview(context.Background(), 9, 7, "needs observation", time.Now().Add(time.Hour), "verify the next registration", 3); err == nil {
		t.Fatal("expected unavailable case when the actor does not own an in-review case")
	}
	due := time.Now().Add(2 * time.Hour)
	mock.ExpectQuery(`WHERE id=\$1 AND \(\(status='in_review' AND assignee_id=\$2\) OR \(status='pending' AND assignee_id=0\)\)`).
		WithArgs(int64(10), int64(7), sqlmock.AnyArg(), "verify pending evidence", 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "decision_id", "signal_family", "status", "resolution", "current_score", "historical_max_score", "primary_signal", "evidence_strength", "assignee_id", "created_by", "review_due_at", "observation_goal", "resolution_reason", "revision", "opened_at", "last_hit_at", "last_activity_at"}).
			AddRow(10, 42, "decision-10", "registration_identity", "observing", "", 40, 40, "v2_registration_ip_accounts", "weak", 7, 0, due, "verify pending evidence", "", 3, time.Now(), time.Now(), time.Now()))
	observing, err := repo.ObserveReviewCaseWithReview(context.Background(), 10, 7, "needs observation", due, "verify pending evidence", 2)
	if err != nil || observing.Status != "observing" || observing.AssigneeID != 7 || observing.Revision != 3 {
		t.Fatalf("observing=%+v error=%v", observing, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
