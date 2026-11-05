package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/wire"
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
		Request:          nil,
	}, rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", upstreamAuth, nil)
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

// Through the real proxy: only a 502 the tunnel gateway answered is the tunnel's.
func TestToolsCallConnections_TunneledBadGateway(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		tunnelError string
		want        mcpconnectionattempts.Code
	}{
		{name: "server behind the tunnel", tunnelError: "", want: mcpconnectionattempts.CodeProviderServerError},
		{name: "tunnel down", tunnelError: wire.TunnelErrorNoLiveSession, want: mcpconnectionattempts.CodeTunnelUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			connections := recordingConnections(rec, "token")
			connections.tunneled = true
			serveToolsCall(t.Context(), t, connections, func(w http.ResponseWriter, _ *http.Request) {
				if tc.tunnelError != "" {
					w.Header().Set(tunnelrouting.ErrorHeader, tc.tunnelError)
				}
				w.WriteHeader(http.StatusBadGateway)
			}, continueCheckpoint())

			rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
		})
	}
}

// Through the real proxy: a server that never answers reads the same behind a tunnel as directly.
func TestToolsCallConnections_TunneledTimeout(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	connections := recordingConnections(rec, "token")
	connections.tunneled = true
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	p := newKillswitchTestProxy(t, server.URL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
	p.NonStreamingTimeout = 50 * time.Millisecond
	p.ToolsCallFailureInterceptors = connections.withFailures(nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	require.Error(t, p.Post(httptest.NewRecorder(), req))

	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnreachable)
}

// A secret an auth client adds goes through the tunnel; its routing headers are not credentials.
func TestToolsCallConnections_TunneledConfiguredCredential(t *testing.T) {
	t.Parallel()

	identity := proxy.ServerIdentity{RemoteMCPServerID: "", TunneledMCPServerID: uuid.NewString(), McpServerID: uuid.NewString(), MetaMCPServerID: ""}
	attribution := &ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
		Details:          nil,
		UpstreamFailures: true,
		SelfCredential:   false,
		Request:          nil,
	}
	routing := tunnelrouting.Headers(identity.TunneledMCPServerID, "forward-token", "affinity")
	reject := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, newToolsCallConnections(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", routing), reject, continueCheckpoint())
	rec.RequireNothing()

	refused := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(tunnelrouting.ErrorHeader, wire.TunnelErrorForwardRejected)
		w.WriteHeader(http.StatusForbidden)
	}
	secret := proxy.ConfiguredHeader{IsRequired: true, IsSecret: true, Name: "X-Api-Key", StaticValue: "key", ValueFromRequestHeader: ""}
	rec = mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, newToolsCallConnections(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", proxy.MergeAuthClientHeaders(proxy.HeaderPolicyTunneled, routing, []proxy.ConfiguredHeader{secret})), reject, continueCheckpoint())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Empty(t, row.ProviderHost)

	rec = mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, newToolsCallConnections(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", proxy.MergeAuthClientHeaders(proxy.HeaderPolicyTunneled, routing, []proxy.ConfiguredHeader{secret})), refused, continueCheckpoint())
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInternalError)
}

func TestUpstreamFailureHappened(t *testing.T) {
	t.Parallel()

	attached := UpstreamCredentialSent{Attached: true, Self: false, Configured: false}
	code, ok := UpstreamFailureHappened(http.StatusUnauthorized, UpstreamCredentialSent{Attached: true, Self: true, Configured: false}, false, "")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeSharedCredentialRejected, code)

	code, ok = UpstreamFailureHappened(http.StatusUnauthorized, UpstreamCredentialSent{Attached: false, Self: false, Configured: true}, false, "")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeStaticKeyRejected, code)

	_, ok = UpstreamFailureHappened(http.StatusUnauthorized, UpstreamCredentialSent{Attached: false, Self: false, Configured: false}, false, "")
	require.False(t, ok, "a 401 with no credential is the sign-in challenge")

	for tunnelError, want := range map[string]mcpconnectionattempts.Code{
		"":                                    mcpconnectionattempts.CodeProviderServerError,
		wire.TunnelErrorNoLiveSession:         mcpconnectionattempts.CodeTunnelUnavailable,
		wire.TunnelErrorSubstreamFailed:       mcpconnectionattempts.CodeTunnelUnavailable,
		wire.TunnelErrorNoRoute:               mcpconnectionattempts.CodeTunnelUnavailable,
		wire.TunnelErrorTunnelBusy:            mcpconnectionattempts.CodeTunnelAtCapacity,
		wire.TunnelErrorRouteLookupFailed:     mcpconnectionattempts.CodeInternalError,
		wire.TunnelErrorRouteStoreUnavailable: mcpconnectionattempts.CodeInternalError,
	} {
		code, _ = UpstreamFailureHappened(http.StatusBadGateway, attached, true, tunnelError)
		require.Equal(t, want, code, tunnelError)
	}
	code, _ = UpstreamFailureHappened(http.StatusBadGateway, attached, false, wire.TunnelErrorNoRoute)
	require.Equal(t, mcpconnectionattempts.CodeProviderServerError, code, "only a tunneled server has a tunnel")
	code, _ = UpstreamFailureHappened(0, attached, true, "")
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code)
	code, _ = UpstreamFailureHappened(0, attached, false, "")
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code)
	code, _ = UpstreamFailureHappened(0, attached, true, "", fmt.Errorf("forward: %w", proxy.ErrUpstreamTimedOut))
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code, "a slow server behind a tunnel is not ours")

	_, ok = UpstreamFailureHappened(http.StatusNotFound, attached, false, "")
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

