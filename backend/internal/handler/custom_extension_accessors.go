package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

func (h *AuthHandler) AuthServiceForCustomExtensions() *service.AuthService {
	if h == nil {
		return nil
	}
	return h.authService
}
