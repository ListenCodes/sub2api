package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func extensionsProxyEngine(client *service.RiskControlClient) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &AuthHandler{riskControlClient: client}
	engine.GET("/api/v1/extensions-self/homepage/*path", handler.ProxyExtensionsHomepage)
	engine.HEAD("/api/v1/extensions-self/homepage/*path", handler.ProxyExtensionsHomepage)
	return engine
}

func TestProxyExtensionsHomepageReturnsStaticAsset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/homepage/" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte("<html>homepage-marker</html>"))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "01234567890123456789012345678901")
	engine := extensionsProxyEngine(service.NewRiskControlClientFromEnv())

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/extensions-self/homepage/", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("proxy response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if recorder.Body.String() != "<html>homepage-marker</html>" {
		t.Fatalf("proxy body = %q", recorder.Body.String())
	}
}

func TestProxyExtensionsHomepageReturnsUnavailableWithoutClient(t *testing.T) {
	engine := extensionsProxyEngine(nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/extensions-self/homepage/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("proxy response status = %d, want 503", recorder.Code)
	}
}
