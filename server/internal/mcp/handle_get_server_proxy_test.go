// handle_get_server_proxy_test.go verifies that GET (standalone SSE stream)
// and DELETE (session termination) on /mcp/{mcpSlug} dispatch through the
// remote proxy for proxy-backed mcp_servers, while toolset-backed servers
// keep the legacy 405 behavior.
package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// serveRuntimeMethod invokes HandleGetServer or HandleDeleteServer with a
// chi-routed request the way the /mcp/{mcpSlug} mux does. The nil metadata
// service passed to HandleGetServer is deliberate: it is only consulted on
// the browser (HTML Accept) branch, which these tests never take.
func serveRuntimeMethod(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	method, mcpSlug, authToken string,
	extraHeaders map[string]string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequest(method, "/mcp/"+mcpSlug, nil)
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	switch method {
	case http.MethodGet:
		if err := ti.service.HandleGetServer(w, req, nil); err != nil {
			return w, fmt.Errorf("handle get server: %w", err)
		}
	case http.MethodDelete:
		if err := ti.service.HandleDeleteServer(w, req); err != nil {
			return w, fmt.Errorf("handle delete server: %w", err)
		}
	default:
		t.Fatalf("unsupported method %s", method)
	}
	return w, nil
}

func requireMethodNotAllowed(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeMethodNotAllowed, oopsErr.Code)
}

func TestRuntimeMethods_RemoteBacked_ProxiedUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	type upstreamHit struct {
		method  string
		accept  string
		session string
	}
	var hits []upstreamHit
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, upstreamHit{
			method:  r.Method,
			accept:  r.Header.Get("Accept"),
			session: r.Header.Get("Mcp-Session-Id"),
		})
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":\"standalone stream hello\"}}\n\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	endpointSlug := "endpoint-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "public", issuerID)
	token := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)

	// A GET without Accept: text/event-stream must keep the legacy 405 and
	// never reach upstream — the SSE gate is what keeps stray probes local.
	_, err := serveRuntimeMethod(t, ctx, ti, http.MethodGet, endpointSlug, token, map[string]string{"Accept": "application/json"})
	requireMethodNotAllowed(t, err)
	require.Empty(t, hits, "non-SSE GET must not be forwarded upstream")

	// The standalone SSE stream relays upstream with the session header.
	w, err := serveRuntimeMethod(t, ctx, ti, http.MethodGet, endpointSlug, token, map[string]string{
		"Accept":         "text/event-stream",
		"Mcp-Session-Id": "sess-abc",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Len(t, hits, 1)
	require.Equal(t, http.MethodGet, hits[0].method, "standalone stream GET must be relayed upstream")
	require.Contains(t, hits[0].accept, "text/event-stream")
	require.Equal(t, "sess-abc", hits[0].session, "session header must be forwarded upstream")
	require.Contains(t, w.Body.String(), "standalone stream hello", "upstream SSE events must be relayed back")

	// DELETE (session termination) relays upstream as well.
	w, err = serveRuntimeMethod(t, ctx, ti, http.MethodDelete, endpointSlug, token, map[string]string{"Mcp-Session-Id": "sess-to-end"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Len(t, hits, 2)
	require.Equal(t, http.MethodDelete, hits[1].method, "session termination must be relayed upstream")
	require.Equal(t, "sess-to-end", hits[1].session, "session header must be forwarded upstream")
}

// TestRuntimeMethods_MountedOnMux drives GET and DELETE through the real
// /mcp/{mcpSlug} route registrations rather than invoking the handlers
// directly, so a regression in the mux wiring fails here. The distinctive
// oops error messages prove our handlers ran instead of the muxer's own
// method-not-allowed fallback.
func TestRuntimeMethods_MountedOnMux(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	metadataService := mcpmetadata.NewService(
		ti.logger,
		ti.tracerProvider,
		testenv.NewMeterProvider(t),
		ti.conn,
		ti.sessionManager,
		ti.serverURL,
		ti.siteURL,
		ti.cacheAdapter,
		authz.NewEngine(ti.logger, ti.conn, nil, workos.NewStubClient()),
		ti.audit,
	)

	mux := goahttp.NewMuxer()
	mcp.Attach(mux, ti.service, metadataService)

	req := httptest.NewRequest(http.MethodGet, "/mcp/no-such-slug-"+uuid.NewString(), nil).WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Contains(t, rr.Body.String(), "compatibility probe", "GET must be routed to HandleGetServer")

	req = httptest.NewRequest(http.MethodDelete, "/mcp/no-such-slug-"+uuid.NewString(), nil).WithContext(ctx)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Contains(t, rr.Body.String(), "session termination is not supported", "DELETE must be routed to HandleDeleteServer")
}

func TestRuntimeMethods_ToolsetBacked_Keep405(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "toolset-slug-"+uuid.NewString()[:8])
	endpointSlug := "endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	_, err := serveRuntimeMethod(t, ctx, ti, http.MethodGet, endpointSlug, "", map[string]string{"Accept": "text/event-stream"})
	requireMethodNotAllowed(t, err)

	_, err = serveRuntimeMethod(t, ctx, ti, http.MethodDelete, endpointSlug, "", nil)
	requireMethodNotAllowed(t, err)

	// Unknown slugs fall through the endpoint lookup and keep the 405 too.
	_, err = serveRuntimeMethod(t, ctx, ti, http.MethodGet, "no-such-slug-"+uuid.NewString(), "", map[string]string{"Accept": "text/event-stream"})
	requireMethodNotAllowed(t, err)
}
