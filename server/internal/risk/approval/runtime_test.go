package approval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// watchResult is one poll of the watch endpoint.
type watchResult struct {
	status       int
	cacheControl string
	body         watchResponse
}

// watch polls the watch endpoint once and decodes a successful body.
func watch(t *testing.T, runtime *Runtime, authorization string) watchResult {
	t.Helper()
	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch))
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+WatchPath, nil)
	require.NoError(t, err)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	result := watchResult{status: res.StatusCode, cacheControl: res.Header.Get("Cache-Control"), body: watchResponse{Status: ""}}
	if res.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(res.Body).Decode(&result.body))
	}
	return result
}

func TestHandleWatchReportsStatusForToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	token := f.hold(t, f.call(`{}`))

	result := watch(t, runtime, "Bearer "+token)
	require.Equal(t, http.StatusOK, result.status)
	require.Equal(t, "no-store", result.cacheControl)
	require.Equal(t, string(StatusBlocked), result.body.Status)

	f.request(t, token)
	require.Equal(t, string(StatusPending), watch(t, runtime, "bearer "+token).body.Status)
}

func TestHandleWatchRejectsMissingOrUnknownTokens(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	unknown, err := NewToken()
	require.NoError(t, err)

	for name, authorization := range map[string]string{
		"missing":    "",
		"not bearer": "Basic " + unknown,
		"malformed":  "Bearer not-a-token",
		"unknown":    "Bearer " + unknown,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, http.StatusUnauthorized, watch(t, runtime, authorization).status)
		})
	}
}

// callRequestApproval calls request_approval over the Streamable HTTP handler
// as the given identity.
func callRequestApproval(t *testing.T, runtime *Runtime, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: server.Client(), MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "request_approval", Arguments: arguments})
	require.NoError(t, err)
	return res
}

func agentAuthenticator(organizationID string, agentID uuid.UUID) Authenticator {
	return func(_ http.ResponseWriter, r *http.Request) (context.Context, error) {
		ctx := mcpidentity.NewValidatorBoundary().StampAgent(r.Context(), agentID)
		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: organizationID}), nil
	}
}

func TestRequestApprovalToolFilesRequestAndReturnsWatcher(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	agentID := uuid.New()
	f.agentURN = urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String()
	token := f.hold(t, f.call(`{}`))
	runtime := NewRuntime(f.service, agentAuthenticator(f.organizationID, agentID), nil)

	res := callRequestApproval(t, runtime, map[string]any{"token": token, "reason": "File the bug the user reported."})
	require.False(t, res.IsError)
	content, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	text := content.Text
	require.Contains(t, text, "status: pending")
	require.Contains(t, text, "https://speakeasy.test/approvals/watch")
	require.Contains(t, text, "APPROVED")
	require.Contains(t, text, watchScript)
	out, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var structured requestApprovalOutput
	require.NoError(t, json.Unmarshal(out, &structured))
	require.Equal(t, string(StatusPending), structured.Status)
	require.Equal(t, "https://speakeasy.test/approvals/watch", structured.WatchURL)
	require.NotEmpty(t, structured.ExpiresAt)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusPending, status)
}

func TestRequestApprovalToolRejectsOtherCallers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	token := f.hold(t, f.call(`{}`))

	// A different agent cannot file a request for this agent's call.
	runtime := NewRuntime(f.service, agentAuthenticator(f.organizationID, uuid.New()), nil)
	res := callRequestApproval(t, runtime, map[string]any{"token": token, "reason": "needed"})
	require.True(t, res.IsError)
	content, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "no approval request matches")

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusBlocked, status)
}

func TestRequestApprovalHandlerRequiresAuthentication(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, func(http.ResponseWriter, *http.Request) (context.Context, error) {
		return nil, errors.New("no credential")
	}, nil)
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	get, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	getRes, err := server.Client().Do(get)
	require.NoError(t, err)
	defer func() { _ = getRes.Body.Close() }()
	require.Equal(t, http.StatusMethodNotAllowed, getRes.StatusCode)
}
