package externalmcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A server that cleanly 404s every well-known URL has not published metadata:
// a real absence, not a failed probe.
func TestDiscoverOAuthMetadata_NotFoundIsNotProbeIncomplete(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(t.Context(), testenv.NewLogger(t), policy, "", server.URL+"/mcp")
	require.NoError(t, err)
	require.Equal(t, OAuthVersionNone, result.Version)
	require.False(t, result.ProbeIncomplete, "clean 404s are not a failed probe")
}

// A host that cannot be reached leaves publication unknown — the result must
// say the probes did not complete, so callers can record a gap instead of
// reading a dead host as "publishes no OAuth metadata".
func TestDiscoverOAuthMetadata_UnreachableHostIsProbeIncomplete(t *testing.T) {
	t.Parallel()

	// A closed port: the listener is bound then immediately released, so
	// connections are refused rather than answered.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := server.URL
	server.Close()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(t.Context(), testenv.NewLogger(t), policy, "", deadURL+"/mcp")
	require.NoError(t, err)
	require.Equal(t, OAuthVersionNone, result.Version)
	require.True(t, result.ProbeIncomplete, "an unreachable host must not read as published-nothing")
}
