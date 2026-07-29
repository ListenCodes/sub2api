package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCustomUserHandlerKeepsRuntimeDependenciesOutOfUserHandler(t *testing.T) {
	t.Setenv("RISK_CONTROL_URL", "http://extensions-self:8090")
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-internal-secret")
	authService := &service.AuthService{}
	settingService := &service.SettingService{}

	base := NewUserHandler(nil, nil, nil, nil, nil, nil, settingService)
	client := service.NewRiskControlClientFromEnv()
	handler := NewCustomUserHandler(base, authService, client)

	require.Same(t, authService, handler.authService)
	require.Same(t, settingService, base.settingService)
	require.Same(t, client, handler.riskControlClient)
}
