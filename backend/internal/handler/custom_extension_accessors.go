package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

func (h *AuthHandler) AuthServiceForCustomExtensions() *service.AuthService {
	if h == nil {
		return nil
	}
	return h.authService
}

func (h *AuthHandler) RiskControlClientForCustomExtensions() *service.RiskControlClient {
	if h == nil {
		return nil
	}
	return h.riskControlClient
}
