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

func serveToolsCall(ctx context.Context, t *testing.T, connections *toolsCallConnections, upstream http.HandlerFunc, checkpoint ToolsCallKillswitchCheckpoint) {
	t.Helper()

	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	p := newKillswitchTestProxy(t, server.URL, NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)))
	p.ToolsCallFailureInterceptors = connections.withFailures(nil)
	p.ToolsCallRefusalInterceptors = connections.refusals()

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
}

func recordingConnections(rec *mcpconnectionattemptstest.Recording, upstreamAuth string) *toolsCallConnections {
	return newToolsCallConnections(&ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
		Details:          nil,
		UpstreamFailures: true,
		SelfCredential:   false,
	}, rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", upstreamAuth)
}

func continueCheckpoint() ToolsCallKillswitchCheckpoint {
	return &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()}
}

// A healthy tool call must write nothing: at tool-call rates, success rows crowd out failures.
func TestToolsCallConnections_HealthyCallRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`))
	}, continueCheckpoint())
	rec.RequireNothing()
}

// A tool's own error is the tool's answer, not a connection failure.
func TestToolsCallConnections_ToolErrorRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{"isError":true,"content":[]}}`))
	}, continueCheckpoint())
	rec.RequireNothing()
}

func TestToolsCallConnections_UpstreamFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		want   mcpconnectionattempts.Code
		who    mcpconnectionattempts.WhoFixes
	}{
		{name: "provider rejects", status: http.StatusUnauthorized, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "provider forbids", status: http.StatusForbidden, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "rate limited", status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider},
		{name: "server error", status: http.StatusBadGateway, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}, continueCheckpoint())

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, "provider.example.com", row.ProviderHost)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

func TestUpstreamFailureHappened(t *testing.T) {
	t.Parallel()

	attached := UpstreamCredential{Attached: true, Self: false}
	code, ok := UpstreamFailureHappened(http.StatusUnauthorized, UpstreamCredential{Attached: true, Self: true}, false)
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, code)

	_, ok = UpstreamFailureHappened(http.StatusUnauthorized, UpstreamCredential{Attached: false, Self: false}, false)
	require.False(t, ok, "a 401 with no credential is the sign-in challenge")

	code, _ = UpstreamFailureHappened(http.StatusBadGateway, attached, true)
	require.Equal(t, mcpconnectionattempts.CodeTunnelUnavailable, code)
	code, _ = UpstreamFailureHappened(0, attached, true)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code)
	code, _ = UpstreamFailureHappened(0, attached, false)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code)

	_, ok = UpstreamFailureHappened(http.StatusNotFound, attached, false)
	require.False(t, ok)
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
			serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(http.ResponseWriter, *http.Request) {}, &fakeKillswitchCheckpoint{disposition: tc.disposition})
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
		})
	}
}

// A caller that went away says nothing about the server.
func TestToolsCallConnections_CanceledCallerRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recordingConnections(rec, "token").record(ctx, mcpconnectionattempts.CodeProviderServerError)
	rec.RequireNothing()
}

func TestRefusalHappened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want mcpconnectionattempts.Code
	}{
		{name: "tool denied", err: mcpaccess.ToolPermissionDenied(oops.C(oops.CodeForbidden)), want: mcpconnectionattempts.CodeToolPermissionDenied},
		{name: "usage limit", err: oops.E(oops.CodeForbidden, ErrToolUsageLimitReached, "tool usage limit reached"), want: mcpconnectionattempts.CodeUsageLimitReached},
		{name: "risk policy", err: &proxy.RejectError{Code: proxy.RejectCodeForbidden, Message: "blocked", Data: nil}, want: mcpconnectionattempts.CodePolicyBlocked},
		{name: "not approved", err: &proxy.RejectError{Code: proxy.RejectCodeInvalidRequest, Message: ToolNotApprovedMessage, Data: nil}, want: mcpconnectionattempts.CodeToolNotApproved},
		{name: "paused", err: proxy.NewKillswitchMatchRejection("paused"), want: mcpconnectionattempts.CodeMCPToolCallsPaused},
		{name: "check failed", err: killswitchCheckError{proxy.NewKillswitchInfrastructureRejection()}, want: mcpconnectionattempts.CodeKillswitchCheckUnavailable},
		{name: "a failed lookup is ours", err: errors.New("resolve remote MCP tool disposition: lookup failed"), want: mcpconnectionattempts.CodeInternalError},
		{name: "a malformed call is ours", err: &proxy.RejectError{Code: proxy.RejectCodeInvalidParams, Message: "malformed", Data: nil}, want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, ok := refusalHappened(tc.err)
			require.True(t, ok)
			require.Equal(t, tc.want, code)
		})
	}

	_, ok := refusalHappened(&proxy.ToolResultError{Message: "needs approval"})
	require.False(t, ok, "a call held for approval is not an end state")
}
