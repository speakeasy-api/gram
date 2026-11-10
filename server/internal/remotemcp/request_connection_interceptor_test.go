package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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

func serveToolsCall(ctx context.Context, t *testing.T, connections connectionsUnderTest, upstream http.HandlerFunc, checkpoint ToolsCallKillswitchCheckpoint) {
	t.Helper()

	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	p := newKillswitchTestProxy(t, server.URL, NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)))
	connections.install(p)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
}

func recordingConnections(rec *mcpconnectionattemptstest.Recording, upstreamAuth string) connectionsUnderTest {
	return newConnectionsUnderTest(&ConnectionAttribution{
		Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
		Details:          nil,
		UpstreamFailures: true,
		SelfCredential:   false,
		Request:          nil,
	}, rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", upstreamAuth, nil)
}

// connectionsUnderTest is a proxy's connection interceptor with the credentials that proxy sends.
type connectionsUnderTest struct {
	*requestConnections

	upstreamAuth string
	headers      []proxy.ConfiguredHeader
}

func newConnectionsUnderTest(attribution *ConnectionAttribution, recorder *mcpconnectionattempts.Recorder, identity proxy.ServerIdentity, upstreamURL, upstreamAuth string, headers []proxy.ConfiguredHeader) connectionsUnderTest {
	return connectionsUnderTest{requestConnections: newRequestConnections(attribution, recorder, identity, upstreamURL, upstreamAuth, headers), upstreamAuth: upstreamAuth, headers: headers}
}

// install has p send the credentials the interceptor was built for, and adds the interceptor.
func (c connectionsUnderTest) install(p *proxy.Proxy) {
	p.AuthorizationOverride = c.upstreamAuth
	p.Headers = c.headers
	if c.tunneled {
		p.HeaderPolicy = proxy.HeaderPolicyTunneled
	}
	p.ToolsCallFailureInterceptors = c.withFailures(nil)
	p.ToolsCallRefusalInterceptors = c.refusals()
	p.RequestFailureInterceptors = c.requestFailures()
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
		name      string
		status    int
		challenge string
		want      mcpconnectionattempts.Code
		who       mcpconnectionattempts.WhoFixes
	}{
		{name: "provider rejects", status: http.StatusUnauthorized, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "provider forbids an invalid token", status: http.StatusForbidden, challenge: `Bearer error="invalid_token"`, want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson},
		{name: "rate limited", status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider},
		{name: "server error", status: http.StatusBadGateway, want: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(w http.ResponseWriter, _ *http.Request) {
				if tc.challenge != "" {
					w.Header().Set("WWW-Authenticate", tc.challenge)
				}
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
	serveToolsCall(t.Context(), t, newConnectionsUnderTest(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", routing), reject, continueCheckpoint())
	rec.RequireNothing()

	refused := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(tunnelrouting.ErrorHeader, wire.TunnelErrorForwardRejected)
		w.WriteHeader(http.StatusForbidden)
	}
	secret := proxy.ConfiguredHeader{IsRequired: true, IsSecret: true, Name: "X-Api-Key", StaticValue: "key", ValueFromRequestHeader: ""}
	rec = mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, newConnectionsUnderTest(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", proxy.MergeAuthClientHeaders(proxy.HeaderPolicyTunneled, routing, []proxy.ConfiguredHeader{secret})), reject, continueCheckpoint())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Empty(t, row.ProviderHost)

	rec = mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, newConnectionsUnderTest(attribution, rec.Recorder, identity, "https://gateway.internal/mcp", "", proxy.MergeAuthClientHeaders(proxy.HeaderPolicyTunneled, routing, []proxy.ConfiguredHeader{secret})), refused, continueCheckpoint())
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInternalError)
}

func TestUpstreamFailureHappened(t *testing.T) {
	t.Parallel()

	attached := UpstreamCredentialSent{Attached: true, Self: false, Configured: false}
	code, ok := UpstreamFailureHappened(http.StatusUnauthorized, true, UpstreamCredentialSent{Attached: true, Self: true, Configured: false}, false, "")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeSharedCredentialRejected, code)

	code, ok = UpstreamFailureHappened(http.StatusUnauthorized, true, UpstreamCredentialSent{Attached: false, Self: false, Configured: true}, false, "")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeStaticKeyRejected, code)

	_, ok = UpstreamFailureHappened(http.StatusUnauthorized, true, UpstreamCredentialSent{Attached: false, Self: false, Configured: false}, false, "")
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
		code, _ = UpstreamFailureHappened(http.StatusBadGateway, true, attached, true, tunnelError)
		require.Equal(t, want, code, tunnelError)
	}
	code, _ = UpstreamFailureHappened(http.StatusBadGateway, true, attached, false, wire.TunnelErrorNoRoute)
	require.Equal(t, mcpconnectionattempts.CodeProviderServerError, code, "only a tunneled server has a tunnel")
	code, _ = UpstreamFailureHappened(0, true, attached, true, "")
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code)
	code, _ = UpstreamFailureHappened(0, true, attached, false, "")
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code)
	code, _ = UpstreamFailureHappened(0, true, attached, true, "", fmt.Errorf("forward: %w", proxy.ErrUpstreamTimedOut))
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code, "a slow server behind a tunnel is not ours")

	_, ok = UpstreamFailureHappened(http.StatusNotFound, true, attached, false, "")
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
				reject := func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
					w.WriteHeader(status)
				}
				serveToolsCall(t.Context(), t, newConnectionsUnderTest(attribution(), rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", "", headers), reject, continueCheckpoint())
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

// unknownToolCall serves connectionToolsCall through the catalog-backed authz
// interceptor; the upstream lists only "other", so "protected" is unknown.
func unknownToolCall(ctx context.Context, t *testing.T, rec *mcpconnectionattemptstest.Recording, store *fakeToolCatalogStore, catalog *toolCatalogSync) {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("an unknown tool must not reach the upstream")
	}))
	t.Cleanup(upstream.Close)

	p := newKillswitchTestProxy(t, upstream.URL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), catalogTestOrgID, catalogTestServerID, testenv.NewLogger(t)), newAuthzInterceptor(t, store, catalog))
	p.ToolsCallRefusalInterceptors = recordingConnections(rec, "token").refusals()

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Contains(t, rr.Body.String(), errToolNotOnUpstream.Message)
}

