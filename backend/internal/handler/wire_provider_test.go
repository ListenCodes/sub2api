package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/stretchr/testify/require"
)

func TestProvideHandlersLeavesCustomRiskWiringToBootstrap(t *testing.T) {
	authHandler := &AuthHandler{}
	adminHandlers := &AdminHandlers{User: &admin.UserHandler{}}

	handlers := ProvideHandlers(
		authHandler,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		adminHandlers,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	require.Same(t, authHandler, handlers.Auth)
	require.Nil(t, authHandler.riskBanHandler)

	custom := NewCustomExtensions(handlers)
	authHandler.SetRiskBanHandler(custom.AdminUser.ApplyRiskBan)
	require.NotNil(t, authHandler.riskBanHandler)
}
