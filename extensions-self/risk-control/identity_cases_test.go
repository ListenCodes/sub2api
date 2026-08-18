package main

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWorkOverviewAggregatesActionableQueuesInOneQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH legacy_api_subjects AS .*risk_identity_signals.*risk_index AS .*case_counts AS .*assignee_id=\$1.*quality_cases AS .*FROM risk_review_cases quality_case.*status IN \('retry','failed'\).*SELECT case_counts.pending`).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{"pending", "mine", "observing", "at_risk", "data_quality"}).AddRow(4, 2, 3, 9, 1))

	got, err := NewSQLIdentityRepository(db).WorkOverview(context.Background(), 17)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"pending": 4, "mine": 2, "observing": 3, "at_risk": 9, "data_quality": 1}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %d, want %d", key, got[key], value)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
