package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type CustomExtensions struct {
	RiskControlClient *service.RiskControlClient
	AdminUser         *admin.CustomUserHandler
}

func NewCustomExtensions(h *Handlers) *CustomExtensions {
	client := service.NewRiskControlClientFromEnv()
	var base *admin.UserHandler
	var authService *service.AuthService
	if h != nil {
		if h.Admin != nil {
			base = h.Admin.User
		}
		if h.Auth != nil {
			authService = h.Auth.AuthServiceForCustomExtensions()
		}
	}
	return &CustomExtensions{
		RiskControlClient: client,
		AdminUser:         admin.NewCustomUserHandler(base, authService, client),
	}
}
