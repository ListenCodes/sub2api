package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestExtensionsHomepageFrameHeadersAreRouteSpecific(t *testing.T) {
	engine := gin.New()
	engine.Use(SecurityHeaders(config.CSPConfig{
		Enabled: true,
		Policy:  "default-src 'self'; frame-ancestors 'none'",
	}, nil))

	homepage := engine.Group("/api/v1/extensions-self/homepage")
	homepage.GET("/*path", ExtensionsHomepageFrameHeaders(), func(c *gin.Context) { c.Status(http.StatusOK) })
	homepage.HEAD("/*path", ExtensionsHomepageFrameHeaders(), func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/api/v1/extensions-self/homepage/*path", func(c *gin.Context) { c.Status(http.StatusMethodNotAllowed) })
	engine.GET("/api/v1/extensions-self/homepage-archive/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/api/v1/extensions-self/account-monitor/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/other", func(c *gin.Context) { c.Status(http.StatusOK) })

	tests := []struct {
		name        string
		method      string
		path        string
		frameOption string
		ancestor    string
	}{
		{name: "homepage get", method: http.MethodGet, path: "/api/v1/extensions-self/homepage/", frameOption: "SAMEORIGIN", ancestor: "frame-ancestors 'self'"},
		{name: "homepage head", method: http.MethodHead, path: "/api/v1/extensions-self/homepage/assets/site.css", frameOption: "SAMEORIGIN", ancestor: "frame-ancestors 'self'"},
		{name: "homepage post", method: http.MethodPost, path: "/api/v1/extensions-self/homepage/", frameOption: "DENY", ancestor: "frame-ancestors 'none'"},
		{name: "similar prefix", method: http.MethodGet, path: "/api/v1/extensions-self/homepage-archive/", frameOption: "DENY", ancestor: "frame-ancestors 'none'"},
		{name: "account monitor", method: http.MethodGet, path: "/api/v1/extensions-self/account-monitor/overview", frameOption: "DENY", ancestor: "frame-ancestors 'none'"},
		{name: "unrelated route", method: http.MethodGet, path: "/other", frameOption: "DENY", ancestor: "frame-ancestors 'none'"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			assert.Equal(t, test.frameOption, response.Header().Get("X-Frame-Options"))
			assert.Contains(t, response.Header().Get("Content-Security-Policy"), test.ancestor)
		})
	}
}
