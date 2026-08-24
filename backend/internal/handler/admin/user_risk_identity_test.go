package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestProxyUserRiskIdentitySearchKeepsExactIPOutOfTheURL(t *testing.T) {
	var searchCalls int
	var auditBodies [][]byte
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/users/9/ip-identities/search":
			searchCalls++
			if request.Method != http.MethodPost {
				t.Errorf("method = %s", request.Method)
			}
			if request.URL.RawQuery != "" {
				t.Errorf("raw query leaked exact IP: %q", request.URL.RawQuery)
			}
			var payload struct {
				Query string `json:"query"`
				Page  int    `json:"page"`
				Limit int    `json:"limit"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Query != "8.8.8.8" || payload.Page != 1 || payload.Limit != 20 {
				t.Errorf("payload = %+v", payload)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"items":[],"total":0,"page":1,"page_size":20}`))
		case "/api/v1/internal/audit":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			auditBodies = append(auditBodies, body)
			writer.WriteHeader(http.StatusAccepted)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "identity-search-test-secret")

	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.POST("/admin/users/:id/ip-identities/search", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		handler.ProxyUserRiskIdentity(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/users/9/ip-identities/search", bytes.NewBufferString(`{"query":"8.8.8.8","page":1,"limit":20}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || searchCalls != 1 {
		t.Fatalf("status=%d searchCalls=%d body=%s", recorder.Code, searchCalls, recorder.Body.String())
	}
	if len(auditBodies) != 1 || bytes.Contains(auditBodies[0], []byte("8.8.8.8")) || bytes.Contains(auditBodies[0], []byte(`\"query\"`)) {
		t.Fatalf("dedicated audit leaked identity search input: %s", auditBodies)
	}
}

func TestProxyUserRiskIdentityReusesHashedAuditKeyWithinOneViewSession(t *testing.T) {
	var auditReports []service.RiskAuditReport
	const sessionID = "drawer-session-sensitive-1"
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/users/9/identity-summary":
			_, _ = writer.Write([]byte(`{"user_id":9,"domains":[]}`))
		case "/api/v1/admin/users/9/ip-identities":
			_, _ = writer.Write([]byte(`{"items":[],"total":0}`))
		case "/api/v1/internal/audit":
			var report service.RiskAuditReport
			if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
				t.Error(err)
			}
			auditReports = append(auditReports, report)
			writer.WriteHeader(http.StatusAccepted)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "identity-session-test-secret")

	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/users/:id/:section", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		handler.ProxyUserRiskIdentity(c)
	})
	for _, path := range []string{"/admin/users/9/identity-summary", "/admin/users/9/ip-identities"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Risk-View-Session", sessionID)
		engine.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}

	if len(auditReports) != 2 || auditReports[0].AuditKey == "" || auditReports[0].AuditKey != auditReports[1].AuditKey {
		t.Fatalf("session audit reports = %+v", auditReports)
	}
	encoded, err := json.Marshal(auditReports)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(sessionID)) {
		t.Fatalf("audit payload leaked the raw view session: %s", encoded)
	}
	if auditReports[0].Metadata["section"] != "identity-summary" || auditReports[1].Metadata["section"] != "ip-identities" {
		t.Fatalf("audit sections = %+v", auditReports)
	}
}

func TestProxyRiskWorkOverviewReturnsOneServerAggregate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/admin/work-overview" {
			t.Errorf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"pending":3,"mine":2,"observing":4,"at_risk":7,"data_quality":1}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "work-overview-test-secret")

	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	context.Request = httptest.NewRequest(http.MethodGet, "/admin/user-risk/work-overview", nil)
	handler.ProxyRiskWorkOverview(context)

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"pending":3,"mine":2,"observing":4,"at_risk":7,"data_quality":1}` {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProxyRiskIdentityRebuildForwardsApprovedDryRunID(t *testing.T) {
	var approvedDryRunID int64
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/admin/risk-rebuilds" {
			t.Errorf("path = %q", request.URL.Path)
		}
		var payload struct {
			ApprovedDryRunID int64 `json:"approved_dry_run_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		approvedDryRunID = payload.ApprovedDryRunID
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":10,"dry_run":false,"status":"completed","approved_dry_run_id":9}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "rebuild-test-secret")

	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	context.Request = httptest.NewRequest(http.MethodPost, "/admin/risk-rebuilds", bytes.NewBufferString(`{"approved_dry_run_id":9}`))
	handler.ProxyRiskIdentityRebuild(context)

	if recorder.Code != http.StatusOK || approvedDryRunID != 9 {
		t.Fatalf("status=%d approved dry run=%d body=%s", recorder.Code, approvedDryRunID, recorder.Body.String())
	}
}

