package server

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var customGatewayRiskAliases = map[string]struct{}{
	"/responses":                {},
	"/responses/*subpath":       {},
	"/alpha/search":             {},
	"/models":                   {},
	"/messages/count_tokens":    {},
	"/chat/completions":         {},
	"/embeddings":               {},
	"/images/generations":       {},
	"/images/edits":             {},
	"/images/generations/async": {},
	"/images/edits/async":       {},
	"/images/tasks/:task_id":    {},
	"/videos/generations":       {},
	"/videos/edits":             {},
	"/videos/extensions":        {},
	"/antigravity/models":       {},
}

func isCustomGatewayRiskRoute(fullPath string) bool {
	if fullPath == "/v1/sub2api/billing" {
		return false
	}
	if strings.HasPrefix(fullPath, "/v1/") ||
		strings.HasPrefix(fullPath, "/v1beta/") ||
		strings.HasPrefix(fullPath, "/backend-api/codex/") ||
		strings.HasPrefix(fullPath, "/antigravity/v1/") ||
		strings.HasPrefix(fullPath, "/antigravity/v1beta/") {
		return true
	}
	_, ok := customGatewayRiskAliases[fullPath]
	return ok
}

func SetupCustomRouter(
	r *gin.Engine,
	handlers *handler.Handlers,
	jwtAuth middleware2.JWTAuthMiddleware,
	optionalJWTAuth middleware2.OptionalJWTAuthMiddleware,
	adminAuth middleware2.AdminAuthMiddleware,
	apiKeyAuth middleware2.APIKeyAuthMiddleware,
	auditLog middleware2.AuditLogMiddleware,
	stepUpAuth middleware2.StepUpAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
	redisClient *redis.Client,
) *gin.Engine {
	customHandlers := handler.NewCustomExtensions(handlers)
	if handlers != nil && handlers.Auth != nil {
		handlers.Auth.SetRiskBanHandler(customHandlers.AdminUser.ApplyRiskBan)
	}

	r.Use(handler.RiskEventMiddlewareWhen(
		customHandlers.RiskControlClient,
		func(c *gin.Context) bool {
			apiKey, ok := middleware2.GetAPIKeyFromContext(c)
			return ok && apiKey != nil && apiKey.Group != nil && isCustomGatewayRiskRoute(c.FullPath())
		},
		customHandlers.AdminUser.ApplyRiskBan,
	))
	r.Use(handler.RiskIdentityAuthLifecycleMiddleware(customHandlers.RiskControlClient))

	SetupRouter(
		r, handlers, jwtAuth, optionalJWTAuth, adminAuth, apiKeyAuth, auditLog,
		stepUpAuth, apiKeyService, subscriptionService, opsService, settingService,
		compositeResolver, cfg, redisClient,
	)
	routes.RegisterCustomExtensionRoutes(
		r.Group("/api/v1"), handlers, customHandlers, adminAuth, auditLog, settingService,
	)
	return r
}
