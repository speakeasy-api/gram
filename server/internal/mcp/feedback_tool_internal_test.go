package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/requestip"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// unreachableDatabase is a pool whose every query fails, because nothing
// listens on port 1.
const unreachableDatabase = "postgres://feedback@127.0.0.1:1/feedback?connect_timeout=1"

// A server whose serving query says the tool is off is offered nothing
// without its settings being read: the same recorder fails when it has to
// read them.
func TestFeedbackLocalToolsReadNoSettingsForAServerWithTheToolOff(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(t.Context(), unreachableDatabase)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), pool, chatredact.New(), mcpfeedback.Limits{Caller: nil, Server: nil}, "gram-server", nil, mcpfeedback.WithSettingsFreshFor(0))
	source := feedbackLocalTools(recorder)
	server := localToolServer{organizationID: "org", projectID: uuid.New(), mcpServerID: uuid.New(), feedbackToolEnabled: false}

	tools, err := source(t.Context(), server)
	require.NoError(t, err)
	require.Empty(t, tools)

	server.feedbackToolEnabled = true
	_, err = source(t.Context(), server)
	require.Error(t, err)
}

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

// A local tool that answers with no result is an unexpected error, not a panic.
func TestHostedLocalToolWithNoResultIsAnError(t *testing.T) {
	t.Parallel()

	tool := localTool{
		definition: &sdkmcp.Tool{Name: "quiet"},
		call: func(context.Context, localToolCall) (*sdkmcp.CallToolResult, error) {
			return nil, nil
		},
	}
	_, err := answerHostedLocalToolCall(t.Context(), testenv.NewLogger(t), nil, &mcpInputs{}, mcpjsonrpc.ID{Number: 1}, tool, json.RawMessage(`{}`), &mcprequests.SanitizedClientInfo{Name: "client", Version: "1"})
	require.ErrorContains(t, err, "answer local tool call")
}
