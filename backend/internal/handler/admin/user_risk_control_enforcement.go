package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ApplyRiskBan is the only gateway-to-account enforcement callback. The main
// service owns the user row and token revocation; the risk service never edits
// the main database directly.
func (h *UserHandler) ApplyRiskBan(ctx context.Context, userID int64, reason string) error {
	if h == nil || h.adminService == nil || userID <= 0 {
		return errors.New("risk ban handler is not configured")
	}
	user, err := h.adminService.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.Status != service.StatusDisabled {
		if _, err := h.adminService.UpdateUser(ctx, userID, &service.UpdateUserInput{Status: service.StatusDisabled}); err != nil {
			return err
		}
	}
	if h.authService != nil {
		if err := h.authService.RevokeAllUserTokens(ctx, userID); err != nil {
			return err
		}
	}
	if h.riskControlClient != nil {
		auditCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return h.riskControlClient.ReportAudit(auditCtx, service.RiskAuditReport{
			Action: "auto_ban", TargetType: "user", TargetID: strconv.FormatInt(userID, 10),
			Result: "success", Reason: reason, Metadata: map[string]any{"source": "risk_rule"},
		})
	}
	return nil
}

func shouldRevokeTokensForRiskBan(user *service.User) bool {
	return user != nil
}
