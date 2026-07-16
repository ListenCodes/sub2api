package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRiskControlClientReportsGenericEventWithoutSensitiveFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["event_type"] != "login_failure" {
			t.Fatalf("event_type = %v", payload["event_type"])
		}
		if payload["user_id"] != float64(42) {
			t.Fatalf("user_id = %v", payload["user_id"])
		}
		if _, ok := payload["password"]; ok {
			t.Fatal("password must not be reported")
		}
		if _, ok := payload["device_id"]; ok {
			t.Fatal("raw device id must not be reported")
		}
		if r.URL.Path != "/api/v1/internal/events" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := &RiskControlClient{baseURL: server.URL, secret: []byte("test-risk-secret"), http: server.Client()}
	err := client.ReportEvent(context.Background(), RiskEventReport{
		EventKey: "login-42-1", EventType: "login_failure", UserID: 42,
		UsernameSnapshot: "alice", AccountStatusSnapshot: "active", ErrorCode: "INVALID_CREDENTIALS",
		Reason: "用户名或密码错误", Endpoint: "/api/v1/auth/login", HTTPStatus: 401,
	})
	if err != nil {
		t.Fatalf("ReportEvent() error = %v", err)
	}
}

func TestRiskControlClientEvaluatesEventAndRetriesTransientFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Path != "/api/v1/internal/events/evaluate" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"decision":"ban","score":91,"risk_level":"critical","reason":"login failure burst"}`)
	}))
	defer server.Close()

	client := &RiskControlClient{
		baseURL: server.URL,
		secret:  []byte("test-risk-secret"),
		http:    &http.Client{Timeout: time.Second},
	}
	decision, err := client.EvaluateEvent(context.Background(), RiskEventReport{
		EventKey: "login-42-2", EventType: "login_failure", UserID: 42,
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if decision.Action != "ban" || decision.Score != 91 || decision.RiskLevel != "critical" {
		t.Fatalf("decision = %+v", decision)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRiskControlClientAddsStableAuditKeyForRetries(t *testing.T) {
	attempts := 0
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload RiskAuditReport
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		keys = append(keys, payload.AuditKey)
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := &RiskControlClient{baseURL: server.URL, secret: []byte("test-risk-secret"), http: &http.Client{Timeout: time.Second}}
	if err := client.ReportAudit(context.Background(), RiskAuditReport{Action: "auto_ban", TargetType: "user", TargetID: "42", Result: "success", Reason: "burst"}); err != nil {
		t.Fatalf("ReportAudit() error = %v", err)
	}
	if attempts != 2 || len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("attempts=%d keys=%#v, want same non-empty key across retries", attempts, keys)
	}
}
