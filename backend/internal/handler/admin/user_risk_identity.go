package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var riskIdentityUserSections = map[string]struct{}{"identity-summary": {}, "ip-identities": {}, "device-identities": {}, "associated-users": {}}

func (h *CustomUserHandler) ProxyUserRiskIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	section := strings.TrimSpace(c.Param("section"))
	if section == "" && c.Request != nil && c.Request.URL != nil {
		parts := strings.Split(strings.Trim(c.Request.URL.Path, "/"), "/")
		if len(parts) > 0 {
			section = parts[len(parts)-1]
		}
	}
	if _, ok := riskIdentityUserSections[section]; !ok {
		response.NotFound(c, "Risk identity endpoint not found")
		return
	}
	path := fmt.Sprintf("/api/v1/admin/users/%d/%s%s", userID, section, querySuffix(c))
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, path, nil)
	if err != nil {
		h.recordIdentityDetailAudit(c, userID, section, http.StatusServiceUnavailable)
		return
	}
	if section == "associated-users" && status >= 200 && status < 300 {
		body = h.enrichAssociatedUsers(c.Request.Context(), body)
	}
	h.recordIdentityDetailAudit(c, userID, section, status)
	c.Data(status, "application/json", body)
}

func (h *CustomUserHandler) ProxyRiskIdentityHealth(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/identity-health", nil)
	if err != nil {
		return
	}
	if status >= 200 && status < 300 {
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			payload["ingest_queue"] = h.riskControlClient.IdentityQueueHealth()
			if encoded, marshalErr := json.Marshal(payload); marshalErr == nil {
				body = encoded
			}
		}
	}
	c.Data(status, "application/json", body)
}
func (h *CustomUserHandler) ProxyRiskIdentitySummaries(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/identity-summaries"+querySuffix(c), nil)
	if err != nil {
		return
	}
	c.Data(status, "application/json", body)
}
func (h *CustomUserHandler) ProxyRiskIdentityRebuildDryRun(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	h.proxyRiskIdentityCommand(c, "/api/v1/admin/risk-rebuilds/dry-run")
}
func (h *CustomUserHandler) ProxyRiskIdentityRebuild(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	h.proxyRiskIdentityCommand(c, "/api/v1/admin/risk-rebuilds")
}
func (h *CustomUserHandler) ProxyRiskIdentityRebuildStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid rebuild ID")
		return
	}
	body, status, proxyErr := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/risk-rebuilds/"+strconv.FormatInt(id, 10), nil)
	if proxyErr != nil {
		return
	}
	c.Data(status, "application/json", body)
}
func (h *CustomUserHandler) proxyRiskIdentityCommand(c *gin.Context, path string) {
	body, status, err := h.proxyRiskIdentity(c, http.MethodPost, path, []byte("{}"))
	if err != nil {
		return
	}
	c.Data(status, "application/json", body)
}

func (h *CustomUserHandler) proxyRiskIdentity(c *gin.Context, method, path string, body []byte) ([]byte, int, error) {
	if h == nil || h.riskControlClient == nil {
		response.Error(c, http.StatusServiceUnavailable, "Risk identity service is not configured")
		return nil, 0, fmt.Errorf("risk identity client unavailable")
	}
	upstream, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), method, path, getAdminIDFromContext(c), body)
	if err != nil && len(upstream) == 0 {
		response.Error(c, http.StatusServiceUnavailable, "Risk identity service is unavailable")
		return nil, 0, err
	}
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	return upstream, status, nil
}

func (h *CustomUserHandler) enrichAssociatedUsers(ctx context.Context, body []byte) []byte {
	if h == nil || h.adminService == nil {
		return body
	}
	var payload struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	users := make(map[int64]map[string]any)
	ids := make([]int64, 0, len(payload.Items))
	for _, item := range payload.Items {
		raw, ok := item["user_id"].(float64)
		if !ok {
			continue
		}
		id := int64(raw)
		if _, seen := users[id]; seen {
			continue
		}
		users[id] = nil
		ids = append(ids, id)
	}
	if batch, ok := h.adminService.(service.RiskIdentityUserBatchReader); ok {
		if accounts, err := batch.GetUsersForRiskIdentity(ctx, ids); err == nil {
			for index := range accounts {
				user := &accounts[index]
				users[user.ID] = identityAccountPayload(user)
			}
			attachIdentityAccounts(payload.Items, users, true)
			if encoded, marshalErr := json.Marshal(payload); marshalErr == nil {
				return encoded
			}
		}
	}
	attachIdentityAccounts(payload.Items, users, false)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func identityAccountPayload(user *service.User) map[string]any {
	availability := "available"
	if user.DeletedAt != nil {
		availability = "deleted"
	}
	return map[string]any{"id": user.ID, "email": user.Email, "username": user.Username, "status": user.Status, "availability": availability, "deleted": user.DeletedAt != nil, "created_at": user.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func attachIdentityAccounts(items []map[string]any, users map[int64]map[string]any, lookupAvailable bool) {
	for _, item := range items {
		if raw, ok := item["user_id"].(float64); ok {
			account := users[int64(raw)]
			if account == nil {
				availability, reason := "unavailable", "account_lookup_failed"
				if lookupAvailable {
					availability, reason = "deleted", "account_record_not_found"
				}
				account = map[string]any{"id": int64(raw), "email": "", "username": "", "status": "", "availability": availability, "unavailable_reason": reason, "deleted": availability == "deleted", "created_at": ""}
			}
			item["account"] = account
		}
	}
}

func (h *CustomUserHandler) recordIdentityDetailAudit(c *gin.Context, userID int64, section string, status int) {
	if h == nil || h.riskControlClient == nil {
		return
	}
	result := "success"
	if status < 200 || status >= 300 {
		result = "failed"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 300*time.Millisecond)
	defer cancel()
	_ = h.riskControlClient.ReportAudit(ctx, service.RiskAuditReport{AuditKey: "identity-view:" + uuid.NewString(), ActorID: getAdminIDFromContext(c), Action: "view_identity_detail", TargetType: "user", TargetID: strconv.FormatInt(userID, 10), Result: result, Metadata: map[string]any{"section": section}})
}
