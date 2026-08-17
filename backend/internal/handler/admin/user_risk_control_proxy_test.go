package admin

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestProxyRiskControlForwardsAuthenticatedAdminAndAllowlistedPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/rules" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Risk-Actor-ID") != "7" {
			t.Errorf("actor = %q", r.Header.Get("X-Risk-Actor-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[]}`)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "proxy-test-secret")

	h := NewCustomUserHandler(nil, nil, serviceClientFromEnvForTest())
	engine := gin.New()
	engine.GET("/admin/user-risk-control/*path", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.ProxyRiskControl(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/user-risk-control/rules?limit=10", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func serviceClientFromEnvForTest() *service.RiskControlClient {
	return service.NewRiskControlClientFromEnv()
}

func TestProxyRiskControlRejectsUnallowlistedPath(t *testing.T) {
	h := NewCustomUserHandler(nil, nil, serviceClientFromEnvForTest())
	engine := gin.New()
	engine.GET("/admin/user-risk-control/*path", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.ProxyRiskControl(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/user-risk-control/secret", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestProxyRiskControlAllowlistsRuleCreation(t *testing.T) {
	if !allowedRiskControlPath(http.MethodGet, "/identity-rules") {
		t.Fatal("GET /identity-rules must be allowlisted for authenticated admin proxy")
	}
	if !allowedRiskControlPath(http.MethodPost, "/rules") {
		t.Fatal("POST /rules must be allowlisted for authenticated admin proxy")
	}
	if allowedRiskControlPath(http.MethodPost, "/rules/secret") {
		t.Fatal("arbitrary POST rule path must remain blocked")
	}
	if !allowedRiskControlPath(http.MethodPost, "/users/42/processed") {
		t.Fatal("POST /users/:id/processed must be allowlisted")
	}
	if !allowedRiskControlPath(http.MethodPost, "/identity-rules/v2_registration_ip_accounts/disable") {
		t.Fatal("POST /identity-rules/:code/disable must be allowlisted")
	}
	if allowedRiskControlPath(http.MethodPost, "/identity-rules/v2_registration_ip_accounts/enable") {
		t.Fatal("online identity rule enable must remain blocked")
	}
	if allowedRiskControlPath(http.MethodPost, "/users/not-an-id/processed") {
		t.Fatal("processed path must require a numeric user id")
	}
}

func TestProxyRiskControlRejectsTraversalInAllowlistPrefixes(t *testing.T) {
	for _, path := range []string{
		"/users/../secret",
		"/users/%2e%2e/secret",
		"/users/%252e%252e/secret",
		"/rules/../secret",
		"/rules/%2e%2e/secret",
		"/rules/%252e%252e/secret",
		"/users\\..\\secret",
		"/users/%5c..%5csecret",
	} {
		if allowedRiskControlPath(http.MethodGet, path) || allowedRiskControlPath(http.MethodPut, path) {
			t.Fatalf("path traversal must be rejected: %s", path)
		}
	}
}

func TestProxyRiskControlRejectsOversizedBodyWithoutCallingUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "proxy-test-secret")

	h := NewCustomUserHandler(nil, nil, serviceClientFromEnvForTest())
	engine := gin.New()
	engine.POST("/admin/user-risk-control/*path", h.ProxyRiskControl)
	request := httptest.NewRequest(http.MethodPost, "/admin/user-risk-control/rules", bytes.NewReader(make([]byte, maxRiskControlProxyBody+1)))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls.Load())
	}
}

func TestProxyRiskControlPreservesUpstreamErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprint(w, `{"error":"rule revision conflict"}`)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "proxy-test-secret")

	h := NewCustomUserHandler(nil, nil, serviceClientFromEnvForTest())
	engine := gin.New()
	engine.PUT("/admin/user-risk-control/*path", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.ProxyRiskControl(c)
	})
	request := httptest.NewRequest(http.MethodPut, "/admin/user-risk-control/rules/login_failure_burst", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if response.Body.String() != `{"error":"rule revision conflict"}` {
		t.Fatalf("body = %q, want upstream JSON", response.Body.String())
	}
}
