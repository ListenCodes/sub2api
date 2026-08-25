package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
	requestID := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if requestID == "" || len(requestID) > 160 {
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
	caseBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), http.MethodGet, fmt.Sprintf("/api/v1/admin/review-cases/%d", caseID), actorID, nil)
	if err != nil || status < 200 || status >= 300 {
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case preflight failed", true), true
	}
	var current riskCasePreflight
	if err := json.Unmarshal(caseBody, &current); err != nil || current.ID != caseID || current.UserID != req.UserID {
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case does not match the selected account", false), false
	}
	if current.Status == "resolved" {
		if current.ResolutionRequestID != requestID || current.Resolution != req.Resolution || strings.TrimSpace(current.ResolutionReason) != req.Reason {
			return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case is already resolved by a different request", false), false
		}
	} else if current.Status != "in_review" || current.Revision != req.ExpectedRevision {
		return resolveRiskCasePartial(caseID, req.UserID, requestID, "not_executed", "failed", "Review case changed; reload before completing review", false), false
	}
	accountStep, ok := h.applyRiskCaseAccountAction(c, req, requestID)
	if !ok {
		return gin.H{"result": "partial", "request_id": requestID, "retryable": true, "account": accountStep, "case": gin.H{"id": caseID, "result": "not_executed"}}, true
	}
	extensionRequest := gin.H{
		"user_id": req.UserID, "resolution": req.Resolution, "reason": strings.TrimSpace(req.Reason),
		"request_id": requestID, "expected_revision": req.ExpectedRevision,
	}
	body, _ := json.Marshal(extensionRequest)
	resolvedBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), http.MethodPost, fmt.Sprintf("/api/v1/admin/review-cases/%d/resolve", caseID), actorID, body)
	if err != nil || status < 200 || status >= 300 {
		return gin.H{"result": "partial", "request_id": requestID, "retryable": true, "account": accountStep, "case": gin.H{"id": caseID, "result": "failed", "failure_reason": "Account action succeeded, but the review case was not resolved"}}, true
	}
	var caseStep map[string]any
	if err := json.Unmarshal(resolvedBody, &caseStep); err != nil {
		caseStep = map[string]any{"id": caseID, "result": "resolved"}
	}
	return gin.H{"result": "success", "request_id": requestID, "retryable": false, "account": accountStep, "case": caseStep}, false
}

func (h *CustomUserHandler) applyRiskCaseAccountAction(c *gin.Context, req ResolveRiskCaseRequest, requestID string) (gin.H, bool) {
	if req.AccountAction == "none" {
		return gin.H{"user_id": req.UserID, "action": "none", "result": "skipped"}, true
	}
	requestedStatus := service.StatusDisabled
	if req.AccountAction == "restore" {
		requestedStatus = service.StatusActive
	}
	before, err := h.adminService.GetUser(c.Request.Context(), req.UserID)
	if err != nil {
		return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "failed", "failure_reason": err.Error()}, false
	}
	afterStatus := before.Status
	if before.Status != requestedStatus {
		updated, err := h.adminService.UpdateUser(c.Request.Context(), req.UserID, &service.UpdateUserInput{Status: requestedStatus, ActorAdminID: getAdminIDFromContext(c)})
		if err != nil {
			return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "failed", "before_status": before.Status, "failure_reason": err.Error()}, false
		}
		afterStatus = updated.Status
	}
	if req.AccountAction == "disable" && h.authService != nil {
		if err := h.authService.RevokeAllUserSessions(c.Request.Context(), req.UserID); err != nil {
			h.reportRiskStatusAudit(getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, afterStatus, "partial", req.Reason, err.Error(), "", requestID)
			return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "partial", "before_status": before.Status, "after_status": afterStatus, "failure_reason": "Account status changed, but active sessions could not be revoked"}, false
		}
	}
	h.reportRiskStatusAudit(getAdminIDFromContext(c), req.UserID, requestedStatus, before.Status, afterStatus, "success", req.Reason, "", "", requestID)
	return gin.H{"user_id": req.UserID, "action": req.AccountAction, "result": "success", "before_status": before.Status, "after_status": afterStatus}, true
}

func resolveRiskCasePartial(caseID, userID int64, requestID, accountResult, caseResult, failure string, retryable bool) gin.H {
	return gin.H{"result": "partial", "request_id": requestID, "retryable": retryable, "account": gin.H{"user_id": userID, "result": accountResult}, "case": gin.H{"id": caseID, "result": caseResult, "failure_reason": failure}}
}
