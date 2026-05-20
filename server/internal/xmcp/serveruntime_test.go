package xmcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	// MCP Streamable HTTP § Sending Messages to the Server (step 2)
	// requires clients to list both application/json and text/event-stream
	// on POST; testmcp's SDK-backed server enforces this.
	req.Header.Set("Accept", "application/json, text/event-stream")
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

func TestServeMCP_SlugNotFoundReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	rr := runHandler(t, ctx, ti, http.MethodPost, "no-such-slug", "", []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestServeMCP_PrivateRemoteBackend_MissingAuthReturns401(t *testing.T) {
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

func TestServeMCP_PrivateRemoteBackend_InvalidAuthReturns401(t *testing.T) {
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

func TestServeMCP_DisabledReturns404(t *testing.T) {
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

func TestServeMCP_PublicRemoteBackend_AnonymousForwardsUpstream(t *testing.T) {
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
func TestServeMCP_PrivateRemoteBackend_APIKeySameOrgReachable(t *testing.T) {
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

func TestServeMCP_PublicRemoteBackend_AppliesStaticSecretHeader(t *testing.T) {
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

func TestServeMCP_PublicRemoteBackend_DeleteForwardsSessionTermination(t *testing.T) {
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
func TestServeMCP_PrivateRemoteBackend_StripsAuthorizationFromUpstream(t *testing.T) {
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
func TestServeMCP_PublicRemoteBackend_NoCallerAuthSendsNoAuthorizationUpstream(t *testing.T) {
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

// Public mcp_server (no external OAuth) + caller Bearer that authenticates
// as a Gram identity: the token was probed as a Gram credential and must
// not be forwarded upstream.
func TestServeMCP_PublicRemoteBackend_GramAPIKeyStripsAuthorizationFromUpstream(t *testing.T) {
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
func TestServeMCP_PrivateRemoteBackend_SameOrgCrossProjectReachable(t *testing.T) {
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
func TestServeMCP_PrivateRemoteBackend_CrossOrgReturns401(t *testing.T) {
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

// TestServeMCP_IssuerGatedRemoteBackend_MissingAuthEmitsChallenge verifies
// that an issuer-gated /x/mcp remote-backed request without a valid
// Authorization header receives 401 + a WWW-Authenticate header whose
// resource_metadata URL points at /.well-known/oauth-protected-resource/x/mcp/<slug>
// — exactly what a spec-compliant MCP client constructs from a resource
// URL of <base>/x/mcp/<slug>.
func TestServeMCP_IssuerGatedRemoteBackend_MissingAuthEmitsChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	wwwAuth := rr.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth)

	expectedResourceMetadataURL := "http://0.0.0.0/.well-known/oauth-protected-resource/x/mcp/" + slug
	require.Equal(t, fmt.Sprintf(`Bearer resource_metadata="%s"`, expectedResourceMetadataURL), wwwAuth)
}

// TestServeMCP_IssuerGatedRFC9728Invariant asserts the RFC 9728 §5.3 / §3
// contract between the ServeMCP challenge and the well-known protected-
// resource metadata: the resource_metadata URL embedded in
// WWW-Authenticate must string-equal the metadata response's `resource`
// field (and its `authorization_servers[0]` entry's RFC 9728 prefix
// path). A drift here breaks spec-compliant MCP-client discovery, which
// follows the WWW-Authenticate header to fetch the metadata document.
func TestServeMCP_IssuerGatedRFC9728Invariant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	// Capture WWW-Authenticate's resource_metadata URL from an unauthenticated
	// ServeMCP request.
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth)
	expectedWWW := fmt.Sprintf(`Bearer resource_metadata="http://0.0.0.0/.well-known/oauth-protected-resource/x/mcp/%s"`, slug)
	require.Equal(t, expectedWWW, wwwAuth)

	// Fetch the protected-resource metadata and confirm its `resource`
	// field is the same `<base>/x/mcp/<slug>` URL the WWW-Authenticate
	// resource_metadata URL is keyed under.
	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/x/mcp/"+slug, metadata["resource"])
}

// TestServeMCP_IssuerGatedRemoteBackend_HappyPath drives the full
// post-consent half of the OAuth flow for an /x/mcp issuer-gated
// remote-backed mcp_server and verifies that the minted bearer token
// authorises a subsequent ServeMCP request to be proxied upstream.
//
// We seed the post-consent state directly (user_session_clients row +
// UserSessionGrant in Redis) rather than driving register → authorize →
// consent → token through HTTP, because the upstream review specifically
// asked for the token-mint and bearer-use legs — the consent-and-earlier
// path is already covered by authnchallenge_test.go on the /mcp side and
// the adapters in xmcp/service.go are thin slug-resolution shims that
// delegate to the same mcp.Service.Serve* methods.
func TestServeMCP_IssuerGatedRemoteBackend_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Upstream stub that records the forwarded request and returns a
	// well-formed JSON-RPC initialize response.
	var (
		gotMethod string
		gotAuth   string
	)
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	// Seed the issuer-gated /x/mcp endpoint and look up its
	// organization id (mcp_servers doesn't carry org id directly —
	// NewResolvedMcpEndpointFromMcpServer needs it threaded in).
	slug, mcpServer, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID)

	// Public OAuth client (token_endpoint_auth_method=none) — no
	// client_secret_hash, PKCE alone establishes proof-of-possession.
	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err = usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "happy-path test client",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	// Seed a UserSessionGrant directly — what HandleConsent's POST
	// would have written after the user clicked "approve". The
	// anonymous subject matches the visibility=public flow:
	// HandleAuthorize stamps urn:gram:anonymous:<uuid> on public
	// endpoints instead of round-tripping through the IDP.
	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code := "auth-code-" + uuid.NewString()
	subject := urn.NewAnonymousSubject(uuid.NewString())
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.Nil,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		Subject:             subject,
		CreatedAt:           time.Now(),
	}))

	// Drive ServeToken directly with the auth_code grant.
	// mcp.Service.ServeToken is shared by /mcp and /x/mcp handler
	// adapters; the xmcp adapter is a slug-resolution shim that calls
	// into this same method, so exercising ServeToken with a
	// hand-built ResolvedMcpEndpoint covers the same code path the
	// /x/mcp/{slug}/token route runs end-to-end.
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", code)
	tokenForm.Set("redirect_uri", redirectURI)
	tokenForm.Set("client_id", clientID)
	tokenForm.Set("code_verifier", verifier)
	tokenReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	tokenReq = tokenReq.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	tokenW := httptest.NewRecorder()
	require.NoError(t, ti.mcpService.ServeToken(tokenW, tokenReq, endpoint))
	require.Equal(t, http.StatusOK, tokenW.Code, "token endpoint should mint an access token: %s", tokenW.Body.String())

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	require.NoError(t, json.Unmarshal(tokenW.Body.Bytes(), &tokenResp))
	require.NotEmpty(t, tokenResp.AccessToken)
	require.Equal(t, "Bearer", tokenResp.TokenType)

	// Use the minted bearer against ServeMCP. The issuer gate accepts
	// the JWT and the request proxies through to the upstream stub,
	// which records the forwarded Authorization header (the proxy
	// always strips the inbound Authorization — empty here because
	// the issuer has no remote_session_clients bound).
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(tokenResp.AccessToken), []byte(initializeBody))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked within 5s; status=%d body=%s", rr.Code, rr.Body.String())
	}
	require.Equal(t, http.StatusOK, rr.Code, "ServeMCP should proxy through; body=%s", rr.Body.String())
	require.Equal(t, http.MethodPost, gotMethod)
	require.Empty(t, gotAuth, "remote proxy strips inbound Authorization; no upstream remote_session is configured")
}

// mintIssuerGatedAccessToken drives ServeToken with a synthesised
// UserSessionGrant against the given endpoint and returns the minted
// JWT. Used by happy-path tests that need a bearer to exercise the
// post-gate code paths without driving register → authorize → consent
// over real HTTP. The grant is keyed by a fresh code per call so
// parallel tests don't race.
func mintIssuerGatedAccessToken(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	endpoint *mcp.ResolvedMcpEndpoint,
	issuerID uuid.UUID,
	subject urn.SessionSubject,
) string {
	t.Helper()

	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "issuer-gated test client",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code := "auth-code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.Nil,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		Subject:             subject,
		CreatedAt:           time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.mcpService.ServeToken(w, req, endpoint))
	require.Equal(t, http.StatusOK, w.Code, "token endpoint should mint an access token: %s", w.Body.String())

	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.AccessToken)
	require.Equal(t, "Bearer", resp.TokenType)
	return resp.AccessToken
}

