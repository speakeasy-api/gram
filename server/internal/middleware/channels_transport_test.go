package middleware

import (
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/stretchr/testify/require"
)

func TestChannelsMCPTransportClassification(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/channels/example-org/example-project/api", "/channels/example-org/example-project/mcp/authorize", "/.well-known/oauth-protected-resource/channels/example-org/example-project/mcp"} {
		require.False(t, isMCPJSONRPCEndpoint(path))
	}
	native := mcpPost("/channels/example-org/example-project/mcp")
	native.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20260728)
	_, reached := serveMCPSecurity(t, native)
	require.True(t, reached)
	hostile := mcpPost("/channels/example-org/example-project/mcp")
	hostile.Header.Set("Origin", hostileOrigin)
	recorder, reached := serveMCPSecurity(t, hostile)
	require.False(t, reached)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	simple := mcpPost("/channels/example-org/example-project/mcp")
	simple.Header.Set("Content-Type", "text/plain")
	recorder, reached = serveMCPSecurity(t, simple)
	require.False(t, reached)
	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
}
