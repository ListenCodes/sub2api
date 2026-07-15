package accountmonitor

import (
	"fmt"
	"time"
)

type ThresholdScope string

const (
	ScopeGlobal   ThresholdScope = "global"
	ScopePlatform ThresholdScope = "platform"
	ScopeParent   ThresholdScope = "parent"
	ScopeAccount  ThresholdScope = "account"
)

type Thresholds struct {
	MinAttempts1H            int
	SuccessRate              float64
	ConsecutiveModelFailures int
	AuthQuotaFailures15M     int
	RateErrorsRatio15M       float64
	NoSuccessDuration        time.Duration
	UserConcentration        float64
	UserConcentrationMinimum int
	TrafficHighRatio         float64
	TrafficLowRatio          float64
	LatencyBaselineRatio     float64
}

type ThresholdOverride struct {
	Scope       ThresholdScope `json:"scope"`
	ScopeID     int64          `json:"scope_id"`
	SuccessRate *float64       `json:"success_rate,omitempty"`
}

type ThresholdContext struct {
	PlatformID      int64
	ParentAccountID int64
	AccountID       int64
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		MinAttempts1H: 20, SuccessRate: 0.90, ConsecutiveModelFailures: 5,
		AuthQuotaFailures15M: 3, RateErrorsRatio15M: 0.30,
		NoSuccessDuration: 30 * time.Minute, UserConcentration: 0.70,
		UserConcentrationMinimum: 100, TrafficHighRatio: 3,
		TrafficLowRatio: 0.20, LatencyBaselineRatio: 2,
	}
}

func ResolveThresholds(base Thresholds, overrides []ThresholdOverride, ctx ThresholdContext) Thresholds {
	for _, scope := range []ThresholdScope{ScopeGlobal, ScopePlatform, ScopeParent, ScopeAccount} {
		for _, override := range overrides {
			if override.Scope != scope || !thresholdScopeMatches(override, ctx) {
				continue
			}
			if override.SuccessRate != nil {
				base.SuccessRate = *override.SuccessRate
			}
		}
	}
	return base
}

func thresholdScopeMatches(override ThresholdOverride, ctx ThresholdContext) bool {
	switch override.Scope {
	case ScopeGlobal:
		return true
	case ScopePlatform:
		return override.ScopeID == ctx.PlatformID
	case ScopeParent:
		return override.ScopeID == ctx.ParentAccountID
	case ScopeAccount:
		return override.ScopeID == ctx.AccountID
	default:
		return false
	}
}

type HealthLevel string

const (
	HealthNormal    HealthLevel = "normal"
	HealthAttention HealthLevel = "attention"
	HealthAbnormal  HealthLevel = "abnormal"
	HealthCritical  HealthLevel = "critical"
)

type Health struct {
	Level   HealthLevel `json:"level"`
	Reasons []string    `json:"reasons"`
}

type HealthMetrics struct {
	Attempts1H               int64
	Successes1H              int64
	Failures1H               int64
	ConsecutiveModelFailures int
	AuthOrQuotaFailures15M   int
	RateOrOverloadRatio15M   float64
	LastSuccessAt            time.Time
	TopUserRatio24H          float64
	Attempts24H              int64
	CurrentHourVolume        int64
	BaselineHourVolume       float64
	P95DurationMS            int64
	BaselineP95DurationMS    float64
	TopErrorCategory         ErrorCategory
	TopErrorCount            int64
}

func EvaluateHealth(metrics HealthMetrics, thresholds Thresholds, now time.Time) Health {
	health := Health{Level: HealthNormal, Reasons: make([]string, 0)}
	add := func(level HealthLevel, reason string) {
		if healthRank(level) > healthRank(health.Level) {
			health.Level = level
		}
		health.Reasons = append(health.Reasons, reason)
	}
	if metrics.AuthOrQuotaFailures15M >= thresholds.AuthQuotaFailures15M {
		add(HealthCritical, fmt.Sprintf("近 15 分钟认证失效或额度不足 %d 次，达到严重阈值 %d 次。", metrics.AuthOrQuotaFailures15M, thresholds.AuthQuotaFailures15M))
	}
	if metrics.Attempts1H >= int64(thresholds.MinAttempts1H) {
		rate := 0.0
		if metrics.Attempts1H > 0 {
			rate = float64(metrics.Successes1H) / float64(metrics.Attempts1H)
		}
		if rate < thresholds.SuccessRate {
			reason := fmt.Sprintf("近 1 小时调用 %d 次，失败 %d 次，成功率 %.1f%%，低于 %.1f%% 阈值", metrics.Attempts1H, metrics.Failures1H, rate*100, thresholds.SuccessRate*100)
			if metrics.TopErrorCount > 0 {
				reason += fmt.Sprintf("；主要原因：%s %d 次", metrics.TopErrorCategory, metrics.TopErrorCount)
			}
			add(HealthAbnormal, reason+"。")
		}
	}
	if metrics.ConsecutiveModelFailures >= thresholds.ConsecutiveModelFailures {
		add(HealthAbnormal, fmt.Sprintf("同一实际模型连续失败 %d 次。", metrics.ConsecutiveModelFailures))
	}
	if metrics.RateOrOverloadRatio15M > thresholds.RateErrorsRatio15M {
		add(HealthAttention, fmt.Sprintf("近 15 分钟限流或过载占比 %.1f%%，高于 %.1f%% 阈值。", metrics.RateOrOverloadRatio15M*100, thresholds.RateErrorsRatio15M*100))
	}
	if metrics.Attempts1H > 0 && !metrics.LastSuccessAt.IsZero() && now.Sub(metrics.LastSuccessAt) >= thresholds.NoSuccessDuration {
		add(HealthAttention, fmt.Sprintf("活跃账号已连续 %s 没有成功调用。", thresholds.NoSuccessDuration))
	}
	if metrics.Attempts24H >= int64(thresholds.UserConcentrationMinimum) && metrics.TopUserRatio24H > thresholds.UserConcentration {
		add(HealthAttention, fmt.Sprintf("单用户占近 24 小时调用量 %.1f%%。", metrics.TopUserRatio24H*100))
	}
	if metrics.BaselineHourVolume > 0 {
		ratio := float64(metrics.CurrentHourVolume) / metrics.BaselineHourVolume
		if ratio > thresholds.TrafficHighRatio || ratio < thresholds.TrafficLowRatio {
			add(HealthAttention, fmt.Sprintf("当前小时调用量为近 7 天同时间基线的 %.1f 倍。", ratio))
		}
	}
	if metrics.BaselineP95DurationMS > 0 && float64(metrics.P95DurationMS) > metrics.BaselineP95DurationMS*thresholds.LatencyBaselineRatio {
		add(HealthAttention, fmt.Sprintf("P95 延迟 %d ms，高于近 7 天基线 %.1f 倍。", metrics.P95DurationMS, thresholds.LatencyBaselineRatio))
	}
	return health
}

func healthRank(level HealthLevel) int {
	switch level {
	case HealthCritical:
		return 3
	case HealthAbnormal:
		return 2
	case HealthAttention:
		return 1
	default:
		return 0
	}
}
