package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) ProxyExtensionsHomepage(c *gin.Context) {
	h.proxyExtensionsAsset(c)
}

func (h *AuthHandler) proxyExtensionsAsset(c *gin.Context) {
	if h == nil || h.riskControlClient == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	asset, err := h.riskControlClient.ProxyHomepage(c.Request.Context(), c.Request.Method, c.Param("path"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidHomepageRequest):
			c.Status(http.StatusBadRequest)
		case errors.Is(err, service.ErrHomepageResponseTooLarge):
			c.Status(http.StatusBadGateway)
		default:
			c.Status(http.StatusServiceUnavailable)
		}
		return
	}
	if asset.CacheControl != "" {
		c.Header("Cache-Control", asset.CacheControl)
	}
	contentType := asset.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Data(asset.Status, contentType, asset.Body)
}
