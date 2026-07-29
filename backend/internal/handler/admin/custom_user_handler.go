package admin

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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

func buildRiskStatusAuditReport(actorID, userID int64, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID string) service.RiskAuditReport {
	action := "unban"
	if requestedStatus == service.StatusDisabled {
		action = "ban"
	}
	metadata := map[string]any{"before_status": beforeStatus, "after_status": afterStatus}
	if failureReason = strings.TrimSpace(failureReason); failureReason != "" {
		metadata["failure_reason"] = failureReason
	}
	if batchID = strings.TrimSpace(batchID); batchID != "" {
		metadata["batch_id"] = batchID
	}
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		metadata["request_id"] = requestID
	}
	auditKey := ""
	if batchID != "" && userID > 0 {
		auditKey = batchID + ":" + strconv.FormatInt(userID, 10)
	}
	return service.RiskAuditReport{AuditKey: auditKey, ActorID: actorID, Action: action, TargetType: "user", TargetID: strconv.FormatInt(userID, 10), Result: result, Reason: strings.TrimSpace(reason), Metadata: metadata}
}

func (h *CustomUserHandler) reportRiskStatusAudit(actorID, userID int64, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID string) {
	if h == nil || h.riskControlClient == nil || userID <= 0 {
		return
	}
	report := buildRiskStatusAuditReport(actorID, userID, requestedStatus, beforeStatus, afterStatus, result, reason, failureReason, batchID, requestID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if err := h.riskControlClient.ReportAudit(ctx, report); err != nil {
			slog.Warn("risk control audit report failed", "error", err, "user_id", userID)
		}
	}()
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
	if err := validateRiskStatusReason(req.Reason); err != nil {
		h.reportRiskStatusAudit(getAdminIDFromContext(c), userID, req.Status, "", "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID)
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	before, err := h.adminService.GetUser(c.Request.Context(), userID)
	if err != nil {
		h.reportRiskStatusAudit(getAdminIDFromContext(c), userID, req.Status, "", "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID)
		response.ErrorFrom(c, err)
		return
	}
	updated, err := h.adminService.UpdateUser(c.Request.Context(), userID, &service.UpdateUserInput{Status: req.Status, ActorAdminID: getAdminIDFromContext(c)})
	if err != nil {
		h.reportRiskStatusAudit(getAdminIDFromContext(c), userID, req.Status, before.Status, "", "failed", req.Reason, err.Error(), req.BatchID, req.RequestID)
		response.ErrorFrom(c, err)
		return
	}
	if shouldRevokeTokensForStatusChange(before.Status, req.Status) && h.authService != nil {
		if err := h.authService.RevokeAllUserTokens(c.Request.Context(), userID); err != nil {
			h.reportRiskStatusAudit(getAdminIDFromContext(c), userID, req.Status, before.Status, updated.Status, "failed", req.Reason, err.Error(), req.BatchID, req.RequestID)
			response.ErrorFrom(c, err)
			return
		}
	}
	h.reportRiskStatusAudit(getAdminIDFromContext(c), userID, req.Status, before.Status, updated.Status, "success", req.Reason, "", req.BatchID, req.RequestID)
	response.Success(c, gin.H{"user": dto.UserFromServiceAdmin(updated), "before_status": before.Status, "after_status": updated.Status, "reason": req.Reason})
}

func shouldRevokeTokensForStatusChange(beforeStatus, requestedStatus string) bool {
	return beforeStatus != service.StatusDisabled && requestedStatus == service.StatusDisabled
}
