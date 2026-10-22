//go:build codemodeintegration

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func configureCodeRunner(t *testing.T, ti *testInstance) *codemode.Coordinator {
	t.Helper()
	endpoint, token := testenv.LaunchCodeRunner(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	client, err := codemode.NewRunnerClient(endpoint, token, policy.Dialer())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, ok := ti.cacheAdapter.(codemode.OwnershipStore)
	require.True(t, ok)
	coordinator, err := codemode.NewCoordinator(&codemode.StaticProvider{Runner: client}, store)
	require.NoError(t, err)
	mcp.SetTestCodeExecutor(ti.service.Service, coordinator)
	return coordinator
}

func codeUpstreamWithCallHook(t *testing.T, hook func(*http.Request)) string {
	t.Helper()
	upstream := newRecordingUpstream(t, "ping")
	u, err := url.Parse(upstream.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(u)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Method == "tools/call" {
			hook(r)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestCodeModeLiveSessionRevocationStopsNextCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(token, urn.NewUserSessionIssuer(issuer).String())
	require.NoError(t, err)
	queries := usersessionsrepo.New(ti.conn)
	session, err := queries.GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer, Jti: claims.ID})
	require.NoError(t, err)
	var calls atomic.Int64
	revoked := make(chan error, 1)
	upstream := codeUpstreamWithCallHook(t, func(r *http.Request) {
		if calls.Add(1) == 1 {
			_, err := queries.RevokeUserSession(r.Context(), usersessionsrepo.RevokeUserSessionParams{ID: session.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
			revoked <- err
		}
	})
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote tools", "remote", 1, upstream)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `first = await tools.call("remote--ping", {})
await tools.call("remote--ping", {})`}}), token, nil)
	require.NoError(t, err)
	select {
	case err := <-revoked:
		require.NoError(t, err)
	default:
		t.Fatalf("tool did not run: %s", w.Body.String())
	}
	require.EqualValues(t, 1, calls.Load(), "%s", w.Body.String())
	require.Contains(t, w.Body.String(), "authorization_revoked")
	require.Contains(t, w.Body.String(), `"outcome":"completed"`)
	require.Contains(t, w.Body.String(), `"isError":true`)
}

func TestCodeModeSessionlessCancellationThroughAnotherCoordinator(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	entered := make(chan struct{}, 1)
	unwound := make(chan struct{}, 1)
	upstream := codeUpstreamWithCallHook(t, func(r *http.Request) { entered <- struct{}{}; <-r.Context().Done(); unwound <- struct{}{} })
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote tools", "remote", 1, upstream)
	requestCtx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	type response struct {
		body string
		err  error
	}
	done := make(chan response, 1)
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.call("remote--ping", {})`}})
	go func() {
		w, err := servePublicHTTP(t, requestCtx, ti, slug, body, token, nil)
		done <- response{body: w.Body.String(), err: err}
	}()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("execution never dispatched: %+v", result)
	case <-requestCtx.Done():
		t.Fatal("tool did not start")
	}
	// This coordinator shares only Redis. It has no local record or runtime for the active run.
	store, ok := ti.cacheAdapter.(codemode.OwnershipStore)
	require.True(t, ok)
	other, err := codemode.NewCoordinator(&codemode.StaticProvider{}, store)
	require.NoError(t, err)
	mcp.SetTestCodeExecutor(ti.service.Service, other)
	otherToken := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	for _, attempt := range []struct {
		token   string
		headers map[string]string
		request string
	}{
		{token: otherToken, headers: nil, request: `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`},
		{token: token, headers: map[string]string{"Mcp-Session-Id": "another-session"}, request: `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`},
		{token: token, headers: nil, request: `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"1"}}`},
	} {
		_, err := servePublicHTTP(t, t.Context(), ti, slug, []byte(attempt.request), attempt.token, attempt.headers)
		require.NoError(t, err)
	}
	require.Never(t, func() bool { return len(done) > 0 }, 300*time.Millisecond, 20*time.Millisecond, "another credential, session, or ID type must not cancel this request")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, w.Code)
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Contains(t, result.body, `"code":"cancelled"`)
		require.Contains(t, result.body, `"outcome":"unknown"`)
	case <-time.After(3 * time.Second):
		t.Fatal("cross-coordinator cancellation did not reach owner")
	}
	select {
	case <-unwound:
	case <-time.After(time.Second):
		t.Fatal("cancelled callback outlived execution cleanup")
	}
}

func TestCodeModeEndToEndDiscoveryRemoteAndTunnel(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	hosted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 0, "public", "list")
	remote := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote tools", "remote", 1, remote.url)
	tunnel := &fakeTunnelGateway{t: t, agentSessionID: "code-agent", backendSessionID: "code-backend", toolNames: []string{"ping"}}
	tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Tunnel tools", "tunnel", 2, "")
	upstream := httptest.NewServer(tunnel)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	var catalog struct {
		Result struct {
			Tools []struct {
				Name        string
				Description string
			}
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	require.Len(t, catalog.Result.Tools, 1)
	require.Equal(t, "execute", catalog.Result.Tools[0].Name)
	require.Contains(t, catalog.Result.Tools[0].Description, hosted.slug+"--")
	require.Contains(t, catalog.Result.Tools[0].Description, "tunnel--")
	code := `import asyncio
page = await tools.search("")
paths = [item["path"] for item in page["items"]]
description = await tools.describe("remote--ping")
remote, tunnel = await asyncio.gather(tools.call("remote--ping", {}), tools.call("tunnel--ping", {}))
{"paths": paths, "remote": remote["ok"], "tunnel": tunnel["ok"]}`
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": code}}), "", map[string]string{"Mcp-Session-Id": uuid.NewString()})
	require.NoError(t, err)
	var response struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			StructuredContent codemode.ExecutionResult
			IsError           bool
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Empty(t, response.Error, "%s", w.Body.String())
	require.False(t, response.Result.IsError, "%s", w.Body.String())
	require.Nil(t, response.Result.StructuredContent.Error, "%s", w.Body.String())
	require.Contains(t, string(response.Result.StructuredContent.Value), hosted.slug+"--list")
	require.Contains(t, string(response.Result.StructuredContent.Value), `"tunnel":true`)
	require.Len(t, response.Result.StructuredContent.ToolCalls, 2)
}

func TestCodeModeSessionNarrowsHelpers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	remote := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote tools", "remote", 1, remote.url)
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.search("")`, "tools": []string{}}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"items":[]`)
	require.NotContains(t, w.Body.String(), "remote--ping")
}

