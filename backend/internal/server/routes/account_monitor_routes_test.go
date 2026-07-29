package routes

import (
	"os"
	"strings"
	"testing"
)

func TestAccountMonitorRoutesUseAdminAuthentication(t *testing.T) {
	raw, err := os.ReadFile("custom_extensions.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		`admin.Group("/extensions-self/account-monitor").Any("/*path", custom.AdminUser.ProxyAccountMonitor)`,
		`admin.Use(gin.HandlerFunc(adminAuth))`,
		`admin.Use(gin.HandlerFunc(auditLog))`,
		`admin.Use(middleware.AdminComplianceGuard(settingService))`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("admin routes missing %q", required)
		}
	}
	for _, forbidden := range []string{`v1.Group("/extensions-self/account-monitor")`, `ProxyExtensionsAccountMonitor`} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("admin routes still contain obsolete public monitor route %q", forbidden)
		}
	}
}
