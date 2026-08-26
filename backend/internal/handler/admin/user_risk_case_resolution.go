package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ResolveRiskCaseRequest struct {
	UserID           int64  `json:"user_id" binding:"required"`
	Resolution       string `json:"resolution" binding:"required"`
	Reason           string `json:"reason" binding:"required,max=500"`
	AccountAction    string `json:"account_action" binding:"required,oneof=none disable restore"`
	ExpectedRevision int    `json:"expected_case_revision"`
}

type riskCasePreflight struct {
	ID                  int64  `json:"id"`
	UserID              int64  `json:"user_id"`
	Status              string `json:"status"`
	Resolution          string `json:"resolution"`
	ResolutionReason    string `json:"resolution_reason"`
	ResolutionRequestID string `json:"resolution_request_id"`
	Revision            int    `json:"revision"`
}

type riskCaseResolveResponse struct {
	Case             riskCasePreflight `json:"case"`
	Resolved         bool              `json:"resolved"`
	IdempotentReplay bool              `json:"idempotent_replay"`
}

type resolveRiskCaseRetryableError struct{ data gin.H }

func (e *resolveRiskCaseRetryableError) Error() string {
	return "risk case resolution is partially complete"
}

func validRiskCaseResolution(value string) bool {
	switch value {
	case "confirmed_abuse", "legitimate_shared", "insufficient_evidence", "data_error", "business_violation":
		return true
	default:
		return false
	}
}

func (h *CustomUserHandler) ResolveRiskCase(c *gin.Context) {
	caseID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || caseID <= 0 {
		response.BadRequest(c, "Invalid review case ID")
		return
	}
	requestID, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil || requestID == "" {
		response.BadRequest(c, "A valid Idempotency-Key is required")
		return
	}
	var req ResolveRiskCaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid resolve request")
		return
	}
	req.Resolution = strings.TrimSpace(req.Resolution)
	req.Reason = strings.TrimSpace(req.Reason)
	req.AccountAction = strings.TrimSpace(req.AccountAction)
	if req.UserID <= 0 || req.ExpectedRevision <= 0 || !validRiskCaseResolution(req.Resolution) || validateRiskStatusReason(req.Reason) != nil {
		response.BadRequest(c, "Invalid resolve request")
		return
	}
	payload := struct {
		CaseID  int64                  `json:"case_id"`
		Request ResolveRiskCaseRequest `json:"request"`
	}{CaseID: caseID, Request: req}
	result, err := executeAdminIdempotent(c, "admin.user_risk_case.resolve", payload, service.DefaultWriteIdempotencyTTL(), func(context.Context) (any, error) {
		data, retryable := h.executeRiskCaseResolution(c, caseID, requestID, req)
		if retryable {
			return nil, &resolveRiskCaseRetryableError{data: data}
		}
		return data, nil
	})
	if err != nil {
		var partial *resolveRiskCaseRetryableError
		if errors.As(err, &partial) {
			response.Success(c, partial.data)
			return
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}

func (h *CustomUserHandler) executeRiskCaseResolution(c *gin.Context, caseID int64, requestID string, req ResolveRiskCaseRequest) (gin.H, bool) {
	actorID := getAdminIDFromContext(c)
	caseAuditAttemptID := "risk-case-resolution-attempt-" + uuid.NewString()
	reportCaseAttempt := func(result, failure string) {
		_ = h.reportRiskAudit(c.Request.Context(), service.RiskAuditReport{
			AuditKey: "risk-case-resolution:" + caseAuditAttemptID, ActorID: actorID, Action: "resolve_risk_review_case", TargetType: "risk_review_case", TargetID: strconv.FormatInt(caseID, 10), Result: result, Reason: req.Reason,
			Metadata: map[string]any{"user_id": req.UserID, "resolution": req.Resolution, "request_id": requestID, "audit_attempt_id": caseAuditAttemptID, "failure_reason": failure, "outcome_unknown": result == "partial"},
		})
	}
	if h == nil || h.riskControlClient == nil {
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case service is unavailable", true), true
	}
	caseBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), http.MethodGet, fmt.Sprintf("/api/v1/admin/review-cases/%d", caseID), actorID, nil)
	if err != nil || status < 200 || status >= 300 {
		reportCaseAttempt("failed", "Review case preflight failed")
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case preflight failed", true), true
	}
	var current riskCasePreflight
	if err := json.Unmarshal(caseBody, &current); err != nil || current.ID != caseID || current.UserID != req.UserID {
		reportCaseAttempt("failed", "Review case does not match the selected account")
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case does not match the selected account", false), false
	}
	if current.Status == "resolved" {
		if current.ResolutionRequestID != requestID || current.Resolution != req.Resolution || strings.TrimSpace(current.ResolutionReason) != req.Reason {
			reportCaseAttempt("failed", "Review case is already resolved by a different request")
			return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case is already resolved by a different request", false), false
		}
	} else if current.Status != "in_review" || current.Revision != req.ExpectedRevision {
		reportCaseAttempt("failed", "Review case changed; reload before completing review")
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case changed; reload before completing review", false), false
	}
	extensionRequest := gin.H{
		"user_id": req.UserID, "resolution": req.Resolution, "reason": strings.TrimSpace(req.Reason),
		"request_id": requestID, "expected_revision": req.ExpectedRevision,
	}
	body, _ := json.Marshal(extensionRequest)
	resolvedBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), http.MethodPost, fmt.Sprintf("/api/v1/admin/review-cases/%d/resolve", caseID), actorID, body)
	if err != nil || status < 200 || status >= 300 {
		result := "failed"
		caseResult := "failed"
		failure := "Review case could not be resolved"
		if err != nil || status == 0 || status >= 500 {
			result = "partial"
			caseResult = "partial"
			failure = "Review case outcome is unknown; retry with the same request ID"
		}
		reportCaseAttempt(result, failure)
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", caseResult, failure, true), true
	}
	var resolved riskCaseResolveResponse
	expectedResolvedRevision := current.Revision
	if current.Status != "resolved" {
		expectedResolvedRevision++
	}
	if err := json.Unmarshal(resolvedBody, &resolved); err != nil ||
		!resolved.Resolved || resolved.Case.ID != caseID || resolved.Case.UserID != req.UserID ||
		resolved.Case.Status != "resolved" || resolved.Case.Resolution != req.Resolution ||
		strings.TrimSpace(resolved.Case.ResolutionReason) != req.Reason || resolved.Case.ResolutionRequestID != requestID ||
		resolved.Case.Revision != expectedResolvedRevision {
		failure := "Review case response could not be verified; retry with the same request ID"
		reportCaseAttempt("partial", failure)
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "partial", failure, true), true
	}
	caseStep := gin.H{
		"id": caseID, "user_id": req.UserID, "status": resolved.Case.Status, "result": "resolved",
		"resolution": resolved.Case.Resolution, "revision": resolved.Case.Revision,
		"idempotent_replay": resolved.IdempotentReplay,
	}
	accountStep, ok, retryable := h.applyRiskCaseAccountAction(c, req, requestID)
	if !ok {
		return gin.H{"result": "partial", "request_id": requestID, "retryable": retryable, "account": accountStep, "case": caseStep}, retryable
	}
	return gin.H{"result": "success", "request_id": requestID, "retryable": false, "account": accountStep, "case": caseStep}, false
}