// TestServeMCP_IssuerGatedToolsetBackend_HappyPath verifies the
// toolset-backed companion to TestServeMCP_IssuerGatedRemoteBackend_HappyPath.
// A bearer minted against the mcp_server's issuer must authorise a
// subsequent ServeMCP request to dispatch through the toolset-backed
// runtime path (no upstream remote MCP server in this case — the
// initialize response comes from Gram itself).
//
// Catches the symmetric bug of the one fixed in serveRemoteBackend:
// inside ServeToolsetResolved the legacy auth chain is skipped on
// !issuerGated, but issuerGated is computed as
// `toolset.UserSessionIssuerID.Valid && !skipIssuerGate`. When /x/mcp
// passes skipIssuerGate=true (the caller already gated), the legacy
// chain runs and rejects the JWT.
func TestServeMCP_IssuerGatedToolsetBackend_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, mcpServer, issuerID := seedIssuerGatedToolsetMCPEndpoint(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "public")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID)

	subject := urn.NewAnonymousSubject(uuid.NewString())
	accessToken := mintIssuerGatedAccessToken(t, ctx, ti, slug, endpoint, issuerID, subject)

	// Drive ServeMCP with the minted bearer. Without the legacy-auth
	// skip fix, ServeToolsetResolved's legacy chain runs and 401s the
	// JWT it doesn't recognise.
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(accessToken), []byte(initializeBody))
	require.NotEqual(t, http.StatusUnauthorized, rr.Code, "issuer-gated bearer must not be rejected by the legacy auth chain inside ServeToolsetResolved; body=%s", rr.Body.String())
	require.Equal(t, http.StatusOK, rr.Code, "ServeMCP should respond 200; body=%s", rr.Body.String())
}

