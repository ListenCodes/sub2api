package handler

import (
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRiskDecisionErrorBlocksOnlyExplicitCandidateRejection(t *testing.T) {
	for _, action := range []string{"allow", "observe", "review"} {
		if err := riskDecisionError(&service.RiskDecision{Action: action}, "registration"); err != nil {
			t.Fatalf("action %q returned error %v", action, err)
		}
	}
	if err := riskDecisionError(&service.RiskDecision{Action: "reject_candidate", Mode: "enforce"}, "registration"); infraerrors.Code(err) != 403 {
		t.Fatalf("reject_candidate error = %v", err)
	}
	if err := riskDecisionError(&service.RiskDecision{Action: "ban", Mode: "enforce"}, "registration"); infraerrors.Code(err) != 403 {
		t.Fatalf("ban error = %v", err)
	}
	for _, action := range []string{"reject_candidate", "ban"} {
		if err := riskDecisionError(&service.RiskDecision{Action: action, Mode: "shadow"}, "registration"); err != nil {
			t.Fatalf("shadow action %q returned error %v", action, err)
		}
	}
}

func TestRiskDecisionFailureIsFailOpenByDefault(t *testing.T) {
	if err := riskControlFailureError(errors.New("risk service unavailable"), false); err != nil {
		t.Fatalf("fail-open returned error %v", err)
	}
	if infraerrors.Code(riskControlFailureError(errors.New("risk service unavailable"), true)) != 503 {
		t.Fatal("fail-closed did not return service unavailable")
	}
}

func TestShouldApplyRiskBanOnlyForEnforcedBanDecision(t *testing.T) {
	if shouldApplyRiskBan(nil) {
		t.Fatal("nil decision must not ban")
	}
	if shouldApplyRiskBan(&service.RiskDecision{Action: "observe", Mode: "enforce"}) {
		t.Fatal("observe decision must not ban")
	}
	if shouldApplyRiskBan(&service.RiskDecision{Action: "ban", Mode: "shadow"}) {
		t.Fatal("shadow decision must not ban")
	}
	if !shouldApplyRiskBan(&service.RiskDecision{Action: "ban", Mode: "enforce"}) {
		t.Fatal("enforced ban decision must ban")
	}
}
