package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSafeUpstreamURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips query", "https://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com/v1/messages"},
		{"strips fragment", "https://api.openai.com/v1/responses#frag", "https://api.openai.com/v1/responses"},
		{"strips both", "https://host/path?token=secret#x", "https://host/path"},
		{"no query or fragment", "https://host/path", "https://host/path"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://h/p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}

func TestAppendOpsUpstreamErrorCopiesMappedModelFromContext(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(OpsUpstreamModelKey, "gpt-5.4")

	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 7})

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := raw.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "gpt-5.4", events[0].UpstreamModel)
}

func TestAppendOpsUpstreamErrorPreservesEventSpecificModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(OpsUpstreamModelKey, "request-model")

	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{UpstreamModel: "attempt-model"})

	raw, _ := c.Get(OpsUpstreamErrorsKey)
	events := raw.([]*OpsUpstreamErrorEvent)
	require.Equal(t, "attempt-model", events[0].UpstreamModel)
}

func TestParseOpsUpstreamErrorsAcceptsHistoricalJSONWithoutModel(t *testing.T) {
	events, err := ParseOpsUpstreamErrors(`[{"account_id":7,"upstream_status_code":429}]`)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Empty(t, events[0].UpstreamModel)
}
