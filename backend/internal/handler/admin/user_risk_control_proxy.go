package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const maxRiskControlProxyBody = 256 * 1024

// ProxyRiskControl forwards only the allowlisted risk-control admin API after
// the normal main-site admin middleware has authenticated the request.
func (h *CustomUserHandler) ProxyRiskControl(c *gin.Context) {
	path := strings.TrimSuffix(c.Param("path"), "/")
	if !allowedRiskControlPath(c.Request.Method, path) {
		response.NotFound(c, "Risk control endpoint not found")
		return
	}
	if h == nil || h.riskControlClient == nil {
		response.Error(c, http.StatusServiceUnavailable, "Risk control service is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRiskControlProxyBody+1))
	if err != nil || len(body) > maxRiskControlProxyBody {
		response.BadRequest(c, "Invalid risk control request body")
		return
	}
	query := c.Request.URL.Query()
	if c.Request.Method == http.MethodGet && path == "/audit" {
		var ok bool
		query, ok, err = h.resolveAuditAccountFilters(c, query)
		if err != nil {
			response.Error(c, http.StatusServiceUnavailable, "Account lookup is unavailable")
			return
		}
		if !ok {
			page, _ := strconv.Atoi(query.Get("page"))
			limit, _ := strconv.Atoi(query.Get("limit"))
			if page < 1 {
				page = 1
			}
			if limit < 1 {
				limit = 20
			}
			c.JSON(http.StatusOK, gin.H{"items": []any{}, "total": 0, "page": page, "page_size": limit})
			return
		}
	}
	upstreamPath := "/api/v1/admin" + path
	if encoded := query.Encode(); encoded != "" {
		upstreamPath += "?" + encoded
	}
	upstreamBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), c.Request.Method, upstreamPath, getAdminIDFromContext(c), body)
	if err != nil && len(upstreamBody) == 0 {
		response.Error(c, http.StatusServiceUnavailable, "Risk control service is unavailable")
		return
	}
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	if c.Request.Method == http.MethodGet && path == "/audit" && status >= 200 && status < 300 {
		upstreamBody = h.enrichRiskAuditAccounts(c.Request.Context(), upstreamBody)
	}
	c.Data(status, "application/json", upstreamBody)
}

func (h *CustomUserHandler) resolveAuditAccountFilters(c *gin.Context, query url.Values) (url.Values, bool, error) {
	resolved := make(url.Values, len(query))
	for key, values := range query {
		resolved[key] = append([]string(nil), values...)
	}
	if actor := strings.TrimSpace(query.Get("actor")); actor != "" {
		id, found, err := h.findExactRiskAccount(c, actor, "admin")
		if err != nil || !found {
			return resolved, found, err
		}
		resolved.Del("actor")
		resolved.Set("actor_id", strconv.FormatInt(id, 10))
	}
	category := strings.TrimSpace(query.Get("category"))
	if target := strings.TrimSpace(query.Get("target")); target != "" && category != "configuration" && category != "testing" && category != "rules" {
		id, found, err := h.findExactRiskAccount(c, target, "")
		if err != nil || !found {
			return resolved, found, err
		}
		resolved.Del("target")
		resolved.Set("target_user_id", strconv.FormatInt(id, 10))
	}
	return resolved, true, nil
}

func (h *CustomUserHandler) findExactRiskAccount(c *gin.Context, value, role string) (int64, bool, error) {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
		return id, true, nil
	}
	if h == nil || h.adminService == nil {
		return 0, false, nil
	}
	users, _, err := h.adminService.ListUsers(c.Request.Context(), 1, 100, service.UserListFilters{Search: value, Role: role}, "created_at", "desc")
	if err != nil {
		return 0, false, err
	}
	for index := range users {
		user := &users[index]
		if strings.EqualFold(strings.TrimSpace(user.Email), value) || strings.EqualFold(strings.TrimSpace(user.Username), value) {
			return user.ID, true, nil
		}
	}
	return 0, false, nil
}

func (h *CustomUserHandler) enrichRiskAuditAccounts(ctx context.Context, body []byte) []byte {
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
	ids := make([]int64, 0, len(payload.Items)*2)
	seen := map[int64]struct{}{}
	for _, item := range payload.Items {
		if actor := int64FromJSON(item["actor_id"]); actor > 0 {
			if _, ok := seen[actor]; !ok {
				seen[actor] = struct{}{}
				ids = append(ids, actor)
			}
		}
		if item["target_type"] == "user" {
			if target, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(item["target_id"])), 10, 64); err == nil && target > 0 {
				if _, ok := seen[target]; !ok {
					seen[target] = struct{}{}
					ids = append(ids, target)
				}
			}
		}
	}
	accounts := make(map[int64]*service.User, len(ids))
	available := false
	if batch, ok := h.adminService.(service.RiskIdentityUserBatchReader); ok {
		if users, err := batch.GetUsersForRiskIdentity(ctx, ids); err == nil {
			available = true
			for index := range users {
				user := &users[index]
				accounts[user.ID] = user
			}
		}
	}
	for _, item := range payload.Items {
		actor := int64FromJSON(item["actor_id"])
		if actor > 0 {
			item["actor_account"] = auditAccountPayload(actor, accounts[actor], available)
		}
		if item["target_type"] == "user" {
			if target, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(item["target_id"])), 10, 64); err == nil && target > 0 {
				item["target_account"] = auditAccountPayload(target, accounts[target], available)
			}
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func int64FromJSON(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		return parsed
	}
}
func auditAccountPayload(id int64, user *service.User, lookupAvailable bool) map[string]any {
	if user != nil {
		return identityAccountPayload(user)
	}
	availability := "unavailable"
	if lookupAvailable {
		availability = "deleted"
	}
	return map[string]any{"id": id, "email": "", "username": "", "status": "", "availability": availability}
}

