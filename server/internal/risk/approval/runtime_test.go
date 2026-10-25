package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
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
	require.Contains(t, text, "-H 'Authorization: Bearer "+token+"' 'https://speakeasy.test/approvals/watch?format=text'")
	require.Contains(t, text, "APPROVED")
	require.Contains(t, text, "The request expires at ")
	require.Contains(t, text, "keep this turn open")
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

func TestMonitorTimeoutCoversTheWindowUpToAnHour(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1_860_000, monitorTimeoutMs(30*time.Minute))
	require.Equal(t, 60_000, monitorTimeoutMs(0))
	require.Equal(t, 3_600_000, monitorTimeoutMs(24*time.Hour))
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

// postRequest posts an approval request to the request endpoint once.
func postRequest(t *testing.T, runtime *Runtime, authorization, body string) (int, string) {
	t.Helper()
	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleRequest))
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+RequestPath, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res.StatusCode, string(raw)
}

func TestHandleRequestFilesTheRequestWithOnlyTheToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	token := f.hold(t, f.call(`{}`))

	code, text := postRequest(t, runtime, "Bearer "+token, `{"reason":"File the bug the user reported."}`)
	require.Equal(t, http.StatusOK, code, text)
	require.Contains(t, text, "status: pending")
	require.Contains(t, text, "The request expires at ")
	require.Contains(t, text, "-H 'Authorization: Bearer "+token+"' 'https://speakeasy.test/approvals/watch?format=text'")
	require.Regexp(t, `Monitor tool \(description: Waiting for approval, timeout_ms: 18[0-9]{5}\)`, text)
	require.Contains(t, text, "keep this turn open")

	status, err := f.service.WatchStatus(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, StatusPending, status)

	// Sending it again changes nothing and returns the same steps.
	code, again := postRequest(t, runtime, "Bearer "+token, `{"reason":"Another reason."}`)
	require.Equal(t, http.StatusOK, code, again)
	require.Contains(t, again, "status: pending")
}

func TestHandleRequestRejectsMissingOrUnknownTokens(t *testing.T) {
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
			code, body := postRequest(t, runtime, authorization, `{"reason":"needed"}`)
			require.Equal(t, http.StatusUnauthorized, code)
			require.Contains(t, body, "apr_")
		})
	}
}

func TestHandleRequestValidatesTheBody(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	token := f.hold(t, f.call(`{}`))

	for name, tc := range map[string]struct{ body, want string }{
		"not json":    {body: `reason=needed`, want: "send a JSON body such as"},
		"no reason":   {body: `{}`, want: "reason is required"},
		"short wait":  {body: `{"reason":"needed","expires_in":5}`, want: "expires_in must be between 60 and 86400 seconds"},
		"huge wait":   {body: fmt.Sprintf(`{"reason":"needed","expires_in":%d}`, math.MaxInt64/2), want: "expires_in must be between"},
		"long reason": {body: `{"reason":"` + strings.Repeat("x", 501) + `"}`, want: "reason must be at most 500 characters"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, body := postRequest(t, runtime, "Bearer "+token, tc.body)
			require.Equal(t, http.StatusBadRequest, code, body)
			require.Contains(t, body, tc.want)
		})
	}
}

func TestHandleRequestRateLimitsPerToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	runtime := NewRuntime(f.service, nil, ratelimit.NewRedisStore(redisClient))
	token := f.hold(t, f.call(`{}`))

	limited := false
	for range 30 {
		code, body := postRequest(t, runtime, "Bearer "+token, `{"reason":"needed"}`)
		if code == http.StatusTooManyRequests {
			limited = true
			break
		}
		require.Equal(t, http.StatusOK, code, body)
	}
	require.True(t, limited, "a token used faster than the limit is told to back off")
}

// watchText polls the plain-text watch endpoint once.
func watchText(t *testing.T, runtime *Runtime, authorization string) (int, string) {
	t.Helper()
	server := httptest.NewServer(oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch))
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+WatchPath+"?format=text", nil)
	require.NoError(t, err)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res.StatusCode, string(raw)
}

