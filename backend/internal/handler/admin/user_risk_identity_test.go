package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