// TestServeMCP_IssuerGatedRemoteBackend_Private_HappyPath exercises the
// private-visibility branch of serveRemoteBackend. Without the
// `if !issuerGated` guard around RequirePrivateIdentityAuth the JWT
// would be rejected the same way today's pre-fix code rejected the
// public branch. The bearer is minted against a user subject (private
// endpoints route through the IDP rather than stamping anonymous).
func TestServeMCP_IssuerGatedRemoteBackend_PrivateHappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	var gotMethod string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	slug, mcpServer, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "private")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID)

	// Private endpoints route through the IDP, which stamps a user
	// subject (not anonymous) onto the cached challenge state.
	subject := urn.NewUserSubject("user_" + uuid.NewString()[:8])
	accessToken := mintIssuerGatedAccessToken(t, ctx, ti, slug, endpoint, issuerID, subject)

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(accessToken), []byte(initializeBody))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked within 5s; status=%d body=%s", rr.Code, rr.Body.String())
	}
	require.NotEqual(t, http.StatusUnauthorized, rr.Code, "issuer-gated bearer on a private endpoint must not be re-rejected by RequirePrivateIdentityAuth; body=%s", rr.Body.String())
	require.Equal(t, http.StatusOK, rr.Code, "ServeMCP should proxy through; body=%s", rr.Body.String())
	require.Equal(t, http.MethodPost, gotMethod)
}

// TestServeMCP_IssuerGated_CrossIssuerTokenRejected asserts the
// audience-binding invariant: a bearer minted against issuer A must be
// rejected when presented at issuer B's endpoint, even if both endpoints
// are issuer-gated and otherwise structurally identical. The check
// lives inside userSessionSigner.ValidateBearer (audience claim
// equality) and is load-bearing for cross-tenant isolation.
func TestServeMCP_IssuerGatedRemoteBackend_CrossIssuerTokenRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	// Endpoint A: where the token is minted.
	slugA, mcpServerA, issuerA := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")
	mcpEndpointA, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slugA,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpointA := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpointA, &mcpServerA, project.OrganizationID)

	// Endpoint B: a sibling under a different issuer.
	slugB, _, _ := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")
	require.NotEqual(t, slugA, slugB)

	subject := urn.NewAnonymousSubject(uuid.NewString())
	accessToken := mintIssuerGatedAccessToken(t, ctx, ti, slugA, endpointA, issuerA, subject)

	// The bearer is bound to endpointA's audience URN. Presenting it
	// at endpointB must 401 with the same WWW-Authenticate shape
	// missing-auth requests get.
	rr := runHandler(t, ctx, ti, http.MethodPost, slugB, bearer(accessToken), []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code, "issuer-A bearer must not authorise issuer-B endpoint; body=%s", rr.Body.String())
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth)
	require.Contains(t, wwwAuth, "/x/mcp/"+slugB, "challenge URL must point at endpoint B's metadata, not A's")
}

