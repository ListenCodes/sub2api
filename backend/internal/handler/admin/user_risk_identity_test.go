package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

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
	listCalls int
}

func (s *riskSortedAdminStub) ListUsers(_ context.Context, page, _ int, _ service.UserListFilters, _, _ string) ([]service.User, int64, error) {
	s.listCalls++
	if page > 1 {
		return nil, 3, nil
	}
	return []service.User{
		{ID: 1, Email: "low@example.com", Status: service.StatusActive},
		{ID: 2, Email: "high@example.com", Status: service.StatusActive},
		{ID: 3, Email: "none@example.com", Status: service.StatusActive},
	}, 3, nil
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
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/admin/users":
			_, _ = writer.Write([]byte(`{"items":[{"id":1,"score":10,"risk_level":"low"},{"id":2,"score":90,"risk_level":"critical"}],"total":2}`))
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
	if payload.Data.Total != 3 || len(payload.Data.Items) != 3 || payload.Data.Items[0].ID != 2 || payload.Data.Items[1].ID != 1 || payload.Data.Items[2].ID != 3 {
		t.Fatalf("risk-sorted users = %+v total=%d", payload.Data.Items, payload.Data.Total)
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
