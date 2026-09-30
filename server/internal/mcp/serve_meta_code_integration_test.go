//go:build codemodeintegration

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
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
	mcp.SetTestCodeExecutor(ti.service, coordinator)
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
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil)
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
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil)
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
	mcp.SetTestCodeExecutor(ti.service, other)
	otherToken := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil)
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
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": code}}), "", nil)
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
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.search("")`, "tools": []string{}}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"items":[]`)
	require.NotContains(t, w.Body.String(), "remote--ping")
}

func TestCodeModeFrozenSessionRestrictsDiscoveryAndCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	configureCodeRunner(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	first := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved tools", "approved", 1, first.url)
	inventory, err := ti.service.GatewayInventory(ctx, gateway.ID, *auth.ProjectID, urn.NewUserSubject(auth.UserID))
	require.NoError(t, err)
	require.Len(t, inventory.Tools, 1)
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, inventory)
	second := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "New tools", "new", 2, second.url)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.search("")`}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "approved--ping")
	require.NotContains(t, w.Body.String(), "new--ping")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": `await tools.call("new--ping", {})`}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"isError":true`)
}

func TestCodeModeRolloutOnlyGatesNewSelections(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	ti.features.SetFlag(feature.FlagGatewayCodeMode, auth.ActiveOrganizationID, true)
	require.False(t, ti.service.CodeModeAvailable(ctx, auth.ActiveOrganizationID, *auth.ProjectID), "no runtime")
	configureCodeRunner(t, ti)
	require.True(t, ti.service.CodeModeAvailable(ctx, auth.ActiveOrganizationID, *auth.ProjectID))
	require.False(t, ti.service.CodeModeAvailable(ctx, auth.ActiveOrganizationID, uuid.New()), "project must belong to org")
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil)
	ti.features.SetFlag(feature.FlagGatewayCodeMode, auth.ActiveOrganizationID, false)
	require.False(t, ti.service.CodeModeAvailable(ctx, auth.ActiveOrganizationID, *auth.ProjectID))
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "6 * 7"}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"value":42`)
	require.NotContains(t, w.Body.String(), `"isError":true`)
}
