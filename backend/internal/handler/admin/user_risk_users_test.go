package admin

import "testing"

func TestRiskRowLastEventDoesNotTurnMissingValuesIntoText(t *testing.T) {
	if got := riskRowLastEvent(map[string]any{}); got != "" {
		t.Fatalf("missing last event = %q, want empty", got)
	}
	if got := riskRowLastEvent(map[string]any{"last_event_at": nil}); got != "" {
		t.Fatalf("nil last event = %q, want empty", got)
	}
	if got := riskRowLastEvent(map[string]any{"last_event_at": " 2026-08-18T08:00:00Z "}); got != "2026-08-18T08:00:00Z" {
		t.Fatalf("last event = %q", got)
	}
}