func (h *CustomUserHandler) applyRiskCaseAccountAction(c *gin.Context, req ResolveRiskCaseRequest, requestID string) (gin.H, bool, bool) {
	if req.AccountAction == "none" {
		return gin.H{"user_id": req.UserID, "action": "none", "result": "skipped"}, true, false
	}
	auditAttemptID := "risk-case-account-attempt-" + uuid.NewString()
	requestedStatus := service.StatusDisabled
	if req.AccountAction == "restore" {
		requestedStatus = service.StatusActive
	}
	if h == nil || h.adminService == nil {
		failureReason := "Admin user service is unavailable"
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, "", "", "failed", req.Reason, failureReason, "", requestID, auditAttemptID)
		return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "failed", "retryable": true, "pending_step": "status_confirmation", "failure_reason": failureReason}, false, true
	}
	before, err := h.adminService.GetUser(c.Request.Context(), req.UserID)
	if err != nil {
		retryable := riskCaseAccountActionRetryable(err)
		result := "failed"
		if !retryable {
			result = "not_executed"
		}
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, "", "", "failed", req.Reason, err.Error(), "", requestID, auditAttemptID)
		return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": result, "retryable": retryable, "pending_step": "status_confirmation", "failure_reason": err.Error()}, false, retryable
	}
	afterStatus := before.Status
	if before.Status != requestedStatus {
		updated, err := h.adminService.UpdateUser(c.Request.Context(), req.UserID, &service.UpdateUserInput{Status: requestedStatus, ActorAdminID: getAdminIDFromContext(c)})
		if err != nil {
			retryable := riskCaseAccountActionRetryable(err)
			result := "failed"
			if !retryable {
				result = "not_executed"
			}
			_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, before.Status, "failed", req.Reason, err.Error(), "", requestID, auditAttemptID)
			return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": result, "retryable": retryable, "pending_step": "status_confirmation", "before_status": before.Status, "failure_reason": err.Error()}, false, retryable
		}
		afterStatus = updated.Status
	}
	if req.AccountAction == "disable" && h.authService == nil {
		failureReason := "Account status changed, but the session revocation service is unavailable"
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, afterStatus, "partial", req.Reason, failureReason, "", requestID, auditAttemptID)
		return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "partial", "retryable": true, "pending_step": "session_revocation", "before_status": before.Status, "after_status": afterStatus, "failure_reason": failureReason}, false, true
	}
	if req.AccountAction == "disable" {
		if err := h.authService.RevokeAllUserSessions(c.Request.Context(), req.UserID); err != nil {
			_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, afterStatus, "partial", req.Reason, err.Error(), "", requestID, auditAttemptID)
			return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "partial", "retryable": true, "pending_step": "session_revocation", "before_status": before.Status, "after_status": afterStatus, "failure_reason": "Account status changed, but active sessions could not be revoked"}, false, true
		}
	}
	if err := h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, afterStatus, "success", req.Reason, "", "", requestID, auditAttemptID); err != nil {
		return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "partial", "retryable": true, "pending_step": "audit_reporting", "before_status": before.Status, "after_status": afterStatus, "failure_reason": "Account action completed, but its audit record could not be confirmed"}, false, true
	}
	return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "success", "before_status": before.Status, "after_status": afterStatus}, true, false
}

func riskCaseAccountActionRetryable(err error) bool {
	code := infraerrors.Code(err)
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

func resolveRiskCasePartial(caseID, userID int64, requestID, accountResult, caseResult, failure string, retryable bool) gin.H {
	return gin.H{"result": "partial", "request_id": requestID, "retryable": retryable, "account": gin.H{"user_id": userID, "result": accountResult}, "case": gin.H{"id": caseID, "result": caseResult, "failure_reason": failure}}
}
