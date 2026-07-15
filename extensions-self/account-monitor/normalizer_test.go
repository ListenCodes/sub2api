package accountmonitor

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeSuccessfulUsage(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	batch, err := Normalize([]UsageSourceRow{{
		ID: 10, CreatedAt: now, UserID: 2, APIKeyID: 3, AccountID: 7,
		RequestID: "r1", Platform: "openai", Model: "alias", RequestedModel: "gpt-5",
		UpstreamModel: "gpt-5.4", ActualCost: 0.2, TotalCost: 0.1,
		AccountRateMultiplier: 1.5, InputTokens: 10, OutputTokens: 5,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 1 || len(batch.Requests) != 1 {
		t.Fatalf("attempts=%d requests=%d", len(batch.Attempts), len(batch.Requests))
	}
	attempt := batch.Attempts[0]
	if attempt.EventKey != "usage:10" || attempt.RequestKey != "request:3:r1" {
		t.Fatalf("keys = %q %q", attempt.EventKey, attempt.RequestKey)
	}
	if attempt.ActualModel != "gpt-5.4" || attempt.ModelAttribution != AttributionExact {
		t.Fatalf("model = %q attribution=%q", attempt.ActualModel, attempt.ModelAttribution)
	}
	if attempt.UserCost != 0.2 || attempt.AccountCost != 0.15 {
		t.Fatalf("costs = %v %v", attempt.UserCost, attempt.AccountCost)
	}
}

func TestNormalizeSkipsZeroCostUsagePlaceholder(t *testing.T) {
	batch, err := Normalize([]UsageSourceRow{{ID: 10, ActualCost: 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 0 || len(batch.Requests) != 0 {
		t.Fatalf("zero-cost row produced facts: %+v", batch)
	}
}

func TestNormalizeRetryThenSuccess(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	events, _ := json.Marshal([]UpstreamError{{
		AccountID: 10, Platform: "openai", UpstreamModel: "gpt-5.4",
		UpstreamStatusCode: 429, Kind: "http_error", Message: "rate limited",
	}})
	batch, err := Normalize(
		[]UsageSourceRow{{ID: 9, CreatedAt: now.Add(time.Second), UserID: 2, APIKeyID: 3, AccountID: 11, RequestID: "r1", UpstreamModel: "gpt-5.4", ActualCost: 0.2}},
		[]ErrorSourceRow{{ID: 8, CreatedAt: now, UserID: 2, APIKeyID: 3, RequestID: "r1", StatusCode: 200, UpstreamErrors: events}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(batch.Attempts))
	}
	if batch.Attempts[0].Result != ResultFailed || !batch.Attempts[0].Recovered {
		t.Fatalf("failed attempt = %+v", batch.Attempts[0])
	}
	if batch.Attempts[1].Result != ResultSucceeded {
		t.Fatalf("success attempt = %+v", batch.Attempts[1])
	}
	if len(batch.Requests) != 1 || batch.Requests[0].Result != ResultSucceeded {
		t.Fatalf("request facts = %+v", batch.Requests)
	}
}

func TestNormalizeSyntheticFinalFailureUsesEstimatedModel(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	batch, err := Normalize(nil, []ErrorSourceRow{{
		ID: 8, CreatedAt: now, UserID: 2, APIKeyID: 3, AccountID: 10,
		RequestID: "r2", Model: "alias", RequestedModel: "gpt-5", StatusCode: 502,
		UpstreamStatusCode: 502, ErrorPhase: "upstream",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 1 || batch.Attempts[0].EventKey != "ops:8:synthetic" {
		t.Fatalf("attempts = %+v", batch.Attempts)
	}
	if batch.Attempts[0].ActualModel != "gpt-5" || batch.Attempts[0].ModelAttribution != AttributionEstimated {
		t.Fatalf("model attribution = %+v", batch.Attempts[0])
	}
	if len(batch.Requests) != 1 || batch.Requests[0].Result != ResultFailed {
		t.Fatalf("request facts = %+v", batch.Requests)
	}
}

func TestNormalizePreRoutingFailureCreatesOnlyFallbackRequest(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	batch, err := Normalize(nil, []ErrorSourceRow{{
		ID: 8, CreatedAt: now, UserID: 2, APIKeyID: 3,
		StatusCode: 403, ErrorPhase: "security", ErrorType: "policy_denied",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 0 {
		t.Fatalf("pre-routing failure produced attempts: %+v", batch.Attempts)
	}
	if len(batch.Requests) != 1 || batch.Requests[0].RequestKey != "ops:8" || batch.Requests[0].IdentityQuality != IdentityFallback {
		t.Fatalf("request facts = %+v", batch.Requests)
	}
}

func TestNormalizeMultipleUpstreamEventsDoNotAddSyntheticFailure(t *testing.T) {
	events, _ := json.Marshal([]UpstreamError{{AccountID: 10}, {AccountID: 11}})
	batch, err := Normalize(nil, []ErrorSourceRow{{ID: 8, AccountID: 12, StatusCode: 503, UpstreamErrors: events}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(batch.Attempts))
	}
	if batch.Attempts[0].EventKey != "ops:8:event:0" || batch.Attempts[1].EventKey != "ops:8:event:1" {
		t.Fatalf("event keys = %q %q", batch.Attempts[0].EventKey, batch.Attempts[1].EventKey)
	}
}

func TestNormalizeFinalSuccessOwnsGroupIdentity(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	failedGroupID := int64(7)
	successGroupID := int64(9)
	batch, err := Normalize(
		[]UsageSourceRow{{
			ID: 20, CreatedAt: now.Add(time.Second), APIKeyID: 3, RequestID: "r-group",
			AccountID: 11, GroupID: &successGroupID, UpstreamModel: "gpt-success", ActualCost: 1,
		}},
		[]ErrorSourceRow{{
			ID: 19, CreatedAt: now, APIKeyID: 3, RequestID: "r-group", AccountID: 10,
			GroupID: &failedGroupID, UpstreamModel: "gpt-failed", StatusCode: 502,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Requests) != 1 || batch.Requests[0].Result != ResultSucceeded {
		t.Fatalf("request facts = %+v", batch.Requests)
	}
	assertRequestGroupID(t, batch.Requests[0], successGroupID)
	if batch.Requests[0].ActualModel != "gpt-success" {
		t.Fatalf("actual model = %q", batch.Requests[0].ActualModel)
	}
}

func TestNormalizeFinalFailureUsesLatestSourceOrder(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	latestGroupID := int64(12)
	staleGroupID := int64(8)
	batch, err := Normalize(nil, []ErrorSourceRow{
		{ID: 22, CreatedAt: now.Add(time.Minute), APIKeyID: 4, RequestID: "r-fail", GroupID: &latestGroupID, UpstreamModel: "latest", StatusCode: 503},
		{ID: 21, CreatedAt: now, APIKeyID: 4, RequestID: "r-fail", GroupID: &staleGroupID, UpstreamModel: "stale", StatusCode: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Requests) != 1 {
		t.Fatalf("request facts = %+v", batch.Requests)
	}
	assertRequestGroupID(t, batch.Requests[0], latestGroupID)
	if batch.Requests[0].ActualModel != "latest" || batch.Requests[0].SourceID != 22 {
		t.Fatalf("latest failure was replaced: %+v", batch.Requests[0])
	}
}

func assertRequestGroupID(t *testing.T, fact RequestFact, want int64) {
	t.Helper()
	field := reflect.ValueOf(fact).FieldByName("GroupID")
	if !field.IsValid() {
		t.Fatal("RequestFact is missing GroupID")
	}
	if field.IsNil() || field.Elem().Int() != want {
		t.Fatalf("GroupID = %v, want %d", field.Interface(), want)
	}
}
