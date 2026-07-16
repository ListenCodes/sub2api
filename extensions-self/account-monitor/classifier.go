package accountmonitor

import "strings"

const (
	ErrorRateLimited      ErrorCategory = "限流"
	ErrorOverloaded       ErrorCategory = "上游过载"
	ErrorAuth             ErrorCategory = "账号认证失效"
	ErrorQuota            ErrorCategory = "账号额度不足"
	ErrorModelUnavailable ErrorCategory = "模型不可用"
	ErrorNetwork          ErrorCategory = "网络连接失败"
	ErrorTimeout          ErrorCategory = "请求超时"
	ErrorUpstream         ErrorCategory = "上游服务错误"
	ErrorInvalidRequest   ErrorCategory = "请求参数错误"
	ErrorSafety           ErrorCategory = "内容或安全拦截"
	ErrorNoAccount        ErrorCategory = "无可用账号"
	ErrorUnknown          ErrorCategory = "未知错误"
)

type FailureSignal struct {
	ProviderCode   string
	ProviderType   string
	UpstreamStatus int
	StatusCode     int
	ErrorType      string
	ErrorPhase     string
	NetworkType    string
	Message        string
}

func ClassifyFailure(signal FailureSignal) ErrorCategory {
	provider := normalizeFailureText(signal.ProviderCode + " " + signal.ProviderType)
	switch {
	case containsAny(provider, "insufficient_quota", "quota_exceeded", "credit_balance", "billing_hard_limit", "余额不足", "额度不足"):
		return ErrorQuota
	case containsAny(provider, "invalid_api_key", "authentication", "unauthorized", "token_expired", "invalid_token", "account_deactivated"):
		return ErrorAuth
	case containsAny(provider, "model_not_found", "model_unavailable", "unsupported_model", "deployment_not_found"):
		return ErrorModelUnavailable
	case containsAny(provider, "content_policy", "safety", "moderation", "blocked_content"):
		return ErrorSafety
	case containsAny(provider, "rate_limit", "too_many_requests"):
		return ErrorRateLimited
	case containsAny(provider, "overloaded", "capacity"):
		return ErrorOverloaded
	}

	status := signal.UpstreamStatus
	if status == 0 {
		status = signal.StatusCode
	}
	switch {
	case status == 429:
		return ErrorRateLimited
	case status == 529:
		return ErrorOverloaded
	case status == 401 || status == 403:
		return ErrorAuth
	case status == 408 || status == 504:
		return ErrorTimeout
	case status >= 500:
		return ErrorUpstream
	case status >= 400:
		return ErrorInvalidRequest
	}

	errorType := normalizeFailureText(signal.ErrorType + " " + signal.ErrorPhase)
	switch {
	case containsAny(errorType, "content_policy", "safety", "moderation", "cyber_policy", "security"):
		return ErrorSafety
	case containsAny(errorType, "no_available_account", "routing"):
		if containsAny(normalizeFailureText(signal.Message), "no available account", "no account", "无可用账号") {
			return ErrorNoAccount
		}
	case containsAny(errorType, "timeout", "deadline"):
		return ErrorTimeout
	case containsAny(errorType, "model_not_found", "model_unavailable"):
		return ErrorModelUnavailable
	}

	network := normalizeFailureText(signal.NetworkType)
	if network != "" {
		if containsAny(network, "timeout", "deadline") {
			return ErrorTimeout
		}
		return ErrorNetwork
	}

	message := normalizeFailureText(signal.Message)
	switch {
	case containsAny(message, "no available account", "no account available", "无可用账号"):
		return ErrorNoAccount
	case containsAny(message, "timed out", "timeout", "deadline exceeded"):
		return ErrorTimeout
	case containsAny(message, "connection reset", "connection refused", "dns", "tls handshake", "broken pipe", "network"):
		return ErrorNetwork
	case containsAny(message, "content policy", "safety", "moderation", "blocked by policy"):
		return ErrorSafety
	case containsAny(message, "model not found", "model unavailable", "unsupported model"):
		return ErrorModelUnavailable
	case containsAny(message, "quota", "credit balance", "额度不足", "余额不足"):
		return ErrorQuota
	case containsAny(message, "rate limit", "too many requests"):
		return ErrorRateLimited
	case containsAny(message, "overloaded", "capacity"):
		return ErrorOverloaded
	}
	return ErrorUnknown
}

func normalizeFailureText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
