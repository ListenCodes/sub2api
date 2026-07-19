package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProvideUserHandlerWiresRuntimeDependencies(t *testing.T) {
	t.Setenv("RISK_CONTROL_URL", "http://extensions-self:8090")
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "test-internal-secret")
	authService := &service.AuthService{}
	settingService := &service.SettingService{}

	handler := ProvideUserHandler(nil, nil, nil, nil, nil, nil, settingService, authService)

	require.Same(t, authService, handler.authService)
	require.Same(t, settingService, handler.settingService)
	require.NotNil(t, handler.riskControlClient)
}
