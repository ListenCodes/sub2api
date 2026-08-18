package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMemoryRiskIndexMergesRiskUsersAndDropsZeroScoreObservations(t *testing.T) {
	repository := NewMemoryRepository(nil)
	repository.subjects[1] = RiskSubject{ID: 1, UserID: 1, RiskType: "login_failure", RiskLevel: "high", Score: 65, LastEventAt: "2026-08-18T08:00:00Z"}
	repository.subjects[2] = RiskSubject{ID: 2, UserID: 2, RiskType: "api_request", RiskLevel: "none", Score: 0, LastEventAt: "2026-08-18T09:00:00Z"}

	items, allIDs, total, err := repository.ListRiskIndex(context.Background(), RiskIndexFilter{MinScore: -1, MaxScore: -1, SortBy: "risk_score", SortOrder: "desc"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].UserID != 1 || len(allIDs) != 1 || allIDs[0] != 1 {
		t.Fatalf("items=%+v ids=%v total=%d", items, allIDs, total)
	}
}

func TestSQLRiskIndexQueriesUnifiedProjectionAndScansAStablePage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	filter := RiskIndexFilter{RiskLevel: "high", MinScore: 60, MaxScore: 79, SortBy: "last_event_at", SortOrder: "asc"}
	args := []driver.Value{"", "high", 60, 79, "", false, sqlmock.AnyArg()}
	mock.ExpectQuery(`(?s)WITH legacy_api_subjects AS .*risk_identity_signals.*risk_index AS .*SELECT COUNT\(\*\) FROM risk_index`).
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`(?s)WITH legacy_api_subjects AS .*risk_identity_signals.*array_agg\(user_id ORDER BY user_id\)`).
		WillReturnRows(sqlmock.NewRows([]string{"ids"}).AddRow("{4,9}"))
	mock.ExpectQuery(`(?s)COALESCE\(current_case.status IN \('pending','in_review'\),FALSE\).*FROM risk_index WHERE .*ORDER BY last_event_at ASC,score DESC,user_id ASC LIMIT \$8 OFFSET \$9`).
		WithArgs(args[0], args[1], args[2], args[3], args[4], args[5], args[6], 2, 0).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "risk_type", "risk_level", "score", "reason", "event_count", "ip_count", "device_count", "last_action", "pending", "last_event_at", "processing_status", "case_id", "case_status", "assignee_id", "evidence_strength", "decision_id", "historical_max_score"}).
			AddRow(4, "login_failure", "high", 60, "登录失败", 5, 1, 1, "review", true, "2026-08-18T08:00:00.000Z", "pending", 0, "", 0, "", "", 60).
			AddRow(9, "v2_registration_ip_accounts", "high", 70, "", 3, 3, 0, "observe", false, "2026-08-18T09:00:00.000Z", "observing", 12, "observing", 7, "weak", "decision-9", 70))

	items, ids, total, err := NewSQLRepository(db).ListRiskIndex(context.Background(), filter, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 || items[0].UserID != 4 || items[1].UserID != 9 || fmt.Sprint(ids) != "[4 9]" {
		t.Fatalf("items=%+v ids=%v total=%d", items, ids, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRiskIndexSignedAdminRouteReturnsServerPage(t *testing.T) {
	repository := NewMemoryRepository(nil)
	repository.subjects[8] = RiskSubject{ID: 8, UserID: 8, RiskType: "login_failure", Score: 70, LastEventAt: "2026-08-18T08:00:00Z"}
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "shadow"}, repository)
	request := signedRequest(http.MethodGet, "/api/v1/admin/risk-index?sort_by=risk_score&sort_order=desc&limit=20", nil, testSecret, "nonce-risk-index", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Items       []RiskIndexItem `json:"items"`
		RiskUserIDs []int64         `json:"risk_user_ids"`
		Total       int             `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].UserID != 8 || len(payload.RiskUserIDs) != 1 || payload.RiskUserIDs[0] != 8 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestRiskIndexPostgresCombinesGenericAndIdentityOnlyRisk(t *testing.T) {
	ctx := context.Background()
	db := openIsolatedRiskTestDB(t)
	if err := ApplySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO risk_subjects(user_id,risk_type,risk_level,score,reason,last_event_at) VALUES
 (100,'login_failure','high',65,'登录失败达到复核阈值',NOW()-INTERVAL '1 hour'),
 (300,'api_request','none',0,'正常 API 请求观察',NOW())`); err != nil {
		t.Fatal(err)
	}
	cfg := enabledIdentityTestConfig()
	identity, err := NewIdentityService(cfg, NewSQLIdentityRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	identityUsers := []int64{100, 1001, 1002, 1003, 1004}
	for index, userID := range identityUsers {
		if _, err := identity.Ingest(ctx, registrationIdentityReport(fmt.Sprintf("risk-index-%d", index), userID, base.Add(time.Duration(index)*time.Second), "8.8.4.4", "shared-browser")); err != nil {
			t.Fatal(err)
		}
	}
	items, allIDs, total, err := NewSQLRepository(db).ListRiskIndex(ctx, RiskIndexFilter{MinScore: -1, MaxScore: -1, SortBy: "risk_score", SortOrder: "desc"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]RiskIndexItem{}
	for _, item := range items {
		seen[item.UserID] = item
	}
	if total != len(identityUsers) || len(seen) != len(identityUsers) || seen[100].Score <= 65 || seen[100].RiskType == "login_failure" || seen[1004].Score <= 0 || len(allIDs) != total {
		t.Fatalf("generic=%+v identity=%+v total=%d all_ids=%v", seen[100], seen[1004], total, allIDs)
	}
	if _, exists := seen[300]; exists {
		t.Fatalf("zero-score observation appeared in risk index: %+v", seen[300])
	}
	overview, err := NewSQLIdentityRepository(db).WorkOverview(ctx, 1)
	if err != nil || overview["at_risk"] != total {
		t.Fatalf("overview=%v total=%d err=%v", overview, total, err)
	}
}

func TestMemoryRiskIndexPaginationIsStableInBothDirections(t *testing.T) {
	repository := NewMemoryRepository(nil)
	repository.subjects[1] = RiskSubject{ID: 1, UserID: 1, RiskType: "login_failure", Score: 70, LastEventAt: "2026-08-18T08:00:00Z"}
	repository.subjects[2] = RiskSubject{ID: 2, UserID: 2, RiskType: "login_failure", Score: 70, LastEventAt: "2026-08-18T09:00:00Z"}
	repository.subjects[3] = RiskSubject{ID: 3, UserID: 3, RiskType: "content_risk", Score: 40, LastEventAt: "2026-08-18T10:00:00Z"}
	repository.subjects[4] = RiskSubject{ID: 4, UserID: 4, RiskType: "quota_abuse", Score: 20, LastEventAt: "2026-08-18T11:00:00Z"}

	for _, order := range []string{"desc", "asc"} {
		seen := map[int64]bool{}
		for page := 0; page < 2; page++ {
			items, _, total, err := repository.ListRiskIndex(context.Background(), RiskIndexFilter{MinScore: -1, MaxScore: -1, SortBy: "risk_score", SortOrder: order}, 2, page*2)
			if err != nil || total != 4 || len(items) != 2 {
				t.Fatalf("order=%s page=%d items=%+v total=%d err=%v", order, page+1, items, total, err)
			}
			for _, item := range items {
				if seen[item.UserID] {
					t.Fatalf("order=%s duplicate user %d", order, item.UserID)
				}
				seen[item.UserID] = true
			}
		}
		if len(seen) != 4 {
			t.Fatalf("order=%s users=%v", order, seen)
		}
	}
}
