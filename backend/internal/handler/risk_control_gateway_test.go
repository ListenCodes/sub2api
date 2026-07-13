package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
}
