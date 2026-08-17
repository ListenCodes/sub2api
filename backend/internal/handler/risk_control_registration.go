package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const riskDeviceContextKey = "risk_device_id"

func (h *AuthHandler) reportRegistrationRisk(c *gin.Context, req RegisterRequest, user *service.User) {
	if h == nil || h.riskControlClient == nil || c == nil || user == nil {
		return
	}
	h.reportRegistrationEvent(c, strings.TrimSpace(req.Email), "email", user, "/api/v1/auth/register")
}

func (h *AuthHandler) reportOAuthRegistrationRisk(c *gin.Context, user *service.User, provider string, email string) {
	if user == nil {
		return
	}
	h.reportRegistrationEvent(c, email, provider, user, "/api/v1/auth/oauth/"+strings.TrimSpace(provider)+"/complete-registration")
}

func (h *AuthHandler) reportIdentityLoginSuccess(c *gin.Context, user *service.User, eventClass string) {
	if h == nil || h.riskControlClient == nil || c == nil || user == nil || !h.riskControlClient.IdentityEnabled() {
		return
	}
	eventType := "login_success"
	if eventClass == "oauth" {
		eventType = "oauth_login_success"
	}
	enqueueRiskIdentity(c, h.riskControlClient, eventType, eventClass, "success", user.Email, user.ID, 0)
}

func (h *AuthHandler) reportRegistrationEvent(c *gin.Context, email, source string, user *service.User, endpoint string) {
	if h == nil || h.riskControlClient == nil || c == nil || user == nil {
		return
	}
	enqueueRiskIdentity(c, h.riskControlClient, "registration_success", "registration", "success", email, user.ID, 0)
	if h.riskControlClient.IdentityEnabled() {
		return
	}
	deviceID := ensureRiskDeviceCookie(c)
	requestID := requestRiskIdentity(c, false).EventRoot
	clientIP := ip.GetTrustedClientIP(c)
	emailHash := service.HashRiskValue(email)
	input := service.RiskEventReport{EventKey: requestID + ":registration_success:" + strings.TrimSpace(source), EventType: "registration_success", RiskType: "registration_success", UserID: user.ID, SubjectID: emailHash, UsernameSnapshot: user.Username, AccountStatusSnapshot: user.Status, EmailHash: emailHash, IPHash: service.HashRiskValue(clientIP), DeviceHash: service.HashRiskValue(deviceID), Endpoint: endpoint, HTTPStatus: http.StatusOK, OccurredAt: time.Now().UTC(), Evidence: map[string]any{"source": strings.TrimSpace(source)}}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if err := h.riskControlClient.ReportEvent(ctx, input); err != nil {
			slog.Warn("risk control shadow report failed", "error", err, "user_id", user.ID)
		}
	}()
}

func (h *AuthHandler) preflightRegistrationRisk(c *gin.Context, email, source string) error {
	if h == nil || h.riskControlClient == nil || c == nil {
		return nil
	}
	enqueueRiskIdentity(c, h.riskControlClient, "registration_attempt", "registration", "attempt", email, 0, 0)
	if h.riskControlClient.IdentityEnabled() {
		return nil
	}
	deviceID := ensureRiskDeviceCookie(c)
	requestID := requestRiskIdentity(c, false).EventRoot
	input := service.RiskEventReport{EventKey: requestID + ":registration_attempt:" + strings.TrimSpace(source), EventType: "registration_attempt", RiskType: "registration_attempt", SubjectID: service.HashRiskValue(email), EmailHash: service.HashRiskValue(email), IPHash: service.HashRiskValue(ip.GetTrustedClientIP(c)), DeviceHash: service.HashRiskValue(deviceID), Endpoint: "/api/v1/auth/register", HTTPStatus: http.StatusOK, OccurredAt: time.Now().UTC(), Evidence: map[string]any{"source": strings.TrimSpace(source)}}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	decision, err := h.riskControlClient.EvaluateEvent(ctx, input)
	if err != nil {
		return riskControlFailureError(err, riskControlFailClosed())
	}
	return riskDecisionError(decision, "registration")
}

func (h *AuthHandler) preflightLoginRisk(c *gin.Context, req LoginRequest) error {
	if h == nil || h.riskControlClient == nil || c == nil {
		return nil
	}
	enqueueRiskIdentity(c, h.riskControlClient, "login_attempt", "login", "attempt", req.Email, 0, 0)
	requestID := requestRiskIdentity(c, false).EventRoot
	ipHash, deviceHash := riskAssociationHashes(c, "")
	if h.riskControlClient.IdentityEnabled() {
		ipHash, deviceHash = "", ""
	}
	input := service.RiskEventReport{EventKey: requestID + ":login_attempt", EventType: "login_attempt", RiskType: "login_attempt", SubjectID: service.HashRiskValue(req.Email), EmailHash: service.HashRiskValue(req.Email), IPHash: ipHash, DeviceHash: deviceHash, Endpoint: "/api/v1/auth/login", HTTPStatus: http.StatusOK, OccurredAt: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	decision, err := h.riskControlClient.EvaluateEvent(ctx, input)
	if err != nil {
		return riskControlFailureError(err, riskControlFailClosed() && !h.riskControlClient.IdentityEnabled())
	}
	return riskDecisionError(decision, "login")
}

func riskDecisionError(decision *service.RiskDecision, operation string) error {
	if decision == nil || !strings.EqualFold(strings.TrimSpace(decision.Mode), "enforce") || (decision.Action != "reject_candidate" && decision.Action != "ban") {
		return nil
	}
	if operation == "login" {
		return infraerrors.Unauthorized("INVALID_CREDENTIALS", "Invalid email or password")
	}
	return infraerrors.Forbidden("RISK_CONTROL_BLOCKED", "This request was rejected by the risk control policy")
}

func riskControlFailureError(err error, failClosed bool) error {
	if !failClosed {
		return nil
	}
	return infraerrors.ServiceUnavailable("RISK_CONTROL_UNAVAILABLE", "Risk control service is temporarily unavailable").WithCause(err)
}

func riskControlFailClosed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("RISK_CONTROL_DECISION_FAIL_MODE")), "closed")
}

