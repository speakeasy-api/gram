package xmcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`

// runHandlerWithHeaders is a generalized variant of runHandler that threads
// extra request headers (e.g. Mcp-Session-Id) onto the test request.
func runHandlerWithHeaders(t *testing.T, ctx context.Context, ti *testInstance, method, slug, authorization string, body []byte, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	mux := chi.NewMux()
	mux.MethodFunc(method, xmcp.RuntimePath, oops.ErrHandle(ti.logger, ti.service.ServeMCP).ServeHTTP)

	req := httptest.NewRequestWithContext(ctx, method, "/x/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// insertProject creates a stub project row so we can test cross-project
// isolation and returns its id for use as a foreign key on remote_mcp_servers.
func insertProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-project-" + uuid.NewString()[:8]
	p, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return p.ID
}

func TestServeRuntime_SlugNotFoundReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	rr := runHandler(t, ctx, ti, http.MethodPost, "no-such-slug", "", []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestServeRuntime_PrivateMissingAuthReturns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "private")

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestServeRuntime_PrivateInvalidAuthReturns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "private")

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer("gram_test_not_a_real_key"), []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestServeRuntime_DisabledReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	// Even with valid auth a disabled server should look exactly like a
	// missing one — visibility is the runtime kill-switch.
	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "disabled")
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestServeRuntime_PublicNoAuth_ForwardsToRemoteMCPServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{
		Tools: []testmcp.Tool{{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
				"required": []any{"location"},
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{{"type": "text", "text": "San Francisco: sunny, 72F"}},
			},
		}},
	})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "public")

	// Public + no external OAuth + no caller-supplied token: the server is
	// open to anonymous traffic.
	initResp := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusOK, initResp.Code, "initialize body=%s", initResp.Body.String())

	sessionID := initResp.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID, "proxy must relay Mcp-Session-Id from upstream")
}

// API key callers bypass RBAC (they have their own scoping); a private
// mcp_server in the API key's own org is reachable as long as the key's
// principal authenticates.
func TestServeRuntime_PrivateAPIKeySameOrgReachable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "private")
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
}

func TestServeRuntime_Post_AppliesStaticSecretHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAPIKey string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-Upstream-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public", remotemcprepo.CreateHeaderParams{
		Name:       "X-Upstream-Api-Key",
		Value:      pgtype.Text{String: "upstream-secret", Valid: true},
		IsRequired: true,
		IsSecret:   true,
	})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "upstream-secret", gotAPIKey, "secret static header must be decrypted and forwarded")
}

func TestServeRuntime_Delete_ForwardsSessionTermination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotMethod, gotSession string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSession = r.Header.Get("Mcp-Session-Id")
		w.WriteHeader(http.StatusNoContent)
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")

	rr := runHandlerWithHeaders(t, ctx, ti, http.MethodDelete, slug, "", nil, map[string]string{"Mcp-Session-Id": "abc-session"})
	<-done
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "abc-session", gotSession)
}

// Private mcp_server: the caller's Authorization is a Gram API key used
// for identity auth and must never reach the upstream MCP server.
func TestServeRuntime_PrivateStripsAuthorizationFromUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "private")
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Empty(t, gotAuth, "Gram API key must never leak to the remote MCP server")
}

// Public mcp_server with no external_oauth_server_id and no caller auth:
// upstream sees no Authorization header (nothing to forward).
func TestServeRuntime_PublicNoCallerAuthSendsNoAuthorizationUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Empty(t, gotAuth)
}

// Public mcp_server + external_oauth_server_id + caller Bearer: the
// caller's Authorization is intended for the upstream MCP server (Gram is
// not the AS in this configuration), so the proxy must forward it
// verbatim.
func TestServeRuntime_PublicExternalOAuthForwardsAuthorizationUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := seedRemoteMCPEndpointWithExternalOAuth(t, ctx, ti, *authCtx.ProjectID, upstream.URL)

	const upstreamToken = "upstream-issued-bearer-token"
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(upstreamToken), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Equal(t, "Bearer "+upstreamToken, gotAuth, "external-OAuth Bearer must reach the upstream verbatim")
}

// Public mcp_server + external_oauth_server_id + no caller Bearer:
// upstream sees no Authorization header (the caller didn't supply one).
func TestServeRuntime_PublicExternalOAuthNoCallerTokenSendsNoAuthorizationUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := seedRemoteMCPEndpointWithExternalOAuth(t, ctx, ti, *authCtx.ProjectID, upstream.URL)

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Empty(t, gotAuth)
}

// Public mcp_server + oauth_proxy_server_id: the resolver
// (mcp.Service.ResolveOAuthProxyUpstreamToken) is currently a stub that
// returns ("", nil), so the upstream sees no Authorization regardless of
// the caller's Bearer. This test pins that stub behavior; once the real
// resolver lands and a stored upstream credential is seeded, the
// upstream should observe the swapped Bearer instead.
func TestServeRuntime_PublicOAuthProxyStubSendsNoAuthorizationUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := seedRemoteMCPEndpointWithOAuthProxy(t, ctx, ti, *authCtx.ProjectID, upstream.URL)

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer("gram_test_anything"), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Empty(t, gotAuth, "stub resolver returns empty override; upstream sees no Authorization")
}

// Public mcp_server (no external OAuth) + caller Bearer that authenticates
// as a Gram identity: the token was probed as a Gram credential and must
// not be forwarded upstream.
func TestServeRuntime_PublicGramAPIKeyStripsAuthorizationFromUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	require.Empty(t, gotAuth, "Gram API key must never leak even on a public mcp_server")
}

// Same-org cross-project access to a private mcp_server is allowed: the
// org-membership check passes (caller and server share the active org)
// and the API key bypasses RBAC scope checking. This mirrors /mcp.
func TestServeRuntime_PrivateSameOrgCrossProjectReachable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProjectID := insertProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, otherProjectID, mockServer.URL, "private")
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
}

// Cross-org access to a private mcp_server is rejected at the org-membership
// gate before RBAC even runs, matching /mcp behavior.
func TestServeRuntime_PrivateCrossOrgReturns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherOrgID := "org_" + uuid.NewString()
	_, err := organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrgID,
		Name:        "other-org",
		Slug:        "other-org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	otherProjectID := insertProject(t, ctx, ti, otherOrgID)
	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, otherProjectID, mockServer.URL, "private")

	// API key is in the original (caller's) org; mcp_server is in a foreign
	// org. The org-membership check rejects.
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
