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
}

func TestUpsertIdentityReviewCasePromotesWithinOneOpenCase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewSQLIdentityRepository(db)
	event := PersistedIdentityEvent{UserID: 42, OccurredAt: time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)}

	mock.ExpectQuery(`(?s)INSERT INTO risk_review_cases.*review_due_at,observation_goal.*VALUES\(\$1,\$2,\$3,\$4,\$5,\$5,\$6,\$7,\$9,\$10,\$8,\$8\).*ON CONFLICT\(user_id,signal_family\) WHERE status IN \('pending','in_review','observing'\) DO UPDATE SET.*status=CASE.*review_due_at=CASE`).
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
	if err != nil || replayed || resolved.Status != "resolved" || resolved.Revision != 4 {
		t.Fatalf("resolved=%+v replayed=%v error=%v", resolved, replayed, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id,user_id,status,resolution,resolution_reason,resolution_request_id,revision,assignee_id FROM risk_review_cases WHERE id=\$1 FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "status", "resolution", "resolution_reason", "resolution_request_id", "revision", "assignee_id"}).AddRow(9, 42, "resolved", "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1", 4, 7))
	mock.ExpectCommit()

	replayedCase, replayed, err := repo.ResolveReviewCase(context.Background(), 9, 7, "confirmed_abuse", "multiple signals confirm abuse", "resolve-9-attempt-1", 3)
	if err != nil || !replayed || replayedCase.Status != "resolved" || replayedCase.Revision != 4 {
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
	mock.ExpectQuery(`WHERE id=\$1 AND status='in_review' AND assignee_id=\$2`).
		WithArgs(int64(9), int64(7), sqlmock.AnyArg(), "verify the next registration", 3).
		WillReturnError(sql.ErrNoRows)
	if _, err := repo.ObserveReviewCaseWithReview(context.Background(), 9, 7, "needs observation", time.Now().Add(time.Hour), "verify the next registration", 3); err == nil {
		t.Fatal("expected unavailable case when the actor does not own an in-review case")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
