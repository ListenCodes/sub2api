package accountmonitor

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolveThresholdsUsesMostSpecificOverride(t *testing.T) {
	globalRate := 0.95
	platformRate := 0.93
	parentRate := 0.91
	accountRate := 0.88
	got := ResolveThresholds(DefaultThresholds(), []ThresholdOverride{
		{Scope: ScopeGlobal, SuccessRate: &globalRate},
		{Scope: ScopePlatform, ScopeID: 4, SuccessRate: &platformRate},
		{Scope: ScopeParent, ScopeID: 7, SuccessRate: &parentRate},
		{Scope: ScopeAccount, ScopeID: 9, SuccessRate: &accountRate},
	}, ThresholdContext{PlatformID: 4, ParentAccountID: 7, AccountID: 9})
	if got.SuccessRate != accountRate {
		t.Fatalf("success rate = %v, want %v", got.SuccessRate, accountRate)
	}
}

func TestEvaluateHealthRiskScoreFormula(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	thresholds := DefaultThresholds()
	tests := []struct {
		name    string
		metrics HealthMetrics
		score   int
	}{
		{name: "auth threshold", metrics: HealthMetrics{AuthOrQuotaFailures15M: 3}, score: 70},
		{name: "auth cap", metrics: HealthMetrics{AuthOrQuotaFailures15M: 8}, score: 90},
		{name: "success rate threshold delta", metrics: HealthMetrics{Attempts1H: 20, Successes1H: 17, Failures1H: 3, LastSuccessAt: now}, score: 41},
		{name: "consecutive threshold", metrics: HealthMetrics{ConsecutiveModelFailures: 5}, score: 40},
		{name: "consecutive cap", metrics: HealthMetrics{ConsecutiveModelFailures: 10}, score: 60},
		{name: "throttle growth", metrics: HealthMetrics{RateOrOverloadRatio15M: 0.31}, score: 20},
		{name: "throttle cap", metrics: HealthMetrics{RateOrOverloadRatio15M: 1}, score: 35},
		{name: "no success", metrics: HealthMetrics{Attempts1H: 1}, score: 25},
		{name: "latency", metrics: HealthMetrics{P95DurationMS: 201, BaselineP95DurationMS: 100}, score: 20},
		{name: "user concentration", metrics: HealthMetrics{Attempts24H: 100, TopUserRatio24H: 0.71}, score: 20},
		{name: "traffic", metrics: HealthMetrics{CurrentHourVolume: 31, BaselineHourVolume: 10}, score: 15},
		{name: "sum capped", metrics: HealthMetrics{AuthOrQuotaFailures15M: 8, ConsecutiveModelFailures: 10, RateOrOverloadRatio15M: 1}, score: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			health := EvaluateHealth(test.metrics, thresholds, now)
			if got := healthRiskScore(t, health); got != test.score {
				t.Fatalf("risk score = %d, want %d; health=%+v", got, test.score, health)
			}
			if !healthRiskAvailable(t, health) {
				t.Fatal("risk score should be available")
			}
		})
	}
}

func TestEvaluateHealthRiskScoreUnavailableWithoutSamples(t *testing.T) {
	health := EvaluateHealth(HealthMetrics{}, DefaultThresholds(), time.Now())
	if got := healthRiskScore(t, health); got != 0 {
		t.Fatalf("risk score = %d, want 0", got)
	}
	if healthRiskAvailable(t, health) {
		t.Fatal("empty metrics must not be scored")
	}
}

func TestEvaluateHealthReasonsUseContributionOrder(t *testing.T) {
	health := EvaluateHealth(HealthMetrics{
		AuthOrQuotaFailures15M:   3,
		ConsecutiveModelFailures: 5,
		RateOrOverloadRatio15M:   1,
	}, DefaultThresholds(), time.Now())
	if len(health.Reasons) < 3 {
		t.Fatalf("reasons = %v", health.Reasons)
	}
	if !strings.Contains(health.Reasons[0], "认证失效") ||
		!strings.Contains(health.Reasons[1], "连续失败") ||
		!strings.Contains(health.Reasons[2], "限流或过载") {
		t.Fatalf("reason order = %v", health.Reasons)
	}
}

func TestRiskLevelUsesOnlyFinalScoreBoundaries(t *testing.T) {
	tests := []struct {
		score int
		want  HealthLevel
	}{
		{score: 19, want: HealthNormal},
		{score: 20, want: HealthAttention},
		{score: 39, want: HealthAttention},
		{score: 40, want: HealthAbnormal},
		{score: 69, want: HealthAbnormal},
		{score: 70, want: HealthCritical},
		{score: 100, want: HealthCritical},
	}
	for _, test := range tests {
		if got := healthLevelForRiskScore(test.score); got != test.want {
			t.Errorf("score %d level = %q, want %q", test.score, got, test.want)
		}
	}
}

func healthRiskScore(t *testing.T, health Health) int {
	t.Helper()
	field := reflect.ValueOf(health).FieldByName("RiskScore")
	if !field.IsValid() {
		t.Fatal("Health is missing RiskScore")
	}
	return int(field.Int())
}

func healthRiskAvailable(t *testing.T, health Health) bool {
	t.Helper()
	field := reflect.ValueOf(health).FieldByName("RiskScoreAvailable")
	if !field.IsValid() {
		t.Fatal("Health is missing RiskScoreAvailable")
	}
	return field.Bool()
}

func TestEvaluateHealthReturnsExplainableAbnormalReason(t *testing.T) {
	health := EvaluateHealth(HealthMetrics{
		Attempts1H: 82, Successes1H: 63, Failures1H: 19,
		TopErrorCategory: ErrorRateLimited, TopErrorCount: 12,
		LastSuccessAt: time.Now(),
	}, DefaultThresholds(), time.Now())
	if health.Level != HealthAbnormal {
		t.Fatalf("level = %q", health.Level)
	}
	joined := strings.Join(health.Reasons, " ")
	for _, want := range []string{"82", "19", "76.8%", "90.0%", "限流 12"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reason %q missing %q", joined, want)
		}
	}
}

func TestEvaluateHealthAuthenticationFailuresAreCritical(t *testing.T) {
	health := EvaluateHealth(HealthMetrics{AuthOrQuotaFailures15M: 3}, DefaultThresholds(), time.Now())
	if health.Level != HealthCritical {
		t.Fatalf("level = %q, reasons=%v", health.Level, health.Reasons)
	}
}

func TestEvaluateHealthFlagsActiveAccountWithNoSuccess(t *testing.T) {
	health := EvaluateHealth(HealthMetrics{Attempts1H: 8, Failures1H: 8}, DefaultThresholds(), time.Now())
	if health.Level != HealthAttention {
		t.Fatalf("level = %q, reasons=%v", health.Level, health.Reasons)
	}
	if joined := strings.Join(health.Reasons, " "); !strings.Contains(joined, "没有成功调用") {
		t.Fatalf("reasons = %q", joined)
	}
}

func TestPlatformScopeIDIsStableAndDistinct(t *testing.T) {
	if PlatformScopeID(" Anthropic ") != PlatformScopeID("anthropic") {
		t.Fatal("platform scope id must normalize case and whitespace")
	}
	if PlatformScopeID("anthropic") == 0 || PlatformScopeID("anthropic") == PlatformScopeID("openai") {
		t.Fatal("platform scope ids must be non-zero and distinct")
	}
}