func TestHandleWatchTextAnswersOneLine(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	line := func(token string) string {
		t.Helper()
		code, body := watchText(t, runtime, "Bearer "+token)
		require.Equal(t, http.StatusOK, code, body)
		return body
	}

	approved := f.hold(t, f.call(`{"title":"approve me"}`))
	require.True(t, strings.HasPrefix(line(approved), "BLOCKED: "))
	f.request(t, approved)
	require.Equal(t, "PENDING\n", line(approved))
	_, err := f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, approved), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line(approved), "APPROVED: "))

	denied := f.hold(t, f.call(`{"title":"deny me"}`))
	f.request(t, denied)
	_, err = f.service.Decide(t.Context(), DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, denied), Approve: false, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line(denied), "DENIED: "))

	unknown, err := NewToken()
	require.NoError(t, err)
	for _, authorization := range []string{"", "Bearer " + unknown} {
		code, body := watchText(t, runtime, authorization)
		require.Equal(t, http.StatusUnauthorized, code)
		require.True(t, strings.HasPrefix(body, "INVALID: "), body)
	}
}

func TestHandleWatchTextReportsExpiry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	runtime := NewRuntime(f.service, nil, nil)
	token := f.hold(t, f.call(`{}`))
	f.request(t, token)
	f.shiftClock(defaultRequestTTL + time.Minute)

	code, body := watchText(t, runtime, "Bearer "+token)
	require.Equal(t, http.StatusOK, code)
	require.True(t, strings.HasPrefix(body, "EXPIRED: "), body)
}

// commandLine is the first line of text that starts with prefix, without its
// indentation: a command the instructions tell the agent to run.
func commandLine(t *testing.T, text, prefix string) string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, prefix) {
			return trimmed
		}
	}
	require.FailNow(t, "no command in the instructions", "want a line starting with %q in:\n%s", prefix, text)
	return ""
}

func TestAgentRequestsAndWaitsWithTheCommandsAsWritten(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"bash", "curl"} {
		_, err := exec.LookPath(tool)
		require.NoError(t, err, "the commands need %s", tool)
	}
	f := newFixture(t)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	service := NewService(testenv.NewLogger(t), f.db, serverURL, audit.NewLogger())
	runtime := NewRuntime(service, nil, nil)
	var watches atomic.Int32
	watchHandler := oops.ErrHandle(testenv.NewLogger(t), runtime.HandleWatch)
	mux.Handle("POST "+RequestPath, oops.ErrHandle(testenv.NewLogger(t), runtime.HandleRequest))
	mux.Handle("GET "+WatchPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		watchHandler.ServeHTTP(w, r)
		watches.Add(1)
	}))
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	// The block message's curl command files the request as written.
	outcome, err := service.HoldCall(ctx, f.call(`{"title":"it's quoted"}`))
	require.NoError(t, err)
	requested, err := exec.CommandContext(ctx, "bash", "-c", commandLine(t, outcome.UserMessage, "curl ")).CombinedOutput()
	require.NoError(t, err, string(requested))
	require.Contains(t, string(requested), "Approval requested (status: pending)")

	// Its wait loop prints nothing while the request is pending, then one line.
	var out bytes.Buffer
	wait := exec.CommandContext(ctx, "bash", "-c", commandLine(t, string(requested), "while :;"))
	wait.Stdout, wait.Stderr = &out, &out
	require.NoError(t, wait.Start())
	require.Eventually(t, func() bool { return watches.Load() > 0 }, 10*time.Second, 50*time.Millisecond, "the loop polls while the request is pending")
	_, err = service.Decide(ctx, DecideInput{OrganizationID: f.organizationID, ProjectID: f.projectID, ApprovalID: f.approvalID(t, outcome.Token), Approve: true, DeciderUserID: f.approverID, Via: DecidedViaDashboard, Note: ""})
	require.NoError(t, err)
	require.NoError(t, wait.Wait(), out.String())
	require.Equal(t, "APPROVED: retry the blocked tool call now with exactly the same arguments\n", out.String())
}

func TestWaitCommandKeepsWaitingThroughErrors(t *testing.T) {
	t.Parallel()
	// Errors, rate limits and a proxy's error page never end the wait.
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch polls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html>APPROVED by nobody: bad gateway</html>")
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, "WAIT: too many approval status checks\n")
		default:
			_, _ = io.WriteString(w, "DENIED: an approver denied this request; do not retry; tell the user\n")
		}
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	token, err := NewToken()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	command := NewService(testenv.NewLogger(t), nil, serverURL, nil).waitCommand(token)
	out, err := exec.CommandContext(ctx, "bash", "-c", command).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "DENIED: an approver denied this request; do not retry; tell the user\n", string(out))
	require.EqualValues(t, 3, polls.Load())
}
