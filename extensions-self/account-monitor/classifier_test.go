package accountmonitor

import "testing"

func TestClassifyFailurePrecedenceAndCategories(t *testing.T) {
	tests := []struct {
		name   string
		signal FailureSignal
		want   ErrorCategory
	}{
		{"provider quota beats 429", FailureSignal{ProviderCode: "insufficient_quota", UpstreamStatus: 429}, ErrorQuota},
		{"provider auth", FailureSignal{ProviderCode: "invalid_api_key"}, ErrorAuth},
		{"provider model", FailureSignal{ProviderCode: "model_not_found"}, ErrorModelUnavailable},
		{"rate limited", FailureSignal{UpstreamStatus: 429}, ErrorRateLimited},
		{"overloaded", FailureSignal{UpstreamStatus: 529}, ErrorOverloaded},
		{"http auth", FailureSignal{UpstreamStatus: 401}, ErrorAuth},
		{"timeout", FailureSignal{UpstreamStatus: 504}, ErrorTimeout},
		{"network", FailureSignal{NetworkType: "connection_reset"}, ErrorNetwork},
		{"upstream", FailureSignal{UpstreamStatus: 503}, ErrorUpstream},
		{"invalid request", FailureSignal{UpstreamStatus: 422}, ErrorInvalidRequest},
		{"safety", FailureSignal{ErrorType: "content_policy_violation"}, ErrorSafety},
		{"no account", FailureSignal{ErrorPhase: "routing", Message: "no available account"}, ErrorNoAccount},
		{"unknown", FailureSignal{Message: "something new"}, ErrorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyFailure(tt.signal); got != tt.want {
				t.Fatalf("ClassifyFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeStoresClassifiedFailure(t *testing.T) {
	batch, err := Normalize(nil, []ErrorSourceRow{{
		ID: 8, AccountID: 10, StatusCode: 429, UpstreamStatusCode: 429,
		ErrorPhase: "upstream", ErrorMessage: "rate limited",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Attempts) != 1 || batch.Attempts[0].ErrorCategory != ErrorRateLimited {
		t.Fatalf("attempts = %+v", batch.Attempts)
	}
	if len(batch.Requests) != 1 || batch.Requests[0].ErrorCategory != ErrorRateLimited {
		t.Fatalf("requests = %+v", batch.Requests)
	}
}
