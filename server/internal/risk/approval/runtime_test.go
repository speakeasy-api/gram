package approval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// watchResult is one poll of the watch endpoint.
type watchResult struct {
	status       int
	cacheControl string
	body         watchResponse
	raw          string
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
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	result := watchResult{status: res.StatusCode, cacheControl: res.Header.Get("Cache-Control"), body: watchResponse{Status: ""}, raw: string(raw)}
	if res.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &result.body))
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
			result := watch(t, runtime, authorization)
			require.Equal(t, http.StatusUnauthorized, result.status)
			// An agent that guessed how to call the endpoint learns where the
			// token goes.
			require.Contains(t, result.raw, `Authorization: Bearer apr_...`)
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
	require.Contains(t, text, "The request expires at ")
	require.Contains(t, text, "In Claude Code, call the Monitor tool with:")
	require.Contains(t, text, `command: bash approval-watch.sh https://speakeasy.test/approvals/watch "$HOME/.speakeasy-approval-`)
	require.Contains(t, text, "timeout_ms: ")
	require.Contains(t, text, `"Authorization: Bearer apr_..." header`)
	require.Contains(t, text, "keep this turn open")
	require.Contains(t, text, "not $HOME or ~")
	require.Contains(t, text, `rm -f "$HOME/.speakeasy-approval-`)
	// The steps are only in the text. Structured content would replace them
	// in clients that show it to the model instead.
	require.Nil(t, res.StructuredContent)

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusPending, status)
}

func TestRequestApprovalToolHasNoOutputSchema(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, agentAuthenticator(f.organizationID, uuid.New()), nil)
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: server.Client(), MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, "request_approval", tools.Tools[0].Name)
	// With an output schema, Claude Code shows the structured result in place
	// of the text, and the agent never sees the watcher steps.
	require.Nil(t, tools.Tools[0].OutputSchema)
}

func TestMonitorTimeoutCoversTheWatcherUpToAnHour(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2_540_000, monitorTimeoutMs(620))
	require.Equal(t, 3_600_000, monitorTimeoutMs(30_000))
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

func TestHandleWatchRateLimitsPerToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	runtime := NewRuntime(f.service, nil, ratelimit.NewRedisStore(redisClient))
	token := f.hold(t, f.call(`{}`))

	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch))
	t.Cleanup(server.Close)
	limited := false
	for range 70 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := server.Client().Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests {
			require.NotEmpty(t, res.Header.Get("Retry-After"))
			limited = true
			break
		}
		require.Equal(t, http.StatusOK, res.StatusCode)
	}
	require.True(t, limited, "a token polled faster than the limit is told to back off")
}

func TestRequestApprovalToolRejectsOutOfRangeExpiry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	agentID := uuid.New()
	f.agentURN = urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String()
	token := f.hold(t, f.call(`{}`))
	runtime := NewRuntime(f.service, agentAuthenticator(f.organizationID, agentID), nil)

	res := callRequestApproval(t, runtime, map[string]any{"token": token, "reason": "needed", "expires_in": math.MaxInt64 / 2})
	require.True(t, res.IsError)
	content, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "expires_in must be between")
}

// runWatcher runs the shipped watcher script once against url.
func runWatcher(t *testing.T, url, tokenFile string) (string, int) {
	t.Helper()
	for _, tool := range []string{"bash", "curl"} {
		_, err := exec.LookPath(tool)
		require.NoError(t, err, "the watcher script needs %s", tool)
	}
	script := filepath.Join(t.TempDir(), "approval-watch.sh")
	require.NoError(t, os.WriteFile(script, []byte(watchScript), 0o600))
	cmd := exec.CommandContext(t.Context(), "bash", script, url, tokenFile, "1", "5")
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		exitCode = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return string(out), exitCode
}

func TestWatchScriptReportsTheDecision(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch))
	t.Cleanup(server.Close)
	token := f.hold(t, f.call(`{}`))
	f.request(t, token)
	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token+"\n"), 0o600))
	out, code := runWatcher(t, server.URL, tokenFile)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "APPROVED: retry the blocked tool call")
}

func TestWatchScriptStopsOnBadInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch))
	t.Cleanup(server.Close)
	unknown, err := NewToken()
	require.NoError(t, err)
	unknownFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(unknownFile, []byte(unknown), 0o600))

	out, code := runWatcher(t, server.URL, unknownFile)
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "WATCH FAILED: the server rejected the request token (HTTP 401)")

	out, code = runWatcher(t, server.URL, filepath.Join(t.TempDir(), "missing"))
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "cannot read a token")

	out, code = runWatcher(t, "http://approvals.example.test/approvals/watch", unknownFile)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "non-HTTPS URL")

	// A local-looking prefix with credentials would send the token elsewhere.
	out, code = runWatcher(t, "http://localhost:80@approvals.example.test/approvals/watch", unknownFile)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "credentials")
}

func TestWatchScriptIgnoresErrorResponses(t *testing.T) {
	t.Parallel()
	// An error response must never read as a decision, whatever its body says.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"approved"}`))
	}))
	t.Cleanup(server.Close)
	token, err := NewToken()
	require.NoError(t, err)
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token), 0o600))

	out, code := runWatcher(t, server.URL, tokenFile)
	require.Equal(t, 1, code, out)
	require.NotContains(t, out, "APPROVED")
	require.Contains(t, out, "WATCH TIMEOUT")
}
