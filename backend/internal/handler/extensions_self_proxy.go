package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) ProxyExtensionsHomepage(c *gin.Context) {
	h.proxyExtensionsAsset(c, false)
}

func (h *AuthHandler) ProxyExtensionsAccountMonitor(c *gin.Context) {
	h.proxyExtensionsAsset(c, true)
}

func (h *AuthHandler) proxyExtensionsAsset(c *gin.Context, accountMonitor bool) {
	if h == nil || h.riskControlClient == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	var asset *service.HomepageAsset
	var err error
	if accountMonitor {
		asset, err = h.riskControlClient.ProxyAccountMonitorAsset(c.Request.Context(), c.Request.Method, c.Param("path"))
	} else {
		asset, err = h.riskControlClient.ProxyHomepage(c.Request.Context(), c.Request.Method, c.Param("path"))
	}
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
