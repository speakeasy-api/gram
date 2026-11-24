package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/requestip"
)

// A feedback tool may not take a name the gateway answers itself in dynamic
// mode, so the feedback package's list must name every one of them.
func TestFeedbackToolCannotTakeTheGatewaysOwnToolNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{searchToolsToolName, listToolsToolName, describeToolsToolName, executeToolToolName} {
		require.Contains(t, mcpfeedback.ReservedToolNames, name)
	}
}

// An anonymous caller is held to its own rate limit by the address the edge
// resolved, which it cannot rotate the way it can its session. Without one,
// the session picks the bucket, hashed so the credential is never written
// to the limiter's store.
func TestFeedbackCallerLimitsAnAnonymousCallerByAddressThenSession(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", nil)
	request.Header.Set(proxy.McpSessionIDHeader, "anonymous-session-credential")

	withAddress := requestip.WithContext(t.Context(), requestip.Result{Addr: netip.MustParseAddr("203.0.113.7"), Source: requestip.SourceHeader})
	require.Equal(t, "address:203.0.113.7", feedbackCaller(withAddress, "org", request, false).LimitSubject)

	digest := sha256.Sum256([]byte("anonymous-session-credential"))
	bySession := feedbackCaller(t.Context(), "org", request, false).LimitSubject
	require.Equal(t, "session:"+hex.EncodeToString(digest[:]), bySession)
	require.NotContains(t, bySession, "anonymous-session-credential")

	// Each session gets its own bucket.
	other := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", nil)
	other.Header.Set(proxy.McpSessionIDHeader, "another-session-credential")
	require.NotEqual(t, bySession, feedbackCaller(t.Context(), "org", other, false).LimitSubject)
}
