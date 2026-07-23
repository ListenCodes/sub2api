package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// CustomExtensionRoutes owns production-only proxies while keeping the
// official route assemblers independent of custom path details.
type CustomExtensionRoutes struct {
	GatewayRiskEvents gin.HandlerFunc
}

// RegisterCustomExtensionRoutes registers all custom public/admin routes once
// and returns the middleware hook consumed by the official gateway assembler.
func RegisterCustomExtensionRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
) *CustomExtensionRoutes {
	admin := v1.Group("/admin")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(gin.HandlerFunc(auditLog))
	admin.Use(middleware.AdminComplianceGuard(settingService))
	registerCustomAdminRoutes(admin, h)

	public := v1.Group("/extensions-self")
	public.GET("/homepage/*path", h.Auth.ProxyExtensionsHomepage)
	public.HEAD("/homepage/*path", h.Auth.ProxyExtensionsHomepage)

	return &CustomExtensionRoutes{
		GatewayRiskEvents: handler.RiskEventMiddleware(
			service.NewRiskControlClientFromEnv(),
			h.Admin.User.ApplyRiskBan,
		),
	}
}

func registerCustomAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	admin.Group("/user-risk-control").Any("/*path", h.Admin.User.ProxyRiskControl)
	admin.Group("/extensions-self/account-monitor").Any("/*path", h.Admin.User.ProxyAccountMonitor)
	admin.POST("/users/:id/risk-status", h.Admin.User.SetRiskStatus)
	admin.GET("/system/custom-release/check", h.Admin.System.CheckCustomRelease)
	admin.GET("/system/release", h.Admin.System.CurrentRelease)
	admin.GET("/system/releases/rollback", h.Admin.System.ListRollbackReleases)
	admin.POST("/system/update/prepare", h.Admin.System.PrepareUpdate)
	admin.POST("/system/update/apply", h.Admin.System.ApplyUpdate)
	admin.POST("/system/rollback/prepare", h.Admin.System.PrepareRollback)
	admin.POST("/system/rollback/apply", h.Admin.System.ApplyRollback)
	admin.GET("/system/update/status", h.Admin.System.GetUpdateStatus)
}