func newUnknownToolCatalog(t *testing.T) (*fakeToolCatalogStore, *toolCatalogSync, *atomic.Int64, *sdkmetric.ManualReader) {
	t.Helper()

	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	var listings atomic.Int64
	catalog := newCatalogSync(store, func(context.Context, *http.Request) ([]*mcp.Tool, error) {
		listings.Add(1)
		return []*mcp.Tool{{Name: "other", InputSchema: map[string]any{}}}, nil
	})
	reader := sdkmetric.NewManualReader()
	catalog.metrics = NewProxyMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))
	now := time.Unix(1_700_000_000, 0)
	catalog.refreshes.now = func() time.Time { return now }
	return store, catalog, &listings, reader
}

func lookupSkippedCount(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == instrumentToolLookupSkipped {
				for _, point := range sum.DataPoints {
					total += point.Value
				}
			}
		}
	}
	return total
}

func TestToolsCallConnections_UnknownToolRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	store, catalog, listings, reader := newUnknownToolCatalog(t)
	ctx := blockDestructiveContext(t)

	unknownToolCall(ctx, t, rec, store, catalog)
	unknownToolCall(ctx, t, rec, store, catalog)

	require.EqualValues(t, 1, listings.Load(), "the repeat falls inside the caller's own cooldown")
	rec.RequireNothing()
	require.Zero(t, lookupSkippedCount(t, reader), "the caller's own listing confirmed the tool is absent")
}

