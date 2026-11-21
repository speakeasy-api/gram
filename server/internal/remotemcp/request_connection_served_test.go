package remotemcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func directScope() mcpconnectionattempts.Scope {
	return mcpconnectionattempts.ServerScope("org", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR)
}

// servedConnections is a direct server's attribution for a named person, with the
// service client their own sign-in used when serviceClient is set.
func servedConnections(rec *mcpconnectionattemptstest.Recording, serviceClient uuid.UUID) connectionsUnderTest {
	return servedConnectionsIn(rec, directScope(), serviceClient)
}

func servedConnectionsIn(rec *mcpconnectionattemptstest.Recording, scope mcpconnectionattempts.Scope, serviceClient uuid.UUID) connectionsUnderTest {
	person := urn.NewUserSubject("user-1")
	details := []mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&person)}
	if serviceClient != uuid.Nil {
		details = append(details, mcpconnectionattempts.RemoteSessionClient(serviceClient))
	}
	return newConnectionsUnderTest(&ConnectionAttribution{
		Scope:            scope,
		Details:          details,
		UpstreamFailures: true,
		SelfCredential:   false,
		Request:          nil,
	}, rec.Recorder, proxy.ServerIdentity{}, "https://provider.example.com/mcp", "token", nil)
}

func answering(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}
}

func upstreamURL(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// A served request and a failed one on the same proxy name the same connection.
func TestRequestConnections_ServedNamesTheFailuresConnection(t *testing.T) {
	t.Parallel()

	service := uuid.New()
	scope := directScope()
	served := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, servedConnectionsIn(served, scope, service), answering("application/json", `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`), continueCheckpoint())
	got := served.RequireServed(true)
	served.RequireNothing()

	failed := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, servedConnectionsIn(failed, scope, service), func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, continueCheckpoint())
	row := failed.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError)
	failed.RequireNoneServed()

	require.Equal(t, row.OrganizationID, got.OrganizationID)
	require.Equal(t, row.ProjectID, got.ProjectID)
	require.Equal(t, row.ServerKind, got.ServerKind)
	require.Equal(t, row.ServerID, got.ServerID)
	require.Equal(t, row.MemberID, got.MemberID)
	require.Equal(t, row.SubjectURN, got.SubjectURN)
	require.Equal(t, row.AppKey, got.AppKey)
	require.Equal(t, "dcr", got.AppKey)
	require.Equal(t, service.String(), row.RemoteSessionClientID, "the failure carries the person's service sign-in")
	require.Equal(t, service.String(), got.RemoteSessionClientID, "the served row carries the person's service sign-in")
}

// Without the person's own sign-in, neither row names a service client.
func TestRequestConnections_ServedWithoutAServiceSignIn(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	serveToolsCall(t.Context(), t, servedConnections(rec, uuid.Nil), answering("application/json", `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`), continueCheckpoint())
	require.Empty(t, rec.RequireServed(true).RemoteSessionClientID)
}

// Every forwarded method the server answered without an error is served by the server.
func TestRequestConnections_ServedMethods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		request  string
		upstream http.HandlerFunc
	}{
		{name: "initialize", request: connectionInitialize, upstream: answering("application/json", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}}`)},
		{name: "tools/list", request: `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, upstream: answering("application/json", `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`)},
		{name: "tools/call over SSE", request: connectionToolsCall, upstream: answering("text/event-stream", "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[]}}\n\n")},
		{name: "notification", request: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, upstream: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveRequest(t, servedConnections(rec, uuid.Nil), upstreamURL(t, tc.upstream), tc.request)
			rec.RequireServed(true)
			rec.RequireNothing()
		})
	}
}

// An error answer, a non-2xx, or a refusal before forwarding is not served.
func TestRequestConnections_NotServed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		request  string
		upstream http.HandlerFunc
	}{
		{name: "tool error", request: connectionToolsCall, upstream: answering("application/json", `{"jsonrpc":"2.0","id":7,"result":{"isError":true,"content":[]}}`)},
		{name: "tool error over SSE", request: connectionToolsCall, upstream: answering("text/event-stream", "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"isError\":true,\"content\":[]}}\n\n")},
		{name: "JSON-RPC error", request: `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, upstream: answering("application/json", `{"jsonrpc":"2.0","id":2,"error":{"code":-32603,"message":"boom"}}`)},
		{name: "SSE stream closed without a reply", request: connectionToolsCall, upstream: answering("text/event-stream", ": keepalive\n\n")},
		{name: "server error", request: connectionToolsCall, upstream: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }},
		{name: "rejected credential", request: connectionInitialize, upstream: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			serveRequest(t, servedConnections(rec, uuid.Nil), upstreamURL(t, tc.upstream), tc.request)
			rec.RequireNoneServed()
		})
	}
}

type refusingToolsCall struct{ err error }

func (r refusingToolsCall) InterceptToolsCallRequest(context.Context, *proxy.ToolsCallRequest) error {
	return r.err
}

func (refusingToolsCall) Name() string { return "refusing" }

// A call Speakeasy refused or held for approval never reached the server.
func TestRequestConnections_RefusedOrHeldCallIsNotServed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "refused", err: &proxy.RejectError{Code: proxy.RejectCodeForbidden, Message: "blocked", Data: nil}},
		{name: "held for approval", err: &proxy.ToolResultError{Message: "needs approval"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			upstream := upstreamURL(t, func(http.ResponseWriter, *http.Request) { t.Error("a refused call must not reach the upstream") })
			p := newKillswitchTestProxy(t, upstream, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)), refusingToolsCall{err: tc.err})
			servedConnections(rec, uuid.Nil).install(p)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			require.NoError(t, p.Post(httptest.NewRecorder(), req))
			rec.RequireNoneServed()
		})
	}
}

// A request with no named person, a gateway member's proxy, or a caller that left records nothing.
func TestRequestConnections_ServedNeedsAPersonAndADirectServer(t *testing.T) {
	t.Parallel()

	ok := answering("application/json", `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`)

	t.Run("no person", func(t *testing.T) {
		t.Parallel()
		rec := mcpconnectionattemptstest.New(t)
		serveToolsCall(t.Context(), t, recordingConnections(rec, "token"), ok, continueCheckpoint())
		rec.RequireNoneServed()
	})

	t.Run("gateway member", func(t *testing.T) {
		t.Parallel()
		rec := mcpconnectionattemptstest.New(t)
		connections := servedConnections(rec, uuid.Nil)
		connections.UpstreamFailures = false
		require.Nil(t, connections.requestsServed())
		serveToolsCall(t.Context(), t, connections, ok, continueCheckpoint())
		rec.RequireNoneServed()
	})

	t.Run("caller left", func(t *testing.T) {
		t.Parallel()
		rec := mcpconnectionattemptstest.New(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		servedConnections(rec, uuid.Nil).InterceptRequestServed(ctx, &proxy.RequestServed{Method: "tools/call"})
		rec.RequireNoneServed()
	})
}