// A rejected admin-set key is the admin's to fix; a header forwarded from the caller is not.
func TestToolsCallConnections_ConfiguredCredentialRejected(t *testing.T) {
	t.Parallel()

	attribution := func() *ConnectionAttribution {
		return &ConnectionAttribution{
			Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
			Details:          nil,
			UpstreamFailures: true,
			SelfCredential:   false,
			Request:          nil,
		}
	}

	for _, tc := range []struct {
		name   string
		header proxy.ConfiguredHeader
		admin  bool
	}{
		{"workspace", proxy.ConfiguredHeader{Name: "X-Workspace", StaticValue: "workspace"}, false},
		{"authorization", proxy.ConfiguredHeader{Name: "authorization", StaticValue: "Bearer key"}, true},
		{"secret API key", proxy.ConfiguredHeader{Name: "X-Api-Key", IsSecret: true, StaticValue: "key"}, true},
		{"custom secret", proxy.ConfiguredHeader{Name: "X-Custom", IsSecret: true, StaticValue: "key"}, true},
		{"empty secret", proxy.ConfiguredHeader{Name: "X-Api-Key", IsSecret: true}, false},
		{"forwarded authorization", proxy.ConfiguredHeader{Name: "Authorization", ValueFromRequestHeader: "X-Caller-Key"}, false},
		{"forwarded secret", proxy.ConfiguredHeader{Name: "X-Api-Key", IsSecret: true, ValueFromRequestHeader: "X-Caller-Key"}, false},
		{"forwarding wins over static", proxy.ConfiguredHeader{Name: "Authorization", IsSecret: true, StaticValue: "ignored", ValueFromRequestHeader: "X-Caller-Key"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			headers := []proxy.ConfiguredHeader{tc.header}
			require.Equal(t, tc.admin, HasConfiguredCredential(headers))
			for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
				rec := mcpconnectionattemptstest.New(t)
				reject := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
				serveToolsCall(t.Context(), t, newToolsCallConnections(attribution(), rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", "", headers), reject, continueCheckpoint())
				if tc.admin {
					row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
					require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
				} else {
					rec.RequireNothing()
				}
			}
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

// The row names the caller's own client, not the proxy's.
func TestToolsCallConnections_RowCarriesTheCallersClient(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	connections := recordingConnections(rec, "token")
	connections.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", nil)
	connections.Request.Header.Set("User-Agent", "claude-code/2.1.0")
	connections.record(t.Context(), mcpconnectionattempts.CodeProviderServerError)
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, rec.Rows()[0].ClientFamily)
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

func TestToolsCallConnections_PreSendFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, target string
		headers      []proxy.ConfiguredHeader
		want         mcpconnectionattempts.Code
	}{
		{name: "missing caller header", target: "https://provider.example.com/mcp", headers: []proxy.ConfiguredHeader{{Name: "X-Tenant", ValueFromRequestHeader: "X-Tenant", IsRequired: true}}, want: mcpconnectionattempts.CodeInvalidRequest},
		{name: "invalid stored destination", target: "https://provider.example.com/mcp", headers: []proxy.ConfiguredHeader{{Name: " bad", ValueFromRequestHeader: "X-Tenant", StaticValue: "", IsRequired: true}}, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "invalid stored source", target: "https://provider.example.com/mcp", headers: []proxy.ConfiguredHeader{{Name: "X-Tenant", ValueFromRequestHeader: " bad", StaticValue: "", IsRequired: true}}, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "reserved stored source", target: "https://provider.example.com/mcp", headers: []proxy.ConfiguredHeader{{Name: "X-Tenant", ValueFromRequestHeader: "Authorization", StaticValue: "", IsRequired: true}}, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "invalid target", target: "http://provider.example.com/mcp", want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "build failure", target: "://invalid", want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			connections := recordingConnections(rec, "token")
			p := newKillswitchTestProxy(t, tc.target, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "org", "server", testenv.NewLogger(t)))
			p.Identity.RemoteMCPServerID = "server"
			p.Headers = tc.headers
			p.ToolsCallFailureInterceptors = connections.withFailures(nil)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			err := p.Post(httptest.NewRecorder(), req)
			require.Error(t, err)
			rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
		})
	}
}
func TestToolsCallConnections_TunnelCapacityAndCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ header, reason, who string }{
		{wire.TunnelErrorTunnelBusy, "tunnel_at_capacity", "admin"},
		{wire.TunnelErrorActiveCheckFailed, "internal_error", "speakeasy"},
	} {
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			c := recordingConnections(rec, "token")
			c.tunneled = true
			serveToolsCall(t.Context(), t, c, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(tunnelrouting.ErrorHeader, tc.header)
				w.WriteHeader(http.StatusBadGateway)
			}, continueCheckpoint())
			rows := rec.Rows()
			require.Len(t, rows, 1)
			require.Equal(t, tc.reason, rows[0].Reason)
			require.Equal(t, tc.who, rows[0].WhoFixes)
		})
	}
}