// TestServeMCP_IssuerGated_RegisterRouteAdapter is a smoke test for the
// xmcp.Attach route wiring: it drives POST /x/mcp/{slug}/register
// through the full chi mux that xmcp.Attach builds, instead of calling
// mcp.Service.ServeRegister directly. Catches route-level integration
// bugs — wrong chi URL param name, wrong method bound, wrong adapter
// dispatched — that would otherwise slip past the post-resolution
// happy-path tests. The other OAuth adapters (authorize, consent,
// token, revoke) are structurally identical so a single smoke test
// covers the family.
func TestAttach_OAuthRegisterRoute_MintsClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	body := []byte(`{"client_name":"adapter smoke test","redirect_uris":["http://localhost:3000/callback"],"token_endpoint_auth_method":"none"}`)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "register adapter should return 201; body=%s", w.Body.String())
	var resp struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ClientID, "register response must include a minted client_id")
}

// TestAttach_OAuthAuthorizeRoute_RedirectsToConsent drives the
// chi.Attach-wired GET /x/mcp/{slug}/authorize and verifies the
// authorize adapter dispatches to mcp.Service.ServeAuthorize correctly:
// a valid request on a public issuer-gated endpoint should 302 to the
// consent URL (/x/mcp/{slug}/connect). Catches a route-wiring regression
// where the wrong adapter or wrong chi URL param name would be observed
// as a 500 or 404 here.
func TestAttach_OAuthAuthorizeRoute_RedirectsToConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	// Pre-register a client so authorize finds it.
	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "authorize-smoke",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", "authorize-smoke-state")

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x/mcp/"+slug+"/authorize?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code, "authorize adapter must 302 to consent; body=%s", w.Body.String())
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/x/mcp/"+slug+"/connect", "302 must point at the /x/mcp consent URL")
}

// TestAttach_OAuthConsentRoute_RendersForm drives the chi.Attach-wired
// GET /x/mcp/{slug}/connect against a pre-stamped AuthnChallengeState
// and verifies the consent adapter dispatches to ServeConsent (renders
// 200 with the consent template HTML). Catches route-wiring regressions
// distinct from the authorize/token paths.
func TestAttach_OAuthConsentRoute_RendersForm(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "consent-smoke",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	// Stamp an AuthnChallengeState the way HandleAuthorize would have.
	challengeID := uuid.NewString()
	anonymous := urn.NewAnonymousSubject(uuid.NewString())
	authnCache := cache.NewTypedObjectCache[mcp.AuthnChallengeState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        slug,
			CustomDomainID: uuid.NullUUID{},
			McpServerID:    uuid.NullUUID{}, // legacy /mcp shape — ServeConsent re-resolves via mcpSlug
			RouteBase:      "x/mcp",
		},
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		Subject:             &anonymous,
		CreatedAt:           time.Now(),
	}))

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x/mcp/"+slug+"/connect?state="+challengeID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "consent adapter must render 200; body=%s", w.Body.String())
	require.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

// TestAttach_OAuthTokenRoute_MintsAccessToken drives the chi.Attach-wired
// POST /x/mcp/{slug}/token with a pre-seeded UserSessionGrant and verifies
// the token adapter dispatches to ServeToken (mints a Bearer access
// token). Catches route-wiring regressions distinct from the upstream
// adapter paths.
func TestAttach_OAuthTokenRoute_MintsAccessToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "token-smoke",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code := "auth-code-" + uuid.NewString()
	subject := urn.NewAnonymousSubject(uuid.NewString())
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		Subject:             subject,
		CreatedAt:           time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "token adapter must mint a Bearer; body=%s", w.Body.String())
	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.AccessToken)
	require.Equal(t, "Bearer", resp.TokenType)
}