func TestToolsCallConnections_SkippedToolLookupIsCountedNotRecorded(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	store, catalog, listings, reader := newUnknownToolCatalog(t)
	ctx := blockDestructiveContext(t)
	// Another key on the same stripe started the cooldown.
	catalog.refreshes.stripe(toolCatalogRefreshKey(ctx, catalogTestServerID)).Store(&toolCatalogRefreshClaim{key: "another caller", started: catalog.refreshes.now().UnixNano()})

	unknownToolCall(ctx, t, rec, store, catalog)

	require.Zero(t, listings.Load())
	rec.RequireNothing()
	require.EqualValues(t, 1, lookupSkippedCount(t, reader))
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

const (
	connectionInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`
	connectionToolsList  = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	connectionRead       = `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"file:///notes.txt"}}`
	connectionNotice     = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
)

// serveRequest posts body through a proxy carrying every connection interceptor and returns what the client got.
func serveRequest(t *testing.T, connections connectionsUnderTest, upstreamURL, body string) *httptest.ResponseRecorder {
	t.Helper()

	p := newKillswitchTestProxy(t, upstreamURL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
	connections.install(p)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	return rr
}

func rejectingUpstream(t *testing.T, status int) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// A provider rejecting the credential at connect time is recorded like a rejected tool call.
func TestRequestConnections_CredentialRejected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		body         string
		status       int
		upstreamAuth string
		self         bool
		headers      []proxy.ConfiguredHeader
		want         mcpconnectionattempts.Code
		who          mcpconnectionattempts.WhoFixes
		method       string
	}{
		{name: "initialize 401", body: connectionInitialize, status: http.StatusUnauthorized, upstreamAuth: "token", want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson, method: "initialize"},
		{name: "tools/list 401", body: connectionToolsList, status: http.StatusUnauthorized, upstreamAuth: "token", want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson, method: "tools_list"},
		{name: "tools/list 403", body: connectionToolsList, status: http.StatusForbidden, upstreamAuth: "token", want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson, method: "tools_list"},
		{name: "resources/read 401", body: connectionRead, status: http.StatusUnauthorized, upstreamAuth: "token", want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson, method: "resources_read"},
		{name: "notification 401", body: connectionNotice, status: http.StatusUnauthorized, upstreamAuth: "token", want: mcpconnectionattempts.CodeProviderRejected, who: mcpconnectionattempts.WhoPerson, method: "notification"},
		{name: "shared credential", body: connectionInitialize, status: http.StatusUnauthorized, upstreamAuth: "token", self: true, want: mcpconnectionattempts.CodeSharedCredentialRejected, who: mcpconnectionattempts.WhoAdmin, method: "initialize"},
		{name: "static key", body: connectionToolsList, status: http.StatusUnauthorized, headers: []proxy.ConfiguredHeader{{Name: "X-Api-Key", IsSecret: true, StaticValue: "key"}}, want: mcpconnectionattempts.CodeStaticKeyRejected, who: mcpconnectionattempts.WhoAdmin, method: "tools_list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			connections := newConnectionsUnderTest(&ConnectionAttribution{
				Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
				Details:          nil,
				UpstreamFailures: true,
				SelfCredential:   tc.self,
				Request:          nil,
			}, rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", tc.upstreamAuth, tc.headers)

			rr := serveRequest(t, connections, rejectingUpstream(t, tc.status), tc.body)
			require.Equal(t, tc.status, rr.Code, "the client gets the upstream's answer")

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, tc.method, row.SubReason, "the row says which request failed")
			require.Equal(t, "provider.example.com", row.ProviderHost)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// A failed tool call carries no method label, so health views can tell it from a failed handshake.
func TestRequestConnections_ToolCallHasNoMethodLabel(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, continueCheckpoint())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
	require.Empty(t, row.SubReason)
	require.Len(t, rec.Rows(), 1)
}

// Through a tunnel, the server's 401 is a rejected credential and the tunnel gateway's own refusal is ours.
func TestRequestConnections_Tunneled(t *testing.T) {
	t.Parallel()

	identity := proxy.ServerIdentity{RemoteMCPServerID: "", TunneledMCPServerID: uuid.NewString(), McpServerID: uuid.NewString(), MetaMCPServerID: ""}
	secret := proxy.ConfiguredHeader{IsRequired: true, IsSecret: true, Name: "X-Api-Key", StaticValue: "key", ValueFromRequestHeader: ""}
	headers := proxy.MergeAuthClientHeaders(proxy.HeaderPolicyTunneled, tunnelrouting.Headers(identity.TunneledMCPServerID, "forward-token", "affinity"), []proxy.ConfiguredHeader{secret})

	for _, tc := range []struct {
		name        string
		status      int
		tunnelError string
		want        mcpconnectionattempts.Code
	}{
		{name: "server rejects the key", status: http.StatusUnauthorized, want: mcpconnectionattempts.CodeStaticKeyRejected},
		{name: "tunnel gateway refuses", status: http.StatusForbidden, tunnelError: wire.TunnelErrorForwardRejected, want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.tunnelError != "" {
					w.Header().Set(tunnelrouting.ErrorHeader, tc.tunnelError)
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(upstream.Close)

			rec := mcpconnectionattemptstest.New(t)
			connections := newConnectionsUnderTest(&ConnectionAttribution{
				Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
				Details:          nil,
				UpstreamFailures: true,
				SelfCredential:   false,
				Request:          nil,
			}, rec.Recorder, identity, "https://gateway.internal/mcp", "", headers)

			serveRequest(t, connections, upstream.URL, connectionInitialize)
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Empty(t, row.ProviderHost)
			require.Equal(t, "initialize", row.SubReason)
		})
	}
}

func TestRequestMethodDetail(t *testing.T) {
	t.Parallel()

	for method, want := range map[string]string{
		"initialize":                           "initialize",
		"tools/list":                           "tools_list",
		"resources/templates/list":             "resources_templates_list",
		"logging/setLevel":                     "logging_setLevel",
		"server/discover":                      "server_discover",
		"tasks/get":                            "tasks_get",
		"resources/subscribe":                  "resources_subscribe",
		"tools/call":                           "tools_call",
		"notifications/initialized":            "notification",
		"notifications/anything":               "notification",
		"x-vendor/" + strings.Repeat("a", 200): "other",
		"":                                     "other",
	} {
		require.Equal(t, want, requestMethodDetail(method), method)
	}
}

// Only a rejected credential is recorded off tools/call; a served or otherwise failed handshake writes nothing.
func TestRequestConnections_OtherStatusesRecordNothing(t *testing.T) {
	t.Parallel()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"upstream","version":"1.0.0"}}}`))
	}))
	t.Cleanup(ok.Close)

	for _, tc := range []struct {
		name, upstream, body string
	}{
		{name: "initialize 200", upstream: ok.URL, body: connectionInitialize},
		{name: "initialize 500", upstream: rejectingUpstream(t, http.StatusInternalServerError), body: connectionInitialize},
		{name: "tools/list 404", upstream: rejectingUpstream(t, http.StatusNotFound), body: connectionToolsList},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveRequest(t, recordingConnections(rec, "token"), tc.upstream, tc.body)
			rec.RequireNothing()
		})
	}

	// The sign-in challenge to an anonymous caller is not a rejected credential.
	rec := mcpconnectionattemptstest.New(t)
	serveRequest(t, recordingConnections(rec, ""), rejectingUpstream(t, http.StatusUnauthorized), connectionInitialize)
	rec.RequireNothing()
}

