package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func ExtensionsHomepageFrameHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		if policy := c.Writer.Header().Get("Content-Security-Policy"); policy != "" {
			c.Header("Content-Security-Policy", replaceDirectiveValues(policy, "frame-ancestors", "'self'"))
		}
		c.Next()
	}
}

func replaceDirectiveValues(policy, directive string, values ...string) string {
	directives := strings.Split(policy, ";")
	replacement := strings.TrimSpace(directive + " " + strings.Join(values, " "))
	replaced := false
	result := make([]string, 0, len(directives)+1)

	for _, rawDirective := range directives {
		trimmed := strings.TrimSpace(rawDirective)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == directive {
			if !replaced {
				result = append(result, replacement)
				replaced = true
			}
			continue
		}
		result = append(result, trimmed)
	}
	if !replaced {
		result = append(result, replacement)
	}
	return strings.Join(result, "; ")
}
