package accountmonitor

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
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

func PlatformScopeID(platform string) int64 {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	if normalized == "" {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(normalized))
	value := int64(hash.Sum64() & uint64(^uint64(0)>>1))
	if value == 0 {
		return 1
	}
	return value
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
	RiskScore          int         `json:"risk_score"`
	RiskScoreAvailable bool        `json:"risk_score_available"`
	Level              HealthLevel `json:"level"`
	Reasons            []string    `json:"reasons"`
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
	health := Health{
		RiskScoreAvailable: healthMetricsAvailable(metrics),
		Level:              HealthNormal,
		Reasons:            make([]string, 0),
	}
	if !health.RiskScoreAvailable {
		return health
	}
	type contribution struct {
		score  int
		order  int
		reason string
	}
	contributions := make([]contribution, 0, 8)
	add := func(score, order int, reason string) {
		contributions = append(contributions, contribution{score: score, order: order, reason: reason})
	}
	if metrics.AuthOrQuotaFailures15M >= thresholds.AuthQuotaFailures15M {
		score := minInt(90, 70+5*(metrics.AuthOrQuotaFailures15M-thresholds.AuthQuotaFailures15M))
		add(score, 0, fmt.Sprintf("近 15 分钟认证失效或额度不足 %d 次，达到严重阈值 %d 次。", metrics.AuthOrQuotaFailures15M, thresholds.AuthQuotaFailures15M))
	}
	if metrics.Attempts1H >= int64(thresholds.MinAttempts1H) {
		rate := 0.0
		if metrics.Attempts1H > 0 {
			rate = float64(metrics.Successes1H) / float64(metrics.Attempts1H)
		}
		if rate < thresholds.SuccessRate {
			score := 40
			if thresholds.SuccessRate > 0 {
				score += int(math.Round(20 * (thresholds.SuccessRate - rate) / thresholds.SuccessRate))
			}
			score = minInt(60, score)
			reason := fmt.Sprintf("近 1 小时调用 %d 次，失败 %d 次，成功率 %.1f%%，低于 %.1f%% 阈值", metrics.Attempts1H, metrics.Failures1H, rate*100, thresholds.SuccessRate*100)
			if metrics.TopErrorCount > 0 {
				reason += fmt.Sprintf("；主要原因：%s %d 次", metrics.TopErrorCategory, metrics.TopErrorCount)
			}
			add(score, 1, reason+"。")
		}
	}
	if metrics.ConsecutiveModelFailures >= thresholds.ConsecutiveModelFailures {
		score := minInt(60, 40+4*(metrics.ConsecutiveModelFailures-thresholds.ConsecutiveModelFailures))
		add(score, 2, fmt.Sprintf("同一实际模型连续失败 %d 次。", metrics.ConsecutiveModelFailures))
	}
	if metrics.RateOrOverloadRatio15M > thresholds.RateErrorsRatio15M {
		score := 20
		if thresholds.RateErrorsRatio15M < 1 {
			score += int(math.Round(15 * (metrics.RateOrOverloadRatio15M - thresholds.RateErrorsRatio15M) / (1 - thresholds.RateErrorsRatio15M)))
		}
		add(minInt(35, score), 3, fmt.Sprintf("近 15 分钟限流或过载占比 %.1f%%，高于 %.1f%% 阈值。", metrics.RateOrOverloadRatio15M*100, thresholds.RateErrorsRatio15M*100))
	}
	if metrics.Attempts1H > 0 && (metrics.LastSuccessAt.IsZero() || now.Sub(metrics.LastSuccessAt) >= thresholds.NoSuccessDuration) {
		add(25, 4, fmt.Sprintf("活跃账号已连续 %s 没有成功调用。", thresholds.NoSuccessDuration))
	}
	if metrics.BaselineP95DurationMS > 0 && float64(metrics.P95DurationMS) > metrics.BaselineP95DurationMS*thresholds.LatencyBaselineRatio {
		add(20, 5, fmt.Sprintf("P95 延迟 %d ms，高于近 7 天基线 %.1f 倍。", metrics.P95DurationMS, thresholds.LatencyBaselineRatio))
	}
	if metrics.Attempts24H >= int64(thresholds.UserConcentrationMinimum) && metrics.TopUserRatio24H > thresholds.UserConcentration {
		add(20, 6, fmt.Sprintf("单用户占近 24 小时调用量 %.1f%%。", metrics.TopUserRatio24H*100))
	}
	if metrics.BaselineHourVolume > 0 {
		ratio := float64(metrics.CurrentHourVolume) / metrics.BaselineHourVolume
		if ratio > thresholds.TrafficHighRatio || ratio < thresholds.TrafficLowRatio {
			add(15, 7, fmt.Sprintf("当前小时调用量为近 7 天同时间基线的 %.1f 倍。", ratio))
		}
	}
	sort.SliceStable(contributions, func(i, j int) bool {
		if contributions[i].score == contributions[j].score {
			return contributions[i].order < contributions[j].order
		}
		return contributions[i].score > contributions[j].score
	})
	for _, item := range contributions {
		health.RiskScore += item.score
		health.Reasons = append(health.Reasons, item.reason)
	}
	health.RiskScore = minInt(100, health.RiskScore)
	health.Level = healthLevelForRiskScore(health.RiskScore)
	return health
}

func healthMetricsAvailable(metrics HealthMetrics) bool {
	return metrics.Attempts1H > 0 || metrics.Attempts24H > 0 ||
		metrics.ConsecutiveModelFailures > 0 || metrics.AuthOrQuotaFailures15M > 0 ||
		metrics.RateOrOverloadRatio15M > 0 || metrics.CurrentHourVolume > 0 ||
		metrics.BaselineHourVolume > 0 || metrics.P95DurationMS > 0 ||
		metrics.BaselineP95DurationMS > 0
}

func healthLevelForRiskScore(score int) HealthLevel {
	switch {
	case score >= 70:
		return HealthCritical
	case score >= 40:
		return HealthAbnormal
	case score >= 20:
		return HealthAttention
	default:
		return HealthNormal
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
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
