package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CustomUserHandler struct {
	adminService      service.AdminService
	authService       *service.AuthService
	riskControlClient *service.RiskControlClient
}

func NewCustomUserHandler(base *UserHandler, authService *service.AuthService, client *service.RiskControlClient) *CustomUserHandler {
	var adminService service.AdminService
	if base != nil {
		adminService = base.adminService
	}
	return &CustomUserHandler{
		adminService:      adminService,
		authService:       authService,
		riskControlClient: client,
	}
}

type RiskStatusRequest struct {
	Status    string `json:"status" binding:"required,oneof=active disabled"`
	Reason    string `json:"reason" binding:"required,max=500"`
	BatchID   string `json:"batch_id,omitempty" binding:"max=160"`
	RequestID string `json:"request_id,omitempty" binding:"max=160"`
}

type RiskSessionRevocationRequest struct {
	Reason    string `json:"reason" binding:"required,max=500"`
	BatchID   string `json:"batch_id,omitempty" binding:"max=160"`
	RequestID string `json:"request_id" binding:"required,max=160"`
}

type riskSessionRevocationRetryableError struct{ data gin.H }

func (e *riskSessionRevocationRetryableError) Error() string {
	return "risk session revocation is partially complete"
}

func validateRiskStatusReason(reason string) error {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return errors.New("risk status reason is required")
	}
	if len([]rune(trimmed)) > 500 {
		return errors.New("risk status reason is too long")
	}
	return nil
}

func buildRiskStatusAuditReport(actorID, userID int64, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID, auditAttemptID string) service.RiskAuditReport {
	action := "unban"
	if requestedStatus == service.StatusDisabled {
		action = "ban"
	}
	metadata := map[string]any{"before_status": beforeStatus, "after_status": afterStatus, "failure_reason": nil}
	if failureReason = strings.TrimSpace(failureReason); failureReason != "" {
		metadata["failure_reason"] = failureReason
	}
	if batchID = strings.TrimSpace(batchID); batchID != "" {
		metadata["batch_id"] = batchID
	}
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		metadata["request_id"] = requestID
	}
	if auditAttemptID = strings.TrimSpace(auditAttemptID); auditAttemptID != "" {
		metadata["audit_attempt_id"] = auditAttemptID
	}
	logicalID := strings.TrimSpace(auditAttemptID)
	if logicalID == "" {
		logicalID = strings.TrimSpace(requestID)
	}
	if logicalID == "" {
		logicalID = strings.TrimSpace(batchID)
	}
	auditKey := ""
	if logicalID != "" && userID > 0 {
		digest := sha256.Sum256([]byte(strings.Join([]string{logicalID, strconv.FormatInt(actorID, 10), strconv.FormatInt(userID, 10), action}, "\x00")))
		auditKey = "risk-status:" + hex.EncodeToString(digest[:])
	}
	return service.RiskAuditReport{AuditKey: auditKey, ActorID: actorID, Action: action, TargetType: "user", TargetID: strconv.FormatInt(userID, 10), Result: result, Reason: strings.TrimSpace(reason), Metadata: metadata}
}

func (h *CustomUserHandler) reportRiskStatusAudit(ctx context.Context, actorID, userID int64, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID, auditAttemptID string) error {
	report := buildRiskStatusAuditReport(actorID, userID, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID, auditAttemptID)
	return h.reportRiskAudit(ctx, report)
}

func (h *CustomUserHandler) reportRiskAudit(ctx context.Context, report service.RiskAuditReport) error {
	if h == nil || h.riskControlClient == nil || report.TargetID == "" {
		return errors.New("risk control audit service is unavailable")
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := h.riskControlClient.ReportAudit(auditCtx, report); err != nil {
		slog.Warn("risk control audit report failed", "error", err, "target_type", report.TargetType, "target_id", report.TargetID)
		return err
	}
	return nil
}

func (h *CustomUserHandler) SetRiskStatus(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	var req RiskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	auditAttemptID := "risk-status-attempt-" + uuid.NewString()
	if err := validateRiskStatusReason(req.Reason); err != nil {
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, "", "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID, auditAttemptID)
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		req.RequestID = "risk-status-" + uuid.NewString()
	}
	if h == nil || h.adminService == nil {
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, "", "", "failed", req.Reason, "Admin user service is unavailable", req.BatchID, req.RequestID, auditAttemptID)
		response.Error(c, http.StatusServiceUnavailable, "Admin user service is unavailable")
		return
	}
	before, err := h.adminService.GetUser(c.Request.Context(), userID)
	if err != nil {
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, "", "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID, auditAttemptID)
		response.ErrorFrom(c, err)
		return
	}
	updated, err := h.adminService.UpdateUser(c.Request.Context(), userID, &service.UpdateUserInput{Status: req.Status, ActorAdminID: getAdminIDFromContext(c)})
	if err != nil {
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, before.Status, "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID, auditAttemptID)
		response.ErrorFrom(c, err)
		return
	}
	if req.Status == service.StatusDisabled && h.authService == nil {
		failureReason := "Account status changed, but the session revocation service is unavailable"
		_ = h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, before.Status, updated.Status, "partial", req.Reason, failureReason, req.BatchID, req.RequestID, auditAttemptID)
		response.Success(c, gin.H{"user": dto.UserFromServiceAdmin(updated), "before_status": before.Status, "after_status": updated.Status, "reason": req.Reason, "result": "partial", "request_id": req.RequestID, "batch_id": req.BatchID, "retryable": true, "pending_step": "session_revocation", "failure_reason": failureReason})
		return
	}
	if req.Status == service.StatusDisabled {
		if err := h.authService.RevokeAllUserSessions(c.Request.Context(), userID); err != nil {
			auditErr := h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, before.Status, updated.Status, "partial", req.Reason, err.Error(), req.BatchID, req.RequestID, auditAttemptID)
			failureReason := "Account status changed, but active sessions could not be revoked"
			if auditErr != nil {
				failureReason += "; the audit record could not be confirmed"
			}
			response.Success(c, gin.H{
				"user":           dto.UserFromServiceAdmin(updated),
				"before_status":  before.Status,
				"after_status":   updated.Status,
				"reason":         req.Reason,
				"result":         "partial",
				"request_id":     req.RequestID,
				"batch_id":       req.BatchID,
				"retryable":      true,
				"pending_step":   "session_revocation",
				"failure_reason": failureReason,
			})
			return
		}
	}
	if err := h.reportRiskStatusAudit(c.Request.Context(), getAdminIDFromContext(c), userID, req.Status, before.Status, updated.Status, "success", req.Reason, "", req.BatchID, req.RequestID, auditAttemptID); err != nil {
		response.Success(c, gin.H{"user": dto.UserFromServiceAdmin(updated), "before_status": before.Status, "after_status": updated.Status, "reason": req.Reason, "result": "partial", "request_id": req.RequestID, "batch_id": req.BatchID, "retryable": true, "pending_step": "audit_reporting", "failure_reason": "Account status changed, but its audit record could not be confirmed"})
		return
	}
	response.Success(c, gin.H{"user": dto.UserFromServiceAdmin(updated), "before_status": before.Status, "after_status": updated.Status, "reason": req.Reason, "result": "success", "request_id": req.RequestID, "batch_id": req.BatchID, "retryable": false})
}

