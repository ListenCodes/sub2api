package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRiskStatusReasonRejectsWhitespaceOnlyInput(t *testing.T) {
	if err := validateRiskStatusReason(" \t\n "); err == nil {
		t.Fatal("whitespace-only reason must be rejected")
	}
}

func TestSetRiskStatusReturnsServiceUnavailableWithoutAdminService(t *testing.T) {
	engine := gin.New()
	handler := &CustomUserHandler{}
	engine.POST("/admin/users/:id/risk-status", handler.SetRiskStatus)
	request := httptest.NewRequest(http.MethodPost, "/admin/users/42/risk-status", bytes.NewBufferString(`{"status":"disabled","reason":"manual review","request_id":"request-456"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestBuildRiskStatusAuditReportKeepsFailureAndBatchContext(t *testing.T) {
	report := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, "active", "disabled", "failed", " repeated login failures ", "目标账号已被其他管理员处理", "batch-123", "request-456", "")
	if report.ActorID != 7 || report.TargetID != "42" || report.Action != "ban" || report.Result != "failed" {
		t.Fatalf("report identity = %+v", report)
	}
	if report.Reason != "repeated login failures" {
		t.Fatalf("reason = %q", report.Reason)
	}
	second := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, "active", "disabled", "success", " repeated login failures ", "", "batch-123", "request-456", "")
	if report.AuditKey == "" || report.AuditKey != second.AuditKey {
		t.Fatalf("audit key = %q", report.AuditKey)
	}
	if report.Metadata["failure_reason"] != "目标账号已被其他管理员处理" || report.Metadata["request_id"] != "request-456" || report.Metadata["before_status"] != "active" || report.Metadata["after_status"] != "disabled" {
		t.Fatalf("metadata = %#v", report.Metadata)
	}
}

func TestBuildRiskStatusAuditReportFallsBackToBatchKey(t *testing.T) {
	report := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, service.StatusActive, service.StatusDisabled, "success", "manual review", "", "batch-123", "", "")
	if report.AuditKey == "" || report.AuditKey == "batch-123:42" {
		t.Fatalf("audit key = %q", report.AuditKey)
	}
}

func TestBuildRiskStatusAuditReportUsesDistinctRetryAttemptKey(t *testing.T) {
	report := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, service.StatusDisabled, service.StatusDisabled, "success", "manual retry", "", "batch-123", "request-456", "retry-789")
	partial := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, service.StatusActive, service.StatusDisabled, "partial", "manual retry", "failed", "batch-123", "request-456", "")
	if report.AuditKey == partial.AuditKey || report.Metadata["request_id"] != "request-456" || report.Metadata["audit_attempt_id"] != "retry-789" {
		t.Fatalf("retry audit = %#v", report)
	}
}

func TestBuildRiskStatusAuditReportSeparatesActorsAndActions(t *testing.T) {
	ban := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, service.StatusActive, service.StatusDisabled, "success", "manual", "", "", "request-456", "")
	unban := buildRiskStatusAuditReport(7, 42, service.StatusActive, service.StatusDisabled, service.StatusActive, "success", "manual", "", "", "request-456", "")
	otherActor := buildRiskStatusAuditReport(8, 42, service.StatusDisabled, service.StatusActive, service.StatusDisabled, "success", "manual", "", "", "request-456", "")
	if ban.AuditKey == unban.AuditKey || ban.AuditKey == otherActor.AuditKey {
		t.Fatalf("audit keys must isolate action and actor: %q %q %q", ban.AuditKey, unban.AuditKey, otherActor.AuditKey)
	}
}

func TestRiskSessionRevocationResultPreservesRetryContext(t *testing.T) {
	user := &service.User{ID: 42, Email: "alice@example.com", Status: service.StatusDisabled}
	req := RiskSessionRevocationRequest{Reason: "manual review", BatchID: "batch-123", RequestID: "request-456"}
	result := riskSessionRevocationResult(user, req, "partial", true, "session_revocation", "cache unavailable")
	if result["result"] != "partial" || result["retryable"] != true || result["request_id"] != "request-456" || result["batch_id"] != "batch-123" {
		t.Fatalf("retry result = %#v", result)
	}
	if result["pending_step"] != "session_revocation" || result["failure_reason"] != "cache unavailable" || result["after_status"] != service.StatusDisabled {
		t.Fatalf("retry result context = %#v", result)
	}
}

func TestBuildRiskStatusAuditReportPreservesPartialOutcome(t *testing.T) {
	report := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, service.StatusActive, service.StatusDisabled, "partial", "manual review", "token revocation failed", "batch-123", "", "")
	if report.Result != "partial" || report.Metadata["after_status"] != service.StatusDisabled {
		t.Fatalf("partial report = %#v", report)
	}
	if report.Metadata["failure_reason"] != "token revocation failed" {
		t.Fatalf("failure reason = %#v", report.Metadata["failure_reason"])
	}
}

func TestRetryRiskSessionRevocationRequiresIdempotencyKey(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	engine := gin.New()
	handler := &CustomUserHandler{}
	engine.POST("/admin/users/:id/risk-status/revoke-sessions", handler.RetryRiskSessionRevocation)

	request := httptest.NewRequest(http.MethodPost, "/admin/users/42/risk-status/revoke-sessions", bytes.NewBufferString(`{"reason":"manual review","request_id":"request-456"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRetryRiskSessionRevocationKeepsUnavailableServiceRetryable(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	engine := gin.New()
	handler := &CustomUserHandler{}
	engine.POST("/admin/users/:id/risk-status/revoke-sessions", handler.RetryRiskSessionRevocation)

	request := httptest.NewRequest(http.MethodPost, "/admin/users/42/risk-status/revoke-sessions", bytes.NewBufferString(`{"reason":"manual review","request_id":"request-456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "retry-456")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data["result"] != "partial" || envelope.Data["retryable"] != true || envelope.Data["pending_step"] != "session_revocation" || envelope.Data["request_id"] != "request-456" {
		t.Fatalf("data = %#v", envelope.Data)
	}
}
