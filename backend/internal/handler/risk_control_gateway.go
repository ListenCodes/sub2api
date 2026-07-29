package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	riskEventTypeKey      = "risk_event_type"
	riskErrorCodeKey      = "risk_error_code"
	riskErrorMessageKey   = "risk_error_message"
	riskUpstreamStatusKey = "risk_upstream_status"
)

// RiskEventObservation is the redacted, provider-neutral outcome of one API
// request. It intentionally contains no request body or credential material.
type RiskEventObservation struct {
	Method         string
	Endpoint       string
	Model          string
	HTTPStatus     int
	ErrorCode      string
	ErrorMessage   string
	UpstreamStatus int
}

type RiskBanHandler func(context.Context, int64, string) error

func classifyRiskEvent(observation RiskEventObservation) (string, string) {
	if strings.EqualFold(strings.TrimSpace(observation.ErrorCode), "content_policy_violation") {
		return "content_risk", "content"
	}
	if observation.UpstreamStatus > 0 || strings.EqualFold(strings.TrimSpace(observation.ErrorCode), "upstream_error") {
		return "upstream_error", "upstream"
	}
	if observation.HTTPStatus == http.StatusTooManyRequests || strings.Contains(strings.ToLower(observation.ErrorCode), "quota") || strings.Contains(strings.ToLower(observation.ErrorCode), "limit") {
		return "quota_exceeded", "quota"
	}
	if observation.HTTPStatus >= 400 {
		return "api_error", "api"
	}
	return "api_request", "api"
}

func SetRiskEventContext(c *gin.Context, eventType, errorCode, message string) {
	if c == nil {
		return
	}
	if eventType = strings.TrimSpace(eventType); eventType == "api_error" {
		switch strings.ToLower(strings.TrimSpace(errorCode)) {
		case "content_policy_violation":
			eventType = "content_risk"
		case "upstream_error":
			eventType = "upstream_error"
		case "billing_error", "insufficient_balance", "insufficient_quota":
			eventType = "quota_exceeded"
		default:
			lowerCode := strings.ToLower(strings.TrimSpace(errorCode))
			if strings.Contains(lowerCode, "quota") || strings.Contains(lowerCode, "limit") {
				eventType = "quota_exceeded"
			}
		}
	}
	if existing, ok := c.Get(riskEventTypeKey); ok {
		if existingType, typeOK := existing.(string); typeOK && existingType != "" && eventType == "api_error" {
			eventType = existingType
		}
	}
	if eventType != "" {
		c.Set(riskEventTypeKey, eventType)
	}
	if errorCode = strings.TrimSpace(errorCode); errorCode != "" {
		c.Set(riskErrorCodeKey, errorCode)
	}
	if message = strings.TrimSpace(message); message != "" {
		c.Set(riskErrorMessageKey, message)
	}
}

type RiskEventPredicate func(*gin.Context) bool

func RiskEventMiddleware(client *service.RiskControlClient, banHandlers ...RiskBanHandler) gin.HandlerFunc {
	return RiskEventMiddlewareWhen(client, nil, banHandlers...)
}

func RiskEventMiddlewareWhen(client *service.RiskControlClient, shouldReport RiskEventPredicate, banHandlers ...RiskBanHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if shouldReport != nil && !shouldReport(c) {
			return
		}
		reportRiskEventFromContext(c, client, banHandlers...)
	}
}