func (h *CustomUserHandler) RetryRiskSessionRevocation(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	idempotencyKey, keyErr := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if keyErr != nil || idempotencyKey == "" {
		response.BadRequest(c, "A valid Idempotency-Key is required")
		return
	}
	var req RiskSessionRevocationRequest
	if err := c.ShouldBindJSON(&req); err != nil || validateRiskStatusReason(req.Reason) != nil || strings.TrimSpace(req.RequestID) == "" {
		response.BadRequest(c, "Invalid session revocation retry request")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.BatchID = strings.TrimSpace(req.BatchID)
	actorID := getAdminIDFromContext(c)
	result, err := executeAdminIdempotent(c, "admin.user_risk_status.revoke_sessions", gin.H{"user_id": userID, "request": req}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		auditAttemptID := "risk-session-attempt-" + uuid.NewString()
		if h == nil || h.adminService == nil || h.authService == nil {
			if h != nil {
				_ = h.reportRiskStatusAudit(ctx, actorID, userID, service.StatusDisabled, "", "", "partial", req.Reason, "Session revocation service is unavailable", req.BatchID, req.RequestID, auditAttemptID)
			}
			data := riskSessionRevocationResult(nil, req, "partial", true, "session_revocation", "Session revocation service is unavailable")
			return nil, &riskSessionRevocationRetryableError{data: data}
		}
		user, loadErr := h.adminService.GetUser(ctx, userID)
		if loadErr != nil {
			_ = h.reportRiskStatusAudit(ctx, actorID, userID, service.StatusDisabled, "", "", "partial", req.Reason, loadErr.Error(), req.BatchID, req.RequestID, auditAttemptID)
			return nil, loadErr
		}
		if user.Status != service.StatusDisabled {
			_ = h.reportRiskStatusAudit(ctx, actorID, userID, service.StatusDisabled, user.Status, user.Status, "partial", req.Reason, "Account is no longer disabled; no sessions were revoked", req.BatchID, req.RequestID, auditAttemptID)
			return riskSessionRevocationResult(user, req, "partial", false, "", "Account is no longer disabled; no sessions were revoked"), nil
		}
		if revokeErr := h.authService.RevokeAllUserSessions(ctx, userID); revokeErr != nil {
			_ = h.reportRiskStatusAudit(ctx, actorID, userID, service.StatusDisabled, user.Status, user.Status, "partial", req.Reason, revokeErr.Error(), req.BatchID, req.RequestID, auditAttemptID)
			data := riskSessionRevocationResult(user, req, "partial", true, "session_revocation", "Active sessions could not be revoked")
			return nil, &riskSessionRevocationRetryableError{data: data}
		}
		if auditErr := h.reportRiskStatusAudit(ctx, actorID, userID, service.StatusDisabled, user.Status, user.Status, "success", req.Reason, "", req.BatchID, req.RequestID, auditAttemptID); auditErr != nil {
			data := riskSessionRevocationResult(user, req, "partial", true, "audit_reporting", "Sessions were revoked, but the audit record could not be confirmed")
			return nil, &riskSessionRevocationRetryableError{data: data}
		}
		return riskSessionRevocationResult(user, req, "success", false, "", ""), nil
	})
	if err != nil {
		var partial *riskSessionRevocationRetryableError
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

func riskSessionRevocationResult(user *service.User, req RiskSessionRevocationRequest, result string, retryable bool, pendingStep, failureReason string) gin.H {
	data := gin.H{"result": result, "retryable": retryable, "request_id": req.RequestID, "batch_id": req.BatchID, "pending_step": pendingStep, "failure_reason": failureReason}
	if user != nil {
		data["after_status"] = user.Status
		data["user"] = dto.UserFromServiceAdmin(user)
	}
	return data
}

func shouldRevokeTokensForStatusChange(beforeStatus, requestedStatus string) bool {
	return beforeStatus != service.StatusDisabled && requestedStatus == service.StatusDisabled
}
