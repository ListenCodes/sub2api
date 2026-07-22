package admin

import (
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const maxRiskControlProxyBody = 256 * 1024

// ProxyRiskControl forwards only the allowlisted risk-control admin API after
// the normal main-site admin middleware has authenticated the request.
func (h *UserHandler) ProxyRiskControl(c *gin.Context) {
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
	upstreamBody, status, err := h.riskControlClient.ProxyAdmin(c.Request.Context(), c.Request.Method, "/api/v1/admin"+path+querySuffix(c), getAdminIDFromContext(c), body)
	if err != nil && len(upstreamBody) == 0 {
		response.Error(c, http.StatusServiceUnavailable, "Risk control service is unavailable")
		return
	}
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	c.Data(status, "application/json", upstreamBody)
}

// ProxyAccountMonitor forwards only the account-monitor admin API after the
// main-site admin middleware has authenticated the request.
func (h *UserHandler) ProxyAccountMonitor(c *gin.Context) {
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
	case method == http.MethodGet && (path == "/overview" || path == "/users" || strings.HasPrefix(path, "/users/") || path == "/rules" || path == "/audit"):
		return true
	case method == http.MethodPut && strings.HasPrefix(path, "/rules/"):
		return true
	case method == http.MethodPost && (path == "/rules" || path == "/rules/test" || isProcessedUserPath(path)):
		return true
	default:
		return false
	}
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