func reportRiskEventFromContext(c *gin.Context, client *service.RiskControlClient, banHandlers ...RiskBanHandler) {
	if client == nil || c == nil || c.Request == nil || !shouldReportRiskMethod(c.Request.Method) {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return
	}
	apiKey, _ := middleware2.GetAPIKeyFromContext(c)
	model, _ := c.Get(opsModelKey)
	modelName, _ := model.(string)
	errorCode, _ := c.Get(riskErrorCodeKey)
	errorCodeValue, _ := errorCode.(string)
	errorMessage, _ := c.Get(riskErrorMessageKey)
	errorMessageValue, _ := errorMessage.(string)
	eventTypeValue, _ := c.Get(riskEventTypeKey)
	eventType, _ := eventTypeValue.(string)
	upstreamStatus := riskUpstreamStatus(c)
	if streamError, ok := service.GetOpsStreamError(c); ok {
		if eventType == "" || eventType == "api_request" {
			switch strings.ToLower(streamError.ErrType) {
			case "upstream_error":
				eventType = "upstream_error"
			case "rate_limit_error", "billing_error":
				eventType = "quota_exceeded"
			default:
				eventType = "api_error"
			}
		}
		if errorCodeValue == "" {
			errorCodeValue = streamError.ErrType
		}
		if errorMessageValue == "" {
			errorMessageValue = streamError.Message
		}
	}
	if errorMessageValue == "" {
		if value, ok := c.Get(service.OpsUpstreamErrorMessageKey); ok {
			errorMessageValue, _ = value.(string)
		}
	}
	if errorCodeValue == "" && upstreamStatus > 0 {
		errorCodeValue = "upstream_error"
	}
	if errorMessageValue == "" {
		if c.Writer.Status() >= 400 {
			errorMessageValue = "gateway request failed with HTTP " + strconv.Itoa(c.Writer.Status())
		} else if eventType == "api_request" {
			errorMessageValue = "gateway request completed"
		}
	}
	classifiedEventType, _ := classifyRiskEvent(RiskEventObservation{HTTPStatus: c.Writer.Status(), ErrorCode: errorCodeValue, UpstreamStatus: upstreamStatus})
	if eventType == "" || (eventType == "api_error" && classifiedEventType != "api_request") {
		eventType = classifiedEventType
	}
	status := c.Writer.Status()
	evidence := map[string]any{"http_status": status}
	if upstreamStatus > 0 {
		evidence["upstream_status"] = upstreamStatus
	}
	if errorMessageValue != "" {
		evidence["error_present"] = true
	}
	username, accountStatus, emailHash := "", "", ""
	if apiKey != nil && apiKey.User != nil {
		username = apiKey.User.Username
		accountStatus = apiKey.User.Status
		emailHash = service.HashRiskValue(apiKey.User.Email)
	}
	endpoint := GetInboundEndpoint(c)
	if endpoint == "" {
		endpoint = c.Request.URL.Path
	}
	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if requestID == "" {
		if value, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok {
			requestID = strings.TrimSpace(value)
		}
	}
	if requestID == "" {
		requestID = randomRiskEventID()
	}
	fallbackDevice := ""
	if apiKey != nil && apiKey.ID > 0 {
		fallbackDevice = "api-key:" + strconv.FormatInt(apiKey.ID, 10)
	}
	ipHash, deviceHash := riskAssociationHashes(c, fallbackDevice)
	input := service.RiskEventReport{
		EventKey:  requestID + ":risk:" + endpoint,
		EventType: eventType, RiskType: eventType, UserID: subject.UserID,
		UsernameSnapshot: username, AccountStatusSnapshot: accountStatus,
		EmailHash: emailHash, IPHash: ipHash, DeviceHash: deviceHash, ErrorCode: errorCodeValue, Reason: errorMessageValue,
		Endpoint: endpoint, Model: strings.TrimSpace(modelName), HTTPStatus: status,
		Evidence: evidence, OccurredAt: time.Now().UTC(),
	}
	if len(banHandlers) == 0 || banHandlers[0] == nil {
		go reportRiskEvent(client, input)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	decision, err := client.EvaluateEvent(ctx, input)
	cancel()
	if err != nil {
		slog.Warn("risk control decision failed", "error", err, "user_id", subject.UserID)
		return
	}
	if shouldApplyRiskBan(decision) {
		if err := banHandlers[0](context.Background(), subject.UserID, decision.Reason); err != nil {
			slog.Warn("risk control auto-ban failed", "error", err, "user_id", subject.UserID)
		}
	}
}

func shouldReportRiskMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func shouldApplyRiskBan(decision *service.RiskDecision) bool {
	return decision != nil && decision.Action == "ban" && decision.Mode == "enforce"
}

func riskUpstreamStatus(c *gin.Context) int {
	if c == nil {
		return 0
	}
	if value, ok := c.Get(service.OpsUpstreamStatusCodeKey); ok {
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		}
	}
	if value, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*service.OpsUpstreamErrorEvent); ok && len(events) > 0 && events[len(events)-1] != nil {
			return events[len(events)-1].UpstreamStatusCode
		}
	}
	return 0
}

func reportRiskEvent(client *service.RiskControlClient, input service.RiskEventReport) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = client.ReportEvent(ctx, input)
}

func randomRiskEventID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf[:])
}
