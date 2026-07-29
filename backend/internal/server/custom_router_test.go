package server

import "testing"

func TestCustomGatewayRiskRouteScope(t *testing.T) {
	tests := map[string]bool{
		"/v1/messages":                      true,
		"/v1/usage":                         true,
		"/v1/images/batches":                true,
		"/v1/sub2api/billing":               false,
		"/v1beta/models/:model":             true,
		"/responses":                        true,
		"/backend-api/codex/responses":      true,
		"/backend-api/codex/realtime/calls": true,
		"/videos/generations":               true,
		"/videos/:request_id":               false,
		"/videos/:request_id/content":       false,
		"/api/v1/admin/users":               false,
		"/antigravity/v1/messages":          true,
		"/antigravity/v1/usage":             true,
		"/antigravity/v1beta/models/:model": true,
	}

	for fullPath, want := range tests {
		t.Run(fullPath, func(t *testing.T) {
			if got := isCustomGatewayRiskRoute(fullPath); got != want {
				t.Fatalf("isCustomGatewayRiskRoute(%q) = %v, want %v", fullPath, got, want)
			}
		})
	}
}
