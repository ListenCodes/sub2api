package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestClassifyRiskEventSeparatesGatewayRiskSources(t *testing.T) {
	tests := []struct {
		name         string
		observation  RiskEventObservation
		eventType    string
		evidenceType string
	}{
		{name: "success", observation: RiskEventObservation{HTTPStatus: http.StatusOK}, eventType: "api_request", evidenceType: "api"},
		{name: "quota", observation: RiskEventObservation{HTTPStatus: http.StatusTooManyRequests}, eventType: "quota_exceeded", evidenceType: "quota"},
		{name: "content", observation: RiskEventObservation{HTTPStatus: http.StatusForbidden, ErrorCode: "content_policy_violation"}, eventType: "content_risk", evidenceType: "content"},
		{name: "upstream", observation: RiskEventObservation{HTTPStatus: http.StatusBadGateway, UpstreamStatus: http.StatusGatewayTimeout}, eventType: "upstream_error", evidenceType: "upstream"},
		{name: "api", observation: RiskEventObservation{HTTPStatus: http.StatusBadRequest}, eventType: "api_error", evidenceType: "api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEvent, gotEvidence := classifyRiskEvent(tt.observation)
			if gotEvent != tt.eventType || gotEvidence != tt.evidenceType {
				t.Fatalf("classifyRiskEvent() = (%q, %q), want (%q, %q)", gotEvent, gotEvidence, tt.eventType, tt.evidenceType)
			}
		})
	}
}

func TestSetRiskEventContextPreservesSpecificClassification(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetRiskEventContext(c, "content_risk", "content_policy_violation", "blocked")
	SetRiskEventContext(c, "api_error", "content_policy_violation", "blocked")

	value, ok := c.Get(riskEventTypeKey)
	if !ok || value != "content_risk" {
		t.Fatalf("event type = %v, want content_risk", value)
	}
}

func TestRiskEventMiddlewareReportsAuthenticatedReadRequests(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		if !shouldReportRiskMethod(method) {
			t.Fatalf("shouldReportRiskMethod(%q) = false, want true", method)
		}
	}
	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		if shouldReportRiskMethod(method) {
			t.Fatalf("shouldReportRiskMethod(%q) = true, want false", method)
		}
	}

	reports := make(chan service.RiskEventReport, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report service.RiskEventReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode risk event: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		reports <- report
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("RISK_CONTROL_URL", server.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-risk-secret")

	client := service.NewRiskControlClientFromEnv()
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99})
		c.Next()
	})
	engine.Use(RiskEventMiddleware(client))
	engine.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	select {
	case report := <-reports:
		if report.IPHash != service.HashRiskValue("203.0.113.10") {
			t.Fatalf("ip_hash = %q, want trusted client IP hash", report.IPHash)
		}
		if report.DeviceHash != service.HashRiskValue("api-key:99") {
			t.Fatalf("device_hash = %q, want API key association hash", report.DeviceHash)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for risk event report")
	}
}

func TestRiskEventMiddlewareWhenFiltersAfterDownstreamContext(t *testing.T) {
	reports := make(chan service.RiskEventReport, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report service.RiskEventReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode risk event: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		reports <- report
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("RISK_CONTROL_URL", server.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-risk-secret")

	client := service.NewRiskControlClientFromEnv()
	filter := func(c *gin.Context) bool {
		apiKey, ok := middleware2.GetAPIKeyFromContext(c)
		return ok && apiKey != nil && apiKey.Group != nil
	}

	grouped := gin.New()
	grouped.Use(RiskEventMiddlewareWhen(client, filter))
	grouped.GET("/v1/models", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99, Group: &service.Group{ID: 7}})
		c.Status(http.StatusOK)
	})
	grouped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	select {
	case report := <-reports:
		if report.UserID != 42 {
			t.Fatalf("reported user_id = %d, want 42", report.UserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for grouped risk event report")
	}

	ungrouped := gin.New()
	ungrouped.Use(RiskEventMiddlewareWhen(client, filter))
	ungrouped.GET("/v1/models", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99})
		c.Status(http.StatusOK)
	})
	ungrouped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	select {
	case report := <-reports:
		t.Fatalf("unexpected ungrouped risk event report: %+v", report)
	case <-time.After(150 * time.Millisecond):
	}
}