func TestCodeModeConsentCatalogRestrictsDiscoveryAndCall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode     toolfilter.CatalogMode
		specific bool
	}{
		{toolfilter.CatalogModeCached, false},
		{toolfilter.CatalogModeCached, true},
		{toolfilter.CatalogModeLive, false},
	} {
		t.Run(fmt.Sprintf("%s/specific=%t", tc.mode, tc.specific), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			configureCodeRunner(t, ti)
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "code-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			first := newCatalogUpstream(t, 1)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved tools", "approved", 1, first.url)
			unavailable := newCatalogUpstream(t, 1)
			unavailable.unavailable.Store(true)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Unavailable tools", "unavailable", 2, unavailable.url)
			var selected []string
			if tc.specific {
				selected = []string{"approved--tool_000"}
			}
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, tc.mode, metamcp.DiscoveryModeCode, selected)
			tokenCalls := seedExpiredCatalogCredential(t, ctx, ti, issuer.ID, urn.NewUserSubject(auth.UserID))
			before := first.requests.Load()
			beforeUnavailable := unavailable.requests.Load()
			require.Positive(t, beforeUnavailable, "consent attempts discovery before recording the member as unavailable")
			unavailable.unavailable.Store(false)
			second := newCatalogUpstream(t, 1)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "New tools", "new", 3, second.url)
			execute := func(code string) string {
				t.Helper()
				w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": code}}), connection.token, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				return w.Body.String()
			}
			found := execute(`[await tools.servers(), await tools.search(""), await tools.describe("approved--tool_000")]`)
			require.NotContains(t, found, `"isError":true`)
			require.Contains(t, found, "approved--tool_000")
			require.Contains(t, found, "inputSchema")
			require.NotContains(t, found, "new--tool_000")
			require.NotContains(t, found, "unavailable--tool_000")
			if tc.mode == toolfilter.CatalogModeCached {
				require.Equal(t, before, first.requests.Load(), "all three Python discovery helpers reuse the consent catalogue")
				require.Zero(t, tokenCalls.Load(), "cached Python discovery never refreshes credentials")
			} else {
				require.Greater(t, first.requests.Load(), before, "live mode refreshes the approved member")
			}
			for _, path := range []string{"new--tool_000", "unavailable--tool_000"} {
				require.Contains(t, execute(`await tools.call("`+path+`", {})`), `"isError":true`)
			}
			require.Zero(t, second.requests.Load(), "a newly added member needs new consent")
			require.Equal(t, beforeUnavailable, unavailable.requests.Load(), "an unavailable-at-consent member receives no requests after consent")
			require.Contains(t, execute(`await tools.call("approved--tool_000", {})`), "executed live")
			require.EqualValues(t, 1, first.calls.Load())
		})
	}
}

func TestCodeModeOrganizationGatewaySearchesAndCallsAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: auth.ActiveOrganizationID,
	})
	require.NoError(t, err)
	// Each member sits behind a decoy gateway in its own project: the
	// organization gateway derives both from the caller's access.
	here := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	there := createMetaMcpEndpoint(t, ctx, ti.conn, otherProject.ID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	local := newRecordingUpstream(t, "pong")
	localSlug := "local-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, here.ID, "Local tools", localSlug, 1, local.url)
	remote := newRecordingUpstream(t, "ping")
	remoteSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, otherProject.ID, there.ID, "Remote tools", remoteSlug, 1, remote.url)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.search("")`}}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), localSlug+"--pong")
	require.Contains(t, w.Body.String(), remoteSlug+"--ping")
	for _, path := range []string{localSlug + "--pong", remoteSlug + "--ping"} {
		w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.call("` + path + `", {})`}}))
		require.NoError(t, err)
		require.NotContains(t, w.Body.String(), `"isError":true`, w.Body.String())
		require.NotContains(t, w.Body.String(), "authorization_revoked")
	}
	for _, upstream := range []*recordingUpstream{local, remote} {
		calls := 0
		for _, request := range upstream.journal() {
			if request.rpcMethod == "tools/call" {
				calls++
			}
		}
		require.Equal(t, 1, calls, "each project's member served its own call")
	}
}

func TestCodeModeAvailableFollowsRuntime(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	require.False(t, ti.service.CodeModeAvailable(), "no runtime")
	configureCodeRunner(t, ti)
	require.True(t, ti.service.CodeModeAvailable())
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "6 * 7"}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"value":42`)
	require.NotContains(t, w.Body.String(), `"isError":true`)
}