type identityBatchAdminStub struct {
	service.AdminService
	batchCalls  int
	singleCalls int
}

func TestRiskLevelForScoreMatchesIdentityExtension(t *testing.T) {
	tests := map[int]string{-1: "none", 0: "none", 1: "low", 29: "low", 30: "medium", 59: "medium", 60: "high", 79: "high", 80: "critical", 100: "critical"}
	for score, expected := range tests {
		if actual := riskLevelForScore(score); actual != expected {
			t.Fatalf("riskLevelForScore(%d) = %q, want %q", score, actual, expected)
		}
	}
}

func (s *identityBatchAdminStub) GetUsersForRiskIdentity(_ context.Context, ids []int64) ([]service.User, error) {
	s.batchCalls++
	return []service.User{{ID: ids[0], Email: "linked@example.com", Username: "linked", Status: service.StatusActive, CreatedAt: time.Unix(1, 0).UTC()}}, nil
}

func (s *identityBatchAdminStub) GetUserIncludeDeleted(_ context.Context, _ int64) (*service.User, error) {
	s.singleCalls++
	return nil, service.ErrUserNotFound
}

func TestEnrichAssociatedUsersUsesOneBatchLookup(t *testing.T) {
	stub := &identityBatchAdminStub{}
	handler := &CustomUserHandler{adminService: stub}
	body := handler.enrichAssociatedUsers(context.Background(), []byte(`{"items":[{"user_id":7},{"user_id":7}],"total":2,"page":1,"page_size":20}`))

	if stub.batchCalls != 1 || stub.singleCalls != 0 {
		t.Fatalf("identity account lookups = batch %d single %d", stub.batchCalls, stub.singleCalls)
	}
	var result struct {
		Items []struct {
			Account map[string]any `json:"account"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if got := result.Items[0].Account["email"]; got != "linked@example.com" {
		t.Fatalf("enriched email = %#v", got)
	}
}

type identityNoBatchAdminStub struct {
	service.AdminService
	singleCalls int
}

func (s *identityNoBatchAdminStub) GetUserIncludeDeleted(_ context.Context, _ int64) (*service.User, error) {
	s.singleCalls++
	return nil, service.ErrUserNotFound
}

func TestEnrichAssociatedUsersDoesNotFallBackToPerUserQueries(t *testing.T) {
	stub := &identityNoBatchAdminStub{}
	handler := &CustomUserHandler{adminService: stub}
	body := handler.enrichAssociatedUsers(context.Background(), []byte(`{"items":[{"user_id":7}],"total":1,"page":1,"page_size":20}`))

	if stub.singleCalls != 0 {
		t.Fatalf("single user lookups = %d", stub.singleCalls)
	}
	if !json.Valid(body) {
		t.Fatal("fallback response is not valid JSON")
	}
	var result struct {
		Items []struct {
			Account map[string]any `json:"account"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Items[0].Account["availability"] != "unavailable" || result.Items[0].Account["deleted"] != false {
		t.Fatalf("lookup failure was presented as deletion: %#v", result.Items[0].Account)
	}
}

func TestRiskIdentityProxyErrorsUseNoStore(t *testing.T) {
	handler := &CustomUserHandler{}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/identity-health", nil)
	handler.ProxyRiskIdentityHealth(context)
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

type identityCountingBatchAdminStub struct {
	service.AdminService
	batchSizes []int
}

type riskSortedAdminStub struct {
	service.AdminService
	listCalls  int
	batchSizes []int
}

func (s *riskSortedAdminStub) ListUsers(_ context.Context, page, pageSize int, filters service.UserListFilters, _, _ string) ([]service.User, int64, error) {
	s.listCalls++
	excluded := map[int64]bool{}
	for _, id := range filters.ExcludeIDs {
		excluded[id] = true
	}
	all := []service.User{
		{ID: 1, Email: "low@example.com", Status: service.StatusActive},
		{ID: 2, Email: "high@example.com", Status: service.StatusActive},
		{ID: 3, Email: "none@example.com", Status: service.StatusActive},
		{ID: 4, Email: "normal@example.com", Status: service.StatusActive},
		{ID: 5, Email: "other@example.com", Status: service.StatusActive},
	}
	users := make([]service.User, 0, len(all))
	for _, user := range all {
		if !excluded[user.ID] {
			users = append(users, user)
		}
	}
	start := (page - 1) * pageSize
	if start >= len(users) {
		return nil, int64(len(users)), nil
	}
	end := start + pageSize
	if end > len(users) {
		end = len(users)
	}
	return users[start:end], int64(len(users)), nil
}

func (s *riskSortedAdminStub) GetUsersForRiskIdentity(_ context.Context, ids []int64) ([]service.User, error) {
	s.batchSizes = append(s.batchSizes, len(ids))
	users := make([]service.User, 0, len(ids))
	for _, id := range ids {
		users = append(users, service.User{ID: id, Email: "risk-" + strconv.FormatInt(id, 10) + "@example.com", Status: service.StatusActive})
	}
	return users, nil
}

func (s *identityCountingBatchAdminStub) GetUsersForRiskIdentity(_ context.Context, ids []int64) ([]service.User, error) {
	s.batchSizes = append(s.batchSizes, len(ids))
	users := make([]service.User, 0, len(ids))
	for _, id := range ids {
		users = append(users, service.User{ID: id, Email: "user-" + strconv.FormatInt(id, 10) + "@example.com", Status: service.StatusActive})
	}
	return users, nil
}

func TestRiskIdentityAccountCompletionUsesBoundedBatches(t *testing.T) {
	stub := &identityCountingBatchAdminStub{}
	handler := &CustomUserHandler{adminService: stub}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/admin/user-risk/users", nil)
	ids := make([]int64, 205)
	for index := range ids {
		ids[index] = int64(index + 1)
	}
	accounts, available := handler.riskIdentityAccounts(ginContext, ids)
	if !available || len(accounts) != len(ids) || len(stub.batchSizes) != 3 || stub.batchSizes[0] != 100 || stub.batchSizes[1] != 100 || stub.batchSizes[2] != 5 {
		t.Fatalf("available=%v accounts=%d batch sizes=%v", available, len(accounts), stub.batchSizes)
	}
}

func TestListAllRiskCasesUsesExtensionPaginationLimit(t *testing.T) {
	requestedPages := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestedPages = append(requestedPages, request.URL.Query().Get("page")+":"+request.URL.Query().Get("limit"))
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		count := 100
		if page == 2 {
			count = 1
		}
		items := make([]riskCaseListItem, count)
		for index := range items {
			items[index] = riskCaseListItem{ID: int64((page-1)*100 + index + 1), UserID: int64((page-1)*100 + index + 1)}
		}
		_ = json.NewEncoder(writer).Encode(riskCaseListPage{Items: items, Total: 101, Page: page, PageSize: 100})
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "risk-case-page-test-secret")
	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?search=user", nil)

	cases, ok := handler.listAllRiskCases(ginContext, url.Values{})
	if !ok || len(cases.Items) != 101 || len(requestedPages) != 2 || requestedPages[0] != "1:100" || requestedPages[1] != "2:100" {
		t.Fatalf("ok=%v items=%d requests=%v", ok, len(cases.Items), requestedPages)
	}
}

func TestRiskAccountMatchesDoesNotAssumeNumericIDType(t *testing.T) {
	account := map[string]any{"id": float64(42), "email": "alice@example.com", "username": "alice", "status": "active"}
	if !riskAccountMatches(account, true, "42", "active") {
		t.Fatal("numeric JSON account id did not match")
	}
}

func TestListAllUserRiskUsersSortsRiskServerSideWithoutDroppingZeroRiskAccounts(t *testing.T) {
	var requestedRiskOffsets []string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/risk-index":
			requestedRiskOffsets = append(requestedRiskOffsets, request.URL.Query().Get("offset")+":"+request.URL.Query().Get("limit"))
			_, _ = writer.Write([]byte(`{"items":[{"id":2,"risk_type":"login_failure","risk_level":"high","score":60,"last_event_at":"2026-08-18T00:00:00Z"},{"id":1,"risk_type":"login_failure","risk_level":"high","score":60,"last_event_at":"2026-08-17T00:00:00Z"}],"risk_user_ids":[1,2],"total":2}`))
		case "/api/v1/admin/identity-summaries":
			_, _ = writer.Write([]byte(`{"items":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "risk-sort-test-secret")
	stub := &riskSortedAdminStub{}
	handler := &CustomUserHandler{adminService: stub, riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?view=all&sort_by=risk_score&sort_order=desc&page=1&page_size=3", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Items []struct {
				ID        int64 `json:"id"`
				RiskScore int   `json:"risk_score"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Total != 5 || len(payload.Data.Items) != 3 || payload.Data.Items[0].ID != 2 || payload.Data.Items[1].ID != 1 || payload.Data.Items[2].ID != 3 {
		t.Fatalf("risk-sorted users = %+v total=%d", payload.Data.Items, payload.Data.Total)
	}
	if stub.listCalls != 1 || len(stub.batchSizes) != 1 || stub.batchSizes[0] != 2 {
		t.Fatalf("account completion was not bounded to the response page: list=%d batch=%v", stub.listCalls, stub.batchSizes)
	}
	if len(requestedRiskOffsets) != 2 || requestedRiskOffsets[0] != "0:1" || requestedRiskOffsets[1] != "0:2" {
		t.Fatalf("risk index requests = %v", requestedRiskOffsets)
	}
}

func TestListAllUserRiskUsersPaginatesAcrossRiskAndNormalBoundary(t *testing.T) {
	riskItems := []map[string]any{
		{"id": 2, "risk_type": "login_failure", "risk_level": "high", "score": 70, "last_event_at": "2026-08-18T00:00:00Z"},
		{"id": 1, "risk_type": "content_risk", "risk_level": "medium", "score": 40, "last_event_at": "2026-08-17T00:00:00Z"},
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/risk-index":
			items := append([]map[string]any(nil), riskItems...)
			if request.URL.Query().Get("sort_order") == "asc" {
				items[0], items[1] = items[1], items[0]
			}
			offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
			end := minIntValue(offset+limit, len(items))
			if offset >= len(items) {
				items = []map[string]any{}
			} else {
				items = items[offset:end]
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": items, "risk_user_ids": []int64{1, 2}, "total": 2})
		case "/api/v1/admin/identity-summaries":
			_, _ = writer.Write([]byte(`{"items":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "risk-pagination-test-secret")

	for _, test := range []struct {
		order string
		want  []int64
	}{{order: "desc", want: []int64{2, 1, 3, 4, 5}}, {order: "asc", want: []int64{3, 4, 5, 1, 2}}} {
		stub := &riskSortedAdminStub{}
		handler := &CustomUserHandler{adminService: stub, riskControlClient: service.NewRiskControlClientFromEnv()}
		engine := gin.New()
		engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
		seen := []int64{}
		for page := 1; page <= 3; page++ {
			recorder := httptest.NewRecorder()
			path := fmt.Sprintf("/admin/user-risk/users?view=all&sort_by=risk_score&sort_order=%s&page=%d&page_size=2", test.order, page)
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("order=%s page=%d status=%d body=%s", test.order, page, recorder.Code, recorder.Body.String())
			}
			var payload struct {
				Data struct {
					Items []struct {
						ID int64 `json:"id"`
					} `json:"items"`
					Total int64 `json:"total"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Data.Total != 5 {
				t.Fatalf("order=%s page=%d total=%d", test.order, page, payload.Data.Total)
			}
			for _, item := range payload.Data.Items {
				seen = append(seen, item.ID)
			}
		}
		if fmt.Sprint(seen) != fmt.Sprint(test.want) {
			t.Fatalf("order=%s users=%v want=%v", test.order, seen, test.want)
		}
		for _, size := range stub.batchSizes {
			if size > 2 {
				t.Fatalf("order=%s unbounded page completion batches=%v", test.order, stub.batchSizes)
			}
		}
	}
}

func TestCreatedAtSortCompletesUnifiedRiskForOnlyTheAccountPage(t *testing.T) {
	var userIDs, includeAllIDs string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/risk-index":
			userIDs = request.URL.Query().Get("user_ids")
			includeAllIDs = request.URL.Query().Get("include_all_ids")
			_, _ = writer.Write([]byte(`{"items":[{"id":2,"risk_type":"v2_registration_ip_accounts","risk_level":"critical","score":90,"last_event_at":"2026-08-18T00:00:00Z"}],"risk_user_ids":[],"total":1}`))
		case "/api/v1/admin/identity-summaries":
			_, _ = writer.Write([]byte(`{"items":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "created-sort-test-secret")
	handler := &CustomUserHandler{adminService: &riskSortedAdminStub{}, riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?view=all&sort_by=created_at&sort_order=desc&page=1&page_size=2", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Items []struct {
				ID        int64 `json:"id"`
				RiskScore int   `json:"risk_score"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Items) != 2 || payload.Data.Items[1].ID != 2 || payload.Data.Items[1].RiskScore != 90 {
		t.Fatalf("items=%+v", payload.Data.Items)
	}
	if userIDs != "1,2" || includeAllIDs != "false" {
		t.Fatalf("risk completion query user_ids=%q include_all_ids=%q", userIDs, includeAllIDs)
	}
}

func TestListAllUserRiskUsersPropagatesRiskServiceFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"error":"risk unavailable"}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "risk-failure-test-secret")
	handler := &CustomUserHandler{adminService: &riskSortedAdminStub{}, riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?view=all&page=1&page_size=3", nil))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != `{"error":"risk unavailable"}` {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRiskFilteredIndexNeverRequestsAllRiskUserIDs(t *testing.T) {
	includeAllIDs := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/risk-index":
			includeAllIDs = append(includeAllIDs, request.URL.Query().Get("include_all_ids"))
			_, _ = writer.Write([]byte(`{"items":[{"id":2,"risk_type":"login_failure","risk_level":"high","score":60}],"risk_user_ids":[],"total":1}`))
		case "/api/v1/admin/identity-summaries":
			_, _ = writer.Write([]byte(`{"items":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "risk-filter-id-test-secret")
	handler := &CustomUserHandler{adminService: &riskSortedAdminStub{}, riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?view=all&risk_only=true&page=1&page_size=2", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(includeAllIDs) != 2 || includeAllIDs[0] != "false" || includeAllIDs[1] != "false" {
		t.Fatalf("include_all_ids requests = %v", includeAllIDs)
	}
}

func TestAccountFilteredRiskRowsReturnAllIDsOnlyForTheInitialMembershipSnapshot(t *testing.T) {
	includeAllIDs := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/risk-index":
			includeAllIDs = append(includeAllIDs, request.URL.Query().Get("include_all_ids"))
			_, _ = writer.Write([]byte(`{"items":[{"id":2,"risk_type":"login_failure","risk_level":"high","score":60}],"risk_user_ids":[2],"total":1}`))
		case "/api/v1/admin/identity-summaries":
			_, _ = writer.Write([]byte(`{"items":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "account-filter-id-test-secret")
	handler := &CustomUserHandler{adminService: &riskSortedAdminStub{}, riskControlClient: service.NewRiskControlClientFromEnv()}
	engine := gin.New()
	engine.GET("/admin/user-risk/users", handler.ListUserRiskUsers)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/user-risk/users?view=all&search=example.com&page=1&page_size=2", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(includeAllIDs) != 2 || includeAllIDs[0] != "" || includeAllIDs[1] != "false" {
		t.Fatalf("include_all_ids requests = %v", includeAllIDs)
	}
}

func TestIdentityAccountPayloadDistinguishesAllCompletionStates(t *testing.T) {
	deletedAt := time.Now().UTC()
	tests := []struct {
		name string
		user *service.User
		want string
	}{
		{name: "available", user: &service.User{ID: 1, Email: "one@example.com", Status: service.StatusActive}, want: "available"},
		{name: "not evaluable missing email", user: &service.User{ID: 2, Status: service.StatusActive}, want: "not_evaluable"},
		{name: "not evaluable bad status", user: &service.User{ID: 3, Email: "three@example.com", Status: "broken"}, want: "not_evaluable"},
		{name: "deleted", user: &service.User{ID: 4, Email: "four@example.com", Status: service.StatusDisabled, DeletedAt: &deletedAt}, want: "deleted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := identityAccountPayload(test.user)["availability"]; got != test.want {
				t.Fatalf("availability = %#v, want %q", got, test.want)
			}
		})
	}
	if got := riskAccountRow(9, nil, false)["account_availability"]; got != "unavailable" {
		t.Fatalf("lookup failure availability = %#v", got)
	}
	if got := riskAccountRow(9, nil, true)["account_availability"]; got != "deleted" {
		t.Fatalf("missing record availability = %#v", got)
	}
}
