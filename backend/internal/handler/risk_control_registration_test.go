package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestLoginRiskEventsIncludeIPAndDeviceAssociations(t *testing.T) {
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

	handler := &AuthHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.11:5432"
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	login := LoginRequest{Email: "user@example.com"}

	if err := handler.preflightLoginRisk(context, login); err != nil {
		t.Fatalf("preflightLoginRisk() error = %v", err)
	}
	handler.reportLoginRisk(context, login, &service.User{ID: 7, Username: "Alice", Status: "active"}, nil)

	deviceHashes := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		select {
		case report := <-reports:
			if report.IPHash != service.HashRiskValue("203.0.113.11") {
				t.Fatalf("event %q ip_hash = %q, want trusted client IP hash", report.EventType, report.IPHash)
			}
			if report.DeviceHash == "" {
				t.Fatalf("event %q device_hash is empty", report.EventType)
			}
			deviceHashes = append(deviceHashes, report.DeviceHash)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for login risk event")
		}
	}
	if deviceHashes[0] != deviceHashes[1] {
		t.Fatalf("one login request used different device hashes: %q and %q", deviceHashes[0], deviceHashes[1])
	}
	if len(recorder.Result().Cookies()) == 0 || recorder.Result().Cookies()[0].Name != "risk_device" {
		t.Fatal("login risk collection did not persist a risk_device cookie")
	}
}

func TestLoginRiskV2LegacyEventDoesNotCarryIdentityHashes(t *testing.T) {
	reports := make(chan service.RiskEventReport, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/internal/events" {
			var report service.RiskEventReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				t.Errorf("decode risk event: %v", err)
			} else {
				reports <- report
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("RISK_CONTROL_URL", server.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-risk-secret")
	t.Setenv("RISK_IDENTITY_V2_ENABLED", "true")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_KEY", "01234567890123456789012345678901")

	handler := &AuthHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.11:5432"
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	handler.reportLoginRisk(context, LoginRequest{Email: "user@example.com"}, &service.User{ID: 7, Username: "Alice", Status: "active"}, nil)

	select {
	case report := <-reports:
		if report.IPHash != "" || report.DeviceHash != "" {
			t.Fatalf("V2 login leaked legacy identity hashes: ip=%q device=%q", report.IPHash, report.DeviceHash)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for V1 compatibility event")
	}
}

func TestLoginPreflightV2StillEvaluatesLegacyNonIdentityRules(t *testing.T) {
	requests := make(chan service.RiskEventReport, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/internal/events/evaluate" {
			var report service.RiskEventReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				t.Errorf("decode risk evaluation: %v", err)
			} else {
				requests <- report
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow","mode":"shadow"}`))
	}))
	defer server.Close()
	t.Setenv("RISK_CONTROL_URL", server.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-risk-secret")
	t.Setenv("RISK_IDENTITY_V2_ENABLED", "true")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_KEY", "01234567890123456789012345678901")

	handler := &AuthHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "8.8.8.8:5432"
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	if err := handler.preflightLoginRisk(context, LoginRequest{Email: "user@example.com"}); err != nil {
		t.Fatalf("preflightLoginRisk() error = %v", err)
	}

	select {
	case report := <-requests:
		if report.EventType != "login_attempt" || report.IPHash != "" || report.DeviceHash != "" {
			t.Fatalf("V2 preflight evaluation = %#v", report)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("V2 login preflight skipped legacy non-identity evaluation")
	}
}

func TestLoginPreflightV2ForcesFailOpenWhenExtensionIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("RISK_CONTROL_URL", server.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-risk-secret")
	t.Setenv("RISK_CONTROL_DECISION_FAIL_MODE", "closed")
	t.Setenv("RISK_IDENTITY_V2_ENABLED", "true")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_KEY", "01234567890123456789012345678901")

	handler := &AuthHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "8.8.8.8:5432"
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	if err := handler.preflightLoginRisk(context, LoginRequest{Email: "user@example.com"}); err != nil {
		t.Fatalf("V2 login must fail open when extension is unavailable: %v", err)
	}
}