// ProxyAccountMonitor forwards only the account-monitor admin API after the
// main-site admin middleware has authenticated the request.
func (h *CustomUserHandler) ProxyAccountMonitor(c *gin.Context) {
	path := strings.TrimSuffix(c.Param("path"), "/")
	if !allowedAccountMonitorPath(c.Request.Method, path) {
		response.NotFound(c, "Account monitor endpoint not found")
		return
	}
	if h == nil || h.riskControlClient == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account monitor service is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRiskControlProxyBody+1))
	if err != nil || len(body) > maxRiskControlProxyBody {
		response.BadRequest(c, "Invalid account monitor request body")
		return
	}
	upstreamPath := "/api/v1/admin/account-monitor" + path + querySuffix(c)
	upstreamBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), c.Request.Method, upstreamPath, getAdminIDFromContext(c), body)
	if err != nil && len(upstreamBody) == 0 {
		response.Error(c, http.StatusServiceUnavailable, "Account monitor service is unavailable")
		return
	}
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	c.Data(status, "application/json", upstreamBody)
}

func allowedAccountMonitorPath(method, path string) bool {
	switch {
	case method == http.MethodGet && (path == "/overview" || path == "/accounts" || path == "/attempts" || path == "/data-quality" || path == "/thresholds"):
		return true
	case method == http.MethodGet && isAccountMonitorAccountPath(path):
		return true
	case method == http.MethodGet && isAccountMonitorRebuildPath(path):
		return true
	case method == http.MethodGet && isAccountMonitorGroupPath(path):
		return true
	case method == http.MethodPut && path == "/thresholds":
		return true
	case method == http.MethodPost && path == "/rebuild-jobs":
		return true
	default:
		return false
	}
}

func isAccountMonitorGroupPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "group-monitor" || parts[1] != "groups" {
		return false
	}
	if len(parts) == 2 {
		return true
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	return err == nil && id > 0
}

func isAccountMonitorAccountPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "accounts" {
		return false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return false
	}
	return len(parts) == 2 || parts[2] == "models" || parts[2] == "users" || parts[2] == "errors" || parts[2] == "trends"
}

func isAccountMonitorRebuildPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "rebuild-jobs" {
		return false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && id > 0
}

func allowedRiskControlPath(method, path string) bool {
	path, ok := normalizeRiskControlPath(path)
	if !ok {
		return false
	}
	switch {
	case method == http.MethodGet && (path == "/overview" || path == "/users" || strings.HasPrefix(path, "/users/") || path == "/rules" || path == "/identity-rules" || strings.HasPrefix(path, "/identity-rules/") || path == "/identity-rule-effects" || path == "/review-cases" || path == "/audit"):
		return true
	case method == http.MethodPut && strings.HasPrefix(path, "/rules/"):
		return true
	case method == http.MethodPost && (path == "/rules" || path == "/rules/test" || path == "/review-cases" || isProcessedUserPath(path) || isReviewCaseActionPath(path) || isNetworkLabelActionPath(path) || isIdentityRuleActionPath(path)):
		return true
	default:
		return false
	}
}

func isIdentityRuleActionPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "identity-rules" || parts[1] == "" {
		return false
	}
	switch parts[2] {
	case "draft", "simulations", "publish", "enable", "disable", "rollback":
		return true
	default:
		return false
	}
}

func isReviewCaseActionPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "review-cases" || (parts[2] != "claim" && parts[2] != "feedback" && parts[2] != "observe") {
		return false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && id > 0
}

func isNetworkLabelActionPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "network-identities" || (parts[2] != "label" && parts[2] != "label-preview" && parts[2] != "label-revoke") {
		return false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && id > 0
}

func normalizeRiskControlPath(rawPath string) (string, bool) {
	decoded := rawPath
	for range 3 {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return "", false
		}
		if next == decoded {
			break
		}
		decoded = next
	}
	if strings.Contains(decoded, "\\") || pathpkg.Clean(decoded) != decoded {
		return "", false
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == ".." {
			return "", false
		}
	}
	return decoded, true
}

func isProcessedUserPath(path string) bool {
	if !strings.HasPrefix(path, "/users/") || !strings.HasSuffix(path, "/processed") {
		return false
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(path, "/users/"), "/processed")
	id, err := strconv.ParseInt(strings.Trim(rawID, "/"), 10, 64)
	return err == nil && id > 0
}

func querySuffix(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || c.Request.URL.RawQuery == "" {
		return ""
	}
	return "?" + c.Request.URL.RawQuery
}