// TestAttach_OAuthRevokeRoute_HandlesUnknownTokenAsSuccess drives the
// chi.Attach-wired POST /x/mcp/{slug}/revoke against a non-existent
// token and verifies the revoke adapter dispatches to ServeRevoke
// (which per RFC 7009 §2.2 must return 200 for unknown tokens, not 4xx).
// Catches a route-wiring regression that would surface as the wrong
// adapter being invoked.
func TestAttach_OAuthRevokeRoute_HandlesUnknownTokenAsSuccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	// Revoke requires client authentication (RFC 7009 §2.1); seed a
	// public client (token_endpoint_auth_method=none).
	clientID := "test-client-" + uuid.NewString()
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuerID,
		ClientID:            clientID,
		ClientName:          "revoke-smoke",
		RedirectUris:        []string{"http://localhost:3000/callback"},
	})
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	form := url.Values{}
	form.Set("token", "definitely-not-a-real-token")
	form.Set("token_type_hint", "access_token")
	form.Set("client_id", clientID)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "revoke adapter must return 200 for unknown tokens (RFC 7009 §2.2); body=%s", w.Body.String())
}

// TestRequireUserSessionIssuer_DanglingFKReturnsNotFound asserts the
// behaviour of mcp.Service.RequireUserSessionIssuer when the
// user_session_issuer FK target has been deleted out from under an
// in-memory ResolvedMcpEndpoint snapshot — the race window between
// loading mcp_servers and using the issuer_id. The schema's
// ON DELETE SET NULL on mcp_servers.user_session_issuer_id only
// triggers on the next write; in-flight requests still hold the old
// UUID. The defensive lookup must surface CodeNotFound so the request
// can fail closed.
func TestRequireUserSessionIssuer_DanglingFKReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, mcpServer, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID)

	// Sanity check: the issuer FK resolves cleanly before deletion.
	require.NoError(t, ti.mcpService.RequireUserSessionIssuer(ctx, endpoint))

	// Soft-delete the issuer. GetUserSessionIssuerByID filters on
	// `deleted IS FALSE`, so the next call must miss.
	_, err = usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	err = ti.mcpService.RequireUserSessionIssuer(ctx, endpoint)
	require.Error(t, err, "dangling issuer FK must surface as a request-level error, not be silently ignored")
	require.Contains(t, err.Error(), "user_session_issuer not found", "error message should identify the dangling FK target")
}

// TestHandleIDPCallback_McpServerMismatch_Returns guard verifies the
// state-confusion check inside loadResolvedMcpEndpointByRef: if the
// cached EndpointRef.McpServerID no longer matches the mcp_server the
// addressed mcp_endpoint currently resolves to, the resumption is
// rejected. Triggered by an mcp_endpoint being re-pointed at a
// different mcp_server mid-flow.
func TestHandleIDPCallback_McpServerMismatchRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, issuerID := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	// Cache a challenge state whose McpServerID points at a different
	// mcp_server than the live mcp_endpoint resolves to. The simplest
	// way to produce a different valid-shape UUID is to mint one
	// uncorrelated with the endpoint — the guard compares by UUID
	// equality, not row existence.
	staleServerID := uuid.New()

	authnCache := cache.NewTypedObjectCache[mcp.AuthnChallengeState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	challengeID := uuid.NewString()
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        slug,
			CustomDomainID: uuid.NullUUID{},
			McpServerID:    uuid.NullUUID{UUID: staleServerID, Valid: true},
			RouteBase:      "x/mcp",
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	}))

	q := url.Values{
		"state": {challengeID},
		"code":  {"idp-auth-code-mismatch"},
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x/mcp/idp_callback?"+q.Encode(), nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext()))

	w := httptest.NewRecorder()
	err := ti.mcpService.HandleIDPCallback(w, req)
	require.Error(t, err, "callback against a mismatching mcp_server ref must fail")
	require.Contains(t, err.Error(), "does not match", "guard error message should describe the mismatch")
}

// TestServeMCP_PublicRemoteBackend_GetForwardsToUpstream exercises the GET
// leg of the MCP Streamable HTTP transport (spec §3.3 "Listening for
// Messages from the Server"). The chi mux registers GET / DELETE / POST
// on /x/mcp/{slug} and [Service.buildProxy] wires up the proxy.Get
// branch; this test confirms the GET method makes it through ServeMCP
// unchanged and the proxy forwards to upstream. Catches a regression
// where GET would be dropped at any layer between Attach and the proxy.
func TestServeMCP_PublicRemoteBackend_GetForwardsToUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotMethod, gotSession string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSession = r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {}\n\n"))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")

	rr := runHandlerWithHeaders(t, ctx, ti, http.MethodGet, slug, "", nil, map[string]string{"Mcp-Session-Id": "sse-session-1"})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked within 5s; status=%d", rr.Code)
	}
	require.Equal(t, http.StatusOK, rr.Code, "GET must proxy through; body=%s", rr.Body.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "sse-session-1", gotSession, "Mcp-Session-Id must be forwarded to upstream on GET")
}

