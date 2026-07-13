package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRiskStatusReasonRejectsWhitespaceOnlyInput(t *testing.T) {
	if err := validateRiskStatusReason(" \t\n "); err == nil {
		t.Fatal("whitespace-only reason must be rejected")
	}
}

func TestBuildRiskStatusAuditReportKeepsFailureAndBatchContext(t *testing.T) {
	report := buildRiskStatusAuditReport(7, 42, service.StatusDisabled, "active", "disabled", "failed", " repeated login failures ", "目标账号已被其他管理员处理", "batch-123", "request-456")
	if report.ActorID != 7 || report.TargetID != "42" || report.Action != "ban" || report.Result != "failed" {
		t.Fatalf("report identity = %+v", report)
	}
	if report.Reason != "repeated login failures" {
		t.Fatalf("reason = %q", report.Reason)
	}
	if report.AuditKey != "batch-123:42" {
		t.Fatalf("audit key = %q", report.AuditKey)
	}
	if report.Metadata["failure_reason"] != "目标账号已被其他管理员处理" || report.Metadata["request_id"] != "request-456" || report.Metadata["before_status"] != "active" || report.Metadata["after_status"] != "disabled" {
		t.Fatalf("metadata = %#v", report.Metadata)
	}
}
