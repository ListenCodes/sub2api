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

func TestAllowedAccountMonitorPathIsNarrow(t *testing.T) {
	allowed := [][2]string{
		{http.MethodGet, "/overview"}, {http.MethodGet, "/accounts"},
		{http.MethodGet, "/accounts/42"}, {http.MethodGet, "/accounts/42/models"},
		{http.MethodGet, "/accounts/42/users"}, {http.MethodGet, "/accounts/42/errors"},
		{http.MethodGet, "/accounts/42/trends"},
		{http.MethodGet, "/attempts"}, {http.MethodGet, "/data-quality"},
		{http.MethodGet, "/thresholds"}, {http.MethodPut, "/thresholds"},
		{http.MethodPost, "/rebuild-jobs"}, {http.MethodGet, "/rebuild-jobs/7"},
		{http.MethodGet, "/group-monitor/groups"}, {http.MethodGet, "/group-monitor/groups/42"},
	}
	for _, item := range allowed {
		if !allowedAccountMonitorPath(item[0], item[1]) {
			t.Fatalf("%s %s must be allowed", item[0], item[1])
		}
	}
	for _, item := range [][2]string{
		{http.MethodPost, "/accounts"}, {http.MethodGet, "/accounts/no"},
		{http.MethodDelete, "/thresholds"}, {http.MethodGet, "/secret"},
		{http.MethodGet, "/accounts/42/credentials"},
		{http.MethodPost, "/group-monitor/groups"},
		{http.MethodGet, "/group-monitor/groups/no"},
		{http.MethodGet, "/group-monitor/groups/42/requests"},
		{http.MethodGet, "/group-monitor/../secret"},
	} {
		if allowedAccountMonitorPath(item[0], item[1]) {
			t.Fatalf("%s %s must be rejected", item[0], item[1])
		}
	}
}

func TestProxyAccountMonitorForwardsSignedAdminRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/account-monitor/overview" || r.URL.Query().Get("page") != "1" {
			t.Errorf("upstream URL = %s", r.URL.String())
		}
		if r.Header.Get("X-Risk-Actor-ID") != "7" || r.Header.Get("X-Risk-Signature") == "" {
			t.Errorf("missing signed actor headers")
		}
		_, _ = fmt.Fprint(w, `{"attempts":1}`)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "01234567890123456789012345678901")

	h := NewCustomUserHandler(nil, nil, service.NewRiskControlClientFromEnv())
	engine := gin.New()
	engine.GET("/api/v1/admin/extensions-self/account-monitor/*path", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.ProxyAccountMonitor(c)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/extensions-self/account-monitor/overview?page=1", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"attempts":1}` {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProxyAccountMonitorRejectsOversizedBodyWithoutCallingUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "01234567890123456789012345678901")

	h := NewCustomUserHandler(nil, nil, service.NewRiskControlClientFromEnv())
	engine := gin.New()
	engine.PUT("/api/v1/admin/extensions-self/account-monitor/*path", h.ProxyAccountMonitor)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/extensions-self/account-monitor/thresholds", bytes.NewReader(make([]byte, maxRiskControlProxyBody+1)))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls.Load())
	}
}