// TestServeMCP_PublicRemoteBackend_ToolsCallForwardsToUpstream exercises
// the full request → proxy → interceptor pipeline for a tools/call.
// Previous tests only fire initialize through the proxy; this one
// catches regressions in the per-tool interceptor chain wired up by
// [Service.buildProxy] (counter, usage limits, shadowmcp validate-strip,
// usage tracking response) for public visibility.
func TestServeMCP_PublicRemoteBackend_ToolsCallForwardsToUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{
		Tools: []testmcp.Tool{{
			Name:        "echo",
			Description: "Echo the provided message",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
				"required": []any{"message"},
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{{"type": "text", "text": "echoed"}},
			},
		}},
	})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "public")

	// Initialize first to get a session id, then drive a tools/call on
	// that session so the proxy's per-tool interceptor pipeline gets to
	// run.
	initResp := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusOK, initResp.Code, "initialize body=%s", initResp.Body.String())
	sessionID := initResp.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	toolsCallBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	rr := runHandlerWithHeaders(t, ctx, ti, http.MethodPost, slug, "", toolsCallBody, map[string]string{"Mcp-Session-Id": sessionID})
	require.Equal(t, http.StatusOK, rr.Code, "tools/call body=%s", rr.Body.String())
	require.Contains(t, rr.Body.String(), "echoed", "upstream tool response must be relayed back")
}

// TestServeMCP_CustomDomainMismatchReturns404 covers the resolution
// scoping in [Service.resolveMCPEndpointAndServer]: an mcp_endpoint
// registered against a custom domain must not resolve for a request
// arriving without that domain's context, even when the slug matches.
// Catches a regression in the `custom_domain_id IS NOT DISTINCT FROM $2`
// predicate or any future refactor that drops the domain scoping.
//
// The schema's UNIQUE (organization_id) on custom_domains prevents the
// "two domains in one org" framing — testing platform-context vs
// domain-context is equivalent (each scope must not bleed into the
// other) and uses the same predicate.
func TestServeMCP_CustomDomainMismatchReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	domain := seedCustomDomain(t, ctx, ti, authCtx.ActiveOrganizationID, "xmcp-cd-mismatch-"+uuid.NewString()[:8]+".example.com")

	toolsetSlug := "tsl-" + uuid.NewString()
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "cd-mismatch-test",
		Slug:                   toolsetSlug,
		Description:            pgtype.Text{String: "custom domain mismatch test", Valid: true},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: toolsetSlug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	slug, _ := seedToolsetMCPEndpointOnDomain(t, ctx, ti, *authCtx.ProjectID, toolset, "public", uuid.NullUUID{UUID: domain.ID, Valid: true})

	// Request arriving without a custom-domain context must miss the
	// domain-scoped endpoint.
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code, "endpoint scoped to a custom domain must not resolve on the platform domain")
}

// TestServeMCP_IssuerGatedToolsetBackend_PrivateHappyPath is the
// private-visibility companion of
// [TestServeMCP_IssuerGatedToolsetBackend_HappyPath]. Without the
// `skipIssuerGate` + visibility-skip fix on the toolset branch, a
// private toolset-backed issuer-gated endpoint would be rejected by
// the legacy identity-auth chain inside ServeToolsetResolved even
// though ApplyIssuerGate already validated the JWT.
func TestServeMCP_IssuerGatedToolsetBackend_PrivateHappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, mcpServer, issuerID := seedIssuerGatedToolsetMCPEndpoint(t, ctx, ti, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "private")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID)

	// Private endpoints route through the IDP and stamp user subjects.
	subject := urn.NewUserSubject("user_" + uuid.NewString()[:8])
	accessToken := mintIssuerGatedAccessToken(t, ctx, ti, slug, endpoint, issuerID, subject)

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(accessToken), []byte(initializeBody))
	require.NotEqual(t, http.StatusUnauthorized, rr.Code, "issuer-gated bearer on a private toolset must not be re-rejected; body=%s", rr.Body.String())
	require.Equal(t, http.StatusOK, rr.Code, "ServeMCP should respond 200; body=%s", rr.Body.String())
}