// A gateway member records its handshake from the gateway's own classification.
func TestRequestConnections_GatewayMemberRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	connections := recordingConnections(rec, "token")
	connections.UpstreamFailures = false
	require.Nil(t, connections.requestFailures())
	serveRequest(t, connections, rejectingUpstream(t, http.StatusUnauthorized), connectionInitialize)
	rec.RequireNothing()
}

// A redirect off the server's origin drops the credentials, so the destination's 401 rejected none.
func TestRequestConnections_RedirectedChallengeRecordsNothing(t *testing.T) {
	t.Parallel()

	secret := []proxy.ConfiguredHeader{{Name: "X-Api-Key", IsSecret: true, StaticValue: "key"}}
	for _, tc := range []struct {
		name         string
		body         string
		upstreamAuth string
		headers      []proxy.ConfiguredHeader
	}{
		{name: "bearer on initialize", body: connectionInitialize, upstreamAuth: "token"},
		{name: "static key on tools/list", body: connectionToolsList, headers: secret},
		{name: "bearer on tools/call", body: connectionToolsCall, upstreamAuth: "token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("Authorization"))
				assert.Empty(t, r.Header.Get("X-Api-Key"))
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(destination.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL, http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			rec := mcpconnectionattemptstest.New(t)
			connections := newConnectionsUnderTest(&ConnectionAttribution{
				Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
				Details:          nil,
				UpstreamFailures: true,
				SelfCredential:   false,
				Request:          nil,
			}, rec.Recorder, proxy.ServerIdentity{}, origin.URL, tc.upstreamAuth, tc.headers)

			p := newKillswitchTestProxy(t, origin.URL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
			connections.install(p)
			p.Identity.RemoteMCPServerID = "server"
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			rr := httptest.NewRecorder()
			require.NoError(t, p.Post(rr, req))
			require.Equal(t, http.StatusUnauthorized, rr.Code)
			rec.RequireNothing()
		})
	}
}

// An optional key that fails validation is not sent, so a 401 did not reject it.
func TestRequestConnections_SuppressedKeyRecordsNothing(t *testing.T) {
	t.Parallel()

	headers := []proxy.ConfiguredHeader{{Name: "X-Api-Key", IsSecret: true, StaticValue: "bad\nkey"}}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-Api-Key"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	for _, body := range []string{connectionInitialize, connectionToolsCall} {
		rec := mcpconnectionattemptstest.New(t)
		connections := newConnectionsUnderTest(&ConnectionAttribution{
			Scope:            mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified),
			Details:          nil,
			UpstreamFailures: true,
			SelfCredential:   false,
			Request:          nil,
		}, rec.Recorder, proxy.ServerIdentity{}, upstream.URL, "", headers)
		require.True(t, HasConfiguredCredential(headers), "the stored row is a credential")
		serveRequest(t, connections, upstream.URL, body)
		rec.RequireNothing()
	}
}

