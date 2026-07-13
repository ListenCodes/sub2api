package admin

import (
	"io"
	"net/http"
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

func allowedRiskControlPath(method, path string) bool {
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
