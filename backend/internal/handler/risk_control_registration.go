package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) reportRegistrationRisk(c *gin.Context, req RegisterRequest, user *service.User) {
	if h == nil || h.riskControlClient == nil || c == nil || user == nil { return }
	deviceID := ensureRiskDeviceCookie(c)
	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID")); if requestID == "" { requestID = randomRiskID() }
	input := service.RiskRegistrationReport{RequestID: requestID, SubjectID: formatRiskSubjectID(user.ID), Email: req.Email, IP: ip.GetTrustedClientIP(c), DeviceID: deviceID}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond); defer cancel()
		if err := h.riskControlClient.ReportRegistration(ctx, input); err != nil { slog.Warn("risk control shadow report failed", "error", err, "user_id", user.ID) }
	}()
}

func ensureRiskDeviceCookie(c *gin.Context) string {
	if cookie, err := c.Request.Cookie("risk_device"); err == nil && strings.TrimSpace(cookie.Value) != "" { return cookie.Value }
	value := randomRiskID()
	http.SetCookie(c.Writer, &http.Cookie{Name: "risk_device", Value: value, Path: "/", HttpOnly: true, Secure: requestIsHTTPS(c), SameSite: http.SameSiteLaxMode, MaxAge: 180 * 24 * 60 * 60})
	return value
}

func requestIsHTTPS(c *gin.Context) bool {
	if c == nil || c.Request == nil { return false }
	if c.Request.TLS != nil { return true }
	return strings.EqualFold(strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0]), "https")
}

func randomRiskID() string { var buf [18]byte; if _, err := rand.Read(buf[:]); err != nil { return "unavailable" }; return hex.EncodeToString(buf[:]) }
func formatRiskSubjectID(id int64) string { return fmt.Sprintf("%d", id) }
