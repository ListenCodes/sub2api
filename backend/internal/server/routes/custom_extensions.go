package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterCustomExtensionRoutes registers all custom public/admin routes once.
func RegisterCustomExtensionRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	custom *handler.CustomExtensions,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
) {
	admin := v1.Group("/admin")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(gin.HandlerFunc(auditLog))
	admin.Use(middleware.AdminComplianceGuard(settingService))
	registerCustomAdminRoutes(admin, h, custom)

	public := v1.Group("/extensions-self")
	homepageFrameHeaders := middleware.ExtensionsHomepageFrameHeaders()
	public.GET("/homepage/*path", homepageFrameHeaders, h.Auth.ProxyExtensionsHomepage)
	public.HEAD("/homepage/*path", homepageFrameHeaders, h.Auth.ProxyExtensionsHomepage)

}

func registerCustomAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers, custom *handler.CustomExtensions) {
	admin.Group("/user-risk-control").Any("/*path", custom.AdminUser.ProxyRiskControl)
	admin.Group("/extensions-self/account-monitor").Any("/*path", custom.AdminUser.ProxyAccountMonitor)
	admin.POST("/users/:id/risk-status", custom.AdminUser.SetRiskStatus)
	admin.GET("/system/custom-release/check", h.Admin.System.CheckCustomRelease)
	admin.POST("/system/custom-release/read", h.Admin.System.MarkCustomReleaseRead)
	admin.GET("/system/release", h.Admin.System.CurrentRelease)
	admin.GET("/system/releases/rollback", h.Admin.System.ListRollbackReleases)
	admin.POST("/system/update/prepare", h.Admin.System.PrepareUpdate)
	admin.POST("/system/update/apply", h.Admin.System.ApplyUpdate)
	admin.POST("/system/rollback/prepare", h.Admin.System.PrepareRollback)
	admin.POST("/system/rollback/apply", h.Admin.System.ApplyRollback)
	admin.GET("/system/update/status", h.Admin.System.GetUpdateStatus)
}
