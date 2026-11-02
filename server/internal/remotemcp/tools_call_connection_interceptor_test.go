package remotemcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const connectionToolsCall = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected","arguments":{}}}`

func serveToolsCall(t *testing.T, connections *toolsCallConnections, upstream http.HandlerFunc, checkpoint ToolsCallKillswitchCheckpoint) *httptest.ResponseRecorder {
	t.Helper()

	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	p := newKillswitchTestProxy(t, server.URL, connections.watch(NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)), killswitchRefusal))
	p.ToolsCallFailureInterceptors = connections.withFailures(nil)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	return rr
}

func newToolsCallConnections(rec *mcpconnectionattemptstest.Recording) *toolsCallConnections {
	return &toolsCallConnections{
		recorder:       rec.Recorder,
		scope:          mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
		details:        nil,
		recordFailures: true,
		selfCredential: false,
		tunneled:       false,
		upstreamAuth:   true,
		provider:       "https://provider.example.com/mcp",
	}
}

// A healthy tool call must write nothing: at tool-call rates, success rows crowd out failures.
func TestToolsCallConnections_HealthyCallRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t, newToolsCallConnections(rec), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`))
	}, &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()})
	rec.RequireNothing()
}

// A tool's own error is the tool's answer, not a connection failure.
func TestToolsCallConnections_ToolErrorRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t, newToolsCallConnections(rec), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{"isError":true,"content":[]}}`))
	}, &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()})
	rec.RequireNothing()
}

func TestToolsCallConnections_UpstreamFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		status    int
		configure func(*toolsCallConnections)
		want      mcpconnectionattempts.Code
		who       mcpconnectionattempts.WhoFixes
	}{
		{name: "provider rejects the person's credential", status: http.StatusUnauthorized, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "provider forbids the person's credential", status: http.StatusForbidden, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "provider rejects the server's own credential", status: http.StatusUnauthorized, configure: func(c *toolsCallConnections) { c.selfCredential = true }, want: mcpconnectionattempts.CodeCredentialMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "rate limited", status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider},
		{name: "server error", status: http.StatusBadGateway, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
		{name: "tunnel down", status: http.StatusBadGateway, configure: func(c *toolsCallConnections) { c.tunneled = true }, want: mcpconnectionattempts.CodeTunnelUnavailable, who: mcpconnectionattempts.WhoAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			connections := newToolsCallConnections(rec)
			if tc.configure != nil {
				tc.configure(connections)
			}
			serveToolsCall(t, connections, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}, &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()})

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// Without a credential Speakeasy attached, a 401 is the upstream asking the caller to sign in.
func TestToolsCallConnections_ChallengeWithoutCredentialRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	connections := newToolsCallConnections(rec)
	connections.upstreamAuth = false
	serveToolsCall(t, connections, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()})
	rec.RequireNothing()
}

func TestToolsCallConnections_FailureHappenedOnUnanswered(t *testing.T) {
	t.Parallel()

	connections := &toolsCallConnections{}
	code, ok := connections.failureHappened(&proxy.ToolsCallFailure{Request: nil, StatusCode: 0, Err: errors.New("dial failed")})
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code)

	connections.tunneled = true
	code, ok = connections.failureHappened(&proxy.ToolsCallFailure{Request: nil, StatusCode: 0, Err: errors.New("dial failed")})
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code)
}

func TestToolsCallConnections_KillswitchRefusals(t *testing.T) {
	t.Parallel()

	match, err := killswitches.NewMatchedDenialDisposition("Calls paused by an administrator.")
	require.NoError(t, err)
	for _, tc := range []struct {
		name        string
		disposition killswitches.TransportDisposition
		want        mcpconnectionattempts.Code
		who         mcpconnectionattempts.WhoFixes
	}{
		{name: "paused", disposition: match, want: mcpconnectionattempts.CodeMCPToolCallsPaused, who: mcpconnectionattempts.WhoAdmin},
		{name: "check failed", disposition: killswitches.NewInfrastructureRejectionDisposition(), want: mcpconnectionattempts.CodeKillswitchCheckUnavailable, who: mcpconnectionattempts.WhoSpeakeasy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveToolsCall(t, newToolsCallConnections(rec), func(http.ResponseWriter, *http.Request) {}, &fakeKillswitchCheckpoint{disposition: tc.disposition})
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
		})
	}
}

type failingToolsCallInterceptor struct{ err error }

func (i *failingToolsCallInterceptor) Name() string { return "failing" }

func (i *failingToolsCallInterceptor) InterceptToolsCallRequest(context.Context, *proxy.ToolsCallRequest) error {
	return i.err
}

func TestToolsCallConnections_WatchKeepsTheError(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	refused := &proxy.RejectError{Code: proxy.RejectCodeInvalidRequest, Message: ToolNotApprovedMessage, Data: nil}
	watched := newToolsCallConnections(rec).watch(&failingToolsCallInterceptor{err: refused}, toolNotApprovedRefusal)
	require.Same(t, refused, watched.InterceptToolsCallRequest(t.Context(), nil))
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeToolNotApproved)
}

func TestToolsCallRefusals(t *testing.T) {
	t.Parallel()

	code, ok := toolPermissionRefusal(mcpaccess.ToolPermissionDenied(oops.C(oops.CodeForbidden)))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeToolPermissionDenied, code)

	_, ok = toolPermissionRefusal(errors.New("resolve remote MCP tool disposition: lookup failed"))
	require.False(t, ok, "a failed lookup is not a denial")

	code, ok = riskPolicyRefusal(&proxy.RejectError{Code: proxy.RejectCodeForbidden, Message: "blocked", Data: nil})
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodePolicyBlocked, code)

	_, ok = riskPolicyRefusal(&proxy.ToolResultError{Message: "needs approval"})
	require.False(t, ok, "a call held for approval is not an end state")

	code, ok = usageLimitRefusal(errors.New("tool usage limit reached"))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeUsageLimitReached, code)
}
