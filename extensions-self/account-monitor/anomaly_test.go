package accountmonitor

import (
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