func (h *AuthHandler) reportLoginRisk(c *gin.Context, req LoginRequest, user *service.User, err error) {
	if h == nil || h.riskControlClient == nil || c == nil {
		return
	}
	requestID := requestRiskIdentity(c, false).EventRoot
	eventType := "login_success"
	status := http.StatusOK
	errorCode := ""
	reason := ""
	userID := int64(0)
	username := ""
	accountStatus := ""
	if err != nil {
		eventType = "login_failure"
		status = http.StatusUnauthorized
		errorCode = infraerrors.Reason(err)
		reason = infraerrors.Message(err)
	}
	if user != nil {
		userID, username, accountStatus = user.ID, user.Username, user.Status
	}
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	enqueueRiskIdentity(c, h.riskControlClient, eventType, "login", outcome, req.Email, userID, 0)
	ipHash, deviceHash := riskAssociationHashes(c, "")
	if h.riskControlClient.IdentityEnabled() {
		ipHash, deviceHash = "", ""
	}
	input := service.RiskEventReport{
		EventKey: requestID + ":" + eventType, EventType: eventType, RiskType: eventType, UserID: userID,
		SubjectID:        service.HashRiskValue(req.Email),
		UsernameSnapshot: username, AccountStatusSnapshot: accountStatus, EmailHash: service.HashRiskValue(req.Email),
		IPHash: ipHash, DeviceHash: deviceHash, ErrorCode: errorCode, Reason: reason, Endpoint: "/api/v1/auth/login", HTTPStatus: status, OccurredAt: time.Now().UTC(),
	}
	if err != nil && user != nil && user.ID > 0 && h.riskBanHandler != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 700*time.Millisecond)
		defer cancel()
		decision, evaluateErr := h.riskControlClient.EvaluateEvent(ctx, input)
		if evaluateErr != nil {
			slog.Warn("risk control login failure evaluation failed", "error", evaluateErr, "user_id", user.ID)
			return
		}
		if shouldApplyRiskBan(decision) {
			if banErr := h.riskBanHandler(ctx, user.ID, decision.Reason); banErr != nil {
				slog.Warn("risk control login failure auto-ban failed", "error", banErr, "user_id", user.ID)
			}
		}
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if reportErr := h.riskControlClient.ReportEvent(ctx, input); reportErr != nil {
			slog.Warn("risk control login report failed", "error", reportErr, "user_id", userID)
		}
	}()
}

func ensureRiskDeviceCookie(c *gin.Context) string {
	if strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_KEY")) != "" {
		value, _ := ensureSignedRiskDeviceCookie(c)
		return value
	}
	if deviceID := existingRiskDeviceID(c); deviceID != "" {
		return deviceID
	}
	value := randomRiskID()
	c.Set(riskDeviceContextKey, value)
	http.SetCookie(c.Writer, &http.Cookie{Name: "risk_device", Value: value, Path: "/", HttpOnly: true, Secure: requestIsHTTPS(c), SameSite: http.SameSiteLaxMode, MaxAge: 180 * 24 * 60 * 60})
	return value
}

func existingRiskDeviceID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_KEY")) != "" {
		value, _ := ensureSignedRiskDeviceCookie(c)
		return value
	}
	if value, ok := c.Get(riskDeviceContextKey); ok {
		if rawDeviceID, isString := value.(string); isString {
			if deviceID := strings.TrimSpace(rawDeviceID); deviceID != "" {
				return deviceID
			}
		}
	}
	if cookie, err := c.Request.Cookie("risk_device"); err == nil {
		if deviceID := strings.TrimSpace(cookie.Value); deviceID != "" {
			c.Set(riskDeviceContextKey, deviceID)
			return deviceID
		}
	}
	return ""
}

func riskAssociationHashes(c *gin.Context, fallbackDevice string) (string, string) {
	ipHash := ""
	if clientIP := strings.TrimSpace(ip.GetTrustedClientIP(c)); clientIP != "" {
		ipHash = service.HashRiskValue(clientIP)
	}
	deviceID := strings.TrimSpace(fallbackDevice)
	if deviceID == "" {
		deviceID = existingRiskDeviceID(c)
	}
	if deviceID == "" && c != nil && c.Request != nil {
		deviceID = ensureRiskDeviceCookie(c)
	}
	if deviceID == "" {
		return ipHash, ""
	}
	return ipHash, service.HashRiskValue(deviceID)
}

func requestIsHTTPS(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.TLS != nil {
		return true
	}
	remoteIP := remoteAddress(c.Request.RemoteAddr)
	trustedIP := strings.TrimSpace(ip.GetTrustedClientIP(c))
	if remoteIP == "" || trustedIP == "" || remoteIP == trustedIP {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0]), "https")
}

func randomRiskID() string {
	var buf [18]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(buf[:])
}
