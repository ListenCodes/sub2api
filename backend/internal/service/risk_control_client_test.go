package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRiskControlClientSignsRegistrationReport(t *testing.T) {
	const secret = "test-risk-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil { t.Fatalf("read body: %v", err) }
		if r.Header.Get("X-Risk-Timestamp") == "" || r.Header.Get("X-Risk-Nonce") == "" || r.Header.Get("X-Risk-Signature") == "" {
			t.Fatal("expected internal request authentication headers")
		}
		if !strings.Contains(string(body), `"subject_id":"42"`) { t.Fatalf("unexpected body: %s", body) }
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &RiskControlClient{baseURL: server.URL, secret: []byte(secret), http: server.Client()}
	err := client.ReportRegistration(context.Background(), RiskRegistrationReport{RequestID: "req-1", SubjectID: "42", Email: "person@example.com", IP: "203.0.113.10", DeviceID: "device-1"})
	if err != nil { t.Fatalf("ReportRegistration() error = %v", err) }
}