// Only a 401, or a 403 whose challenge says the token is invalid, rejects the credential.
func TestCredentialRejected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		status    int
		challenge []string
		want      bool
	}{
		{name: "401 without a challenge", status: http.StatusUnauthorized, want: true},
		{name: "403 without a challenge", status: http.StatusForbidden, want: false},
		{name: "403 insufficient scope", status: http.StatusForbidden, challenge: []string{`Bearer error="insufficient_scope", scope="repo"`}, want: false},
		{name: "403 invalid token", status: http.StatusForbidden, challenge: []string{`Bearer error="invalid_token"`}, want: true},
		{name: "403 unquoted and mixed case", status: http.StatusForbidden, challenge: []string{`bearer ERROR=Invalid_Token`}, want: true},
		{name: "403 invalid token in a later challenge", status: http.StatusForbidden, challenge: []string{`Basic realm="api", DPoP error="invalid_token", algs="ES256"`}, want: true},
		{name: "403 invalid token in a second header", status: http.StatusForbidden, challenge: []string{`Basic realm="api"`, `Bearer error="invalid_token"`}, want: true},
		{name: "403 invalid token only inside a quoted value", status: http.StatusForbidden, challenge: []string{`Bearer error_description="not error=invalid_token", error="insufficient_scope"`}, want: false},
		{name: "404", status: http.StatusNotFound, challenge: []string{`Bearer error="invalid_token"`}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, CredentialRejected(tc.status, tc.challenge))
		})
	}
}

// A 403 on a direct server's handshake or tool call is a rejection only when its challenge says the token is invalid.
func TestRequestConnections_ForbiddenNeedsAnInvalidToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		status    int
		challenge string
		recorded  bool
	}{
		{name: "401", status: http.StatusUnauthorized, recorded: true},
		{name: "403 without a challenge", status: http.StatusForbidden},
		{name: "403 insufficient scope", status: http.StatusForbidden, challenge: `Bearer error="insufficient_scope"`},
		{name: "403 invalid token", status: http.StatusForbidden, challenge: `Bearer error="invalid_token"`, recorded: true},
	} {
		upstream := func(w http.ResponseWriter, _ *http.Request) {
			if tc.challenge != "" {
				w.Header().Set("WWW-Authenticate", tc.challenge)
			}
			w.WriteHeader(tc.status)
		}
		t.Run("initialize "+tc.name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			server := httptest.NewServer(http.HandlerFunc(upstream))
			t.Cleanup(server.Close)
			serveRequest(t, recordingConnections(rec, "token"), server.URL, connectionInitialize)
			if !tc.recorded {
				rec.RequireNothing()
				return
			}
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
			require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
			require.Equal(t, "initialize", row.SubReason)
		})
		t.Run("tools/call "+tc.name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), upstream, continueCheckpoint())
			if !tc.recorded {
				rec.RequireNothing()
				return
			}
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
			require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
			require.Empty(t, row.SubReason)
		})
	}
}
