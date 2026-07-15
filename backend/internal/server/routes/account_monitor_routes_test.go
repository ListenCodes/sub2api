package routes

import (
	"os"
	"strings"
	"testing"
)

func TestAccountMonitorRoutesUseAdminAuthentication(t *testing.T) {
	raw, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		`v1.Group("/extensions-self/account-monitor")`,
		`monitorPage.Use(gin.HandlerFunc(adminAuth))`,
		`monitorPage.GET("/*path", h.Auth.ProxyExtensionsAccountMonitor)`,
		`admin.Group("/extensions-self/account-monitor").Any("/*path", h.Admin.User.ProxyAccountMonitor)`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("admin routes missing %q", required)
		}
	}
}
