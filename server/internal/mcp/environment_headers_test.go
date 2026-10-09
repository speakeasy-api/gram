package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Synthetic environment header values. A failing assertion may print them.
const (
	envProdTenant      = "synthetic-tenant-prod"
	envSandboxTenant   = "synthetic-tenant-sandbox"
	envProdInstance    = "https://prod.instance.invalid"
	envSandboxInstance = "https://sandbox.instance.invalid"
	envSecretValue     = "synthetic-env-secret-value"
)

type envEntry struct {
	name   string
	value  string
	secret bool
}

type seededEnvironment struct {
	id   uuid.UUID
	slug string
}

// seedEnvironment creates an environment in projectID with entries written
// through the encryption boundary.
func seedEnvironment(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, entries ...envEntry) seededEnvironment {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "env-" + uuid.NewString()[:8]
	env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		Description:    conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)
	if len(entries) > 0 {
		params := environmentsrepo.CreateEnvironmentEntriesParams{EnvironmentID: env.ID, Names: nil, Values: nil, IsSecrets: nil}
		for _, e := range entries {
			params.Names = append(params.Names, e.name)
			params.Values = append(params.Values, e.value)
			params.IsSecrets = append(params.IsSecrets, e.secret)
		}
		_, err = environments.NewEnvironmentEntries(ti.logger, ti.conn, ti.enc, nil).CreateEnvironmentEntries(ctx, params)
		require.NoError(t, err)
	}
	return seededEnvironment{id: env.ID, slug: slug}
}

// linkEnvironment sets an MCP server's environment link directly, the state
// PR5's authorized mcpServers.update path writes.
func linkEnvironment(t *testing.T, ctx context.Context, ti *testInstance, projectID, serverID uuid.UUID, environmentID uuid.NullUUID) {
	t.Helper()
	q := mcpserversrepo.New(ti.conn)
	server, err := q.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err)
	_, err = q.UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                  server.Name,
		Slug:                  server.Slug,
		EnvironmentID:         environmentID,
		UserSessionIssuerID:   server.UserSessionIssuerID,
		RemoteMcpServerID:     server.RemoteMcpServerID,
		TunneledMcpServerID:   server.TunneledMcpServerID,
		ToolsetID:             server.ToolsetID,
		UnproxiedMcpServerID:  server.UnproxiedMcpServerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID,
		Visibility:            server.Visibility,
		NetworkAccessModeSet:  false,
		NetworkAccessMode:     server.NetworkAccessMode,
		ID:                    server.ID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
}

// metaMemberServerID returns the MCP server of the gateway member at sortOrder.
func metaMemberServerID(t *testing.T, ctx context.Context, ti *testInstance, projectID, metaID uuid.UUID, sortOrder int32) uuid.UUID {
	t.Helper()
	members, err := metamcprepo.New(ti.conn).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{MetaMcpServerID: metaID, ProjectID: projectID})
	require.NoError(t, err)
	for _, m := range members {
		if m.SortOrder == sortOrder {
			return m.McpServerID
		}
	}
	require.FailNow(t, "no gateway member at sort order", sortOrder)
	return uuid.Nil
}

// createRemoteHeader stores a static source header on a remote MCP server.
func createRemoteHeader(t *testing.T, ctx context.Context, ti *testInstance, projectID, remoteServerID uuid.UUID, name, value string) {
	t.Helper()
	_, err := remotemcp.NewHeaders(ti.logger, ti.conn, ti.enc).CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
		Name: name, Description: conv.ToPGText(""), IsRequired: true, IsSecret: false,
		Value: conv.ToPGText(value), ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
		RemoteMcpServerID: remoteServerID, ProjectID: projectID,
	})
	require.NoError(t, err)
}

func linkTo(env seededEnvironment) uuid.NullUUID {
	return uuid.NullUUID{UUID: env.id, Valid: true}
}

// addPublicSibling adds a second public MCP server on the fixture's tunnel.
func addPublicSibling(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	sibling, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("sandbox"),
		Slug:                conv.ToPGText("sandbox-" + uuid.NewString()[:8]),
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, ti.conn, projectID)),
		TunneledMcpServerID: conv.ToNullUUID(tunnelID),
		Visibility:          "public",
	})
	require.NoError(t, err)
	slug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(sibling.ID), Slug: slug,
	})
	require.NoError(t, err)
	return sibling.ID, slug
}

func servePublicInitialize(t *testing.T, ti *testInstance, slug string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := publicTunnelRequest(slug, makeInitializeBody(), "")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	err := ti.service.ServePublic(w, req)
	if err != nil {
		// Mirror the HTTP error mapping so assertions read a status.
		writeServeError(w, err)
	}
	return w
}

// writeServeError records a handler error as a non-2xx response for tests that
// expect a refusal.
func writeServeError(w *httptest.ResponseRecorder, err error) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.WriteString(err.Error())
}

func requireNotContainsValues(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, v := range values {
		require.NotContains(t, body, v)
	}
}

// Two MCP servers on one tunnel, each linked to its own environment, send
// their own values for the same header; the tunnel-wide source header is
// replaced, other source headers still apply, and a client-supplied
// Gram-Environment cannot select another environment.
func TestEnvironmentHeaders_TwoServersOnOneTunnel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	seedTunnelHeaders(t, ctx, ti, projectID, fixture.tunnelID, false)
	siblingID, siblingSlug := addPublicSibling(t, ctx, ti, projectID, fixture.tunnelID)

	prod := seedEnvironment(t, ctx, ti, projectID,
		envEntry{name: "MCP_HEADER_X-Jamf-Tenant", value: envProdTenant, secret: false},
		envEntry{name: "MCP_HEADER_X-Instance-Url", value: envProdInstance, secret: false},
		envEntry{name: "UNRELATED_SECRET", value: "synthetic-unrelated", secret: true},
	)
	sandbox := seedEnvironment(t, ctx, ti, projectID,
		envEntry{name: "MCP_HEADER_X-Jamf-Tenant", value: envSandboxTenant, secret: true},
		envEntry{name: "MCP_HEADER_X-Instance-Url", value: envSandboxInstance, secret: false},
	)
	linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, linkTo(prod))
	linkEnvironment(t, ctx, ti, projectID, siblingID, linkTo(sandbox))

	w := servePublicInitialize(t, ti, fixture.endpointSlug, map[string]string{"Gram-Environment": sandbox.slug})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sid := w.Header().Get("Mcp-Session-Id")
	prodForward := gateway.lastForward()
	require.Equal(t, envProdTenant, prodForward.Get("X-Jamf-Tenant"))
	require.Equal(t, envProdInstance, prodForward.Get("X-Instance-Url"))
	require.Equal(t, headerTestSecret, prodForward.Get("X-Api-Key"), "unreplaced source headers still apply")
	require.Equal(t, "eu-west", prodForward.Get("X-Jamf-Region"))
	require.Empty(t, prodForward.Values("Gram-Environment"))
	requireNoSpeakeasyCredentialsForwarded(t, prodForward)

	w = servePublicInitialize(t, ti, siblingSlug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sandboxForward := gateway.lastForward()
	require.Equal(t, envSandboxTenant, sandboxForward.Get("X-Jamf-Tenant"))
	require.Equal(t, envSandboxInstance, sandboxForward.Get("X-Instance-Url"))

	// The pinned public session continuation sends the same values.
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(fixture.endpointSlug, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), sid)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, envProdTenant, gateway.lastForward().Get("X-Jamf-Tenant"))
	require.Equal(t, envProdInstance, gateway.lastForward().Get("X-Instance-Url"))
}

// Edits to the environment and to the link apply from the next request:
// an entry edit, removing an entry (the source value applies again),
// unlinking, relinking, and deleting the environment (which refuses rather
// than falling back to the source's values).
func TestEnvironmentHeaders_ChangesApplyToTheNextRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	seedTunnelHeaders(t, ctx, ti, projectID, fixture.tunnelID, false)
	prod := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Jamf-Tenant", value: envProdTenant, secret: true})
	sandbox := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Jamf-Tenant", value: envSandboxTenant, secret: false})
	linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, linkTo(prod))
	entries := environments.NewEnvironmentEntries(ti.logger, ti.conn, ti.enc, nil)

	tenant := func() string {
		t.Helper()
		w := servePublicInitialize(t, ti, fixture.endpointSlug, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return gateway.lastForward().Get("X-Jamf-Tenant")
	}
	require.Equal(t, envProdTenant, tenant())

	require.NoError(t, entries.UpdateEnvironmentEntry(ctx, environmentsrepo.UpsertEnvironmentEntryParams{
		Name: "MCP_HEADER_X-Jamf-Tenant", Value: "synthetic-tenant-prod-2", IsSecret: true, EnvironmentID: prod.id, ProjectID: projectID,
	}))
	require.Equal(t, "synthetic-tenant-prod-2", tenant())

	require.NoError(t, entries.DeleteEnvironmentEntry(ctx, environmentsrepo.DeleteEnvironmentEntryParams{EnvironmentID: prod.id, Name: "MCP_HEADER_X-Jamf-Tenant"}))
	require.Equal(t, "tenant-1", tenant(), "removing the entry restores the source value")

	linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, linkTo(sandbox))
	require.Equal(t, envSandboxTenant, tenant())

	linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.Equal(t, "tenant-1", tenant())

	linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, linkTo(sandbox))
	_, err := environmentsrepo.New(ti.conn).DeleteEnvironment(ctx, environmentsrepo.DeleteEnvironmentParams{Slug: sandbox.slug, ProjectID: projectID})
	require.NoError(t, err)

	before := gateway.forwardCount()
	w := servePublicInitialize(t, ti, fixture.endpointSlug, nil)
	require.NotEqual(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "environment headers are misconfigured")
	require.Equal(t, before, gateway.forwardCount(), "a deleted linked environment makes no upstream call")
}

// An opted-in entry that cannot be sent refuses the request before any
// upstream call, without echoing its value, and a reserved name can never
// carry a Speakeasy credential upstream.
func TestEnvironmentHeaders_InvalidEntryRefuses(t *testing.T) {
	t.Parallel()

	for name, entry := range map[string]envEntry{
		"reserved":      {name: "MCP_HEADER_Gram-Key", value: envSecretValue, secret: true},
		"invalid value": {name: "MCP_HEADER_X-Foo", value: envSecretValue + "\r\nX-Injected: 1", secret: true},
		"empty value":   {name: "MCP_HEADER_X-Foo", value: "   ", secret: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := *authCtx.ProjectID
			gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
			fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
			env := seedEnvironment(t, ctx, ti, projectID, entry, envEntry{name: "MCP_HEADER_X-Ok", value: envProdTenant, secret: false})
			linkEnvironment(t, ctx, ti, projectID, fixture.mcpServerID, linkTo(env))

			w := servePublicInitialize(t, ti, fixture.endpointSlug, nil)
			require.NotEqual(t, http.StatusOK, w.Code)
			require.Contains(t, w.Body.String(), "environment headers are misconfigured")
			requireNotContainsValues(t, w.Body.String(), envSecretValue, entry.name)
			require.Zero(t, gateway.forwardCount())
		})
	}
}

// A private tunneled server sends its environment headers on the direct
// route.
func TestEnvironmentHeaders_TunneledPrivateDirect(t *testing.T) {
	t.Parallel()

	issuer, _ := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *auth.ProjectID
	sessionIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "env-direct-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug,
	})
	require.NoError(t, err)
	seedMetaMemberConnectGrant(t, ctx, ti.conn, auth.ActiveOrganizationID, serverID)
	seedTunnelHeaders(t, ctx, ti, projectID, tunnelID, false)
	env := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Jamf-Tenant", value: envProdTenant, secret: true})
	linkEnvironment(t, ctx, ti, projectID, serverID, linkTo(env))

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))

	bearer := mintMetaIssuerBearer(t, ti, slug, sessionIssuer, urn.NewUserSubject(auth.UserID))
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, bytes.NewReader(makeInitializeBody()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header["X_jamf_tenant"] = []string{"client-alias"}
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(response, request))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	forwarded := gateway.lastForward()
	require.Equal(t, []string{envProdTenant}, forwarded.Values("X-Jamf-Tenant"))
	require.Empty(t, forwarded.Values("X_jamf_tenant"), "the environment header owns its folded spellings")
	require.Equal(t, headerTestSecret, forwarded.Get("X-Api-Key"))
}

// The consent transport builds through the same tunnel path and sends the
// environment headers.
func TestEnvironmentHeaders_TunneledConsentTransport(t *testing.T) {
	t.Parallel()

	issuer, _ := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	_, sessionIssuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	projectID := sessionIssuer.ProjectID.UUID
	slug := "env-consent-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer.ID, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug})
	require.NoError(t, err)
	env := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Instance-Url", value: envProdInstance, secret: false})
	linkEnvironment(t, ctx, ti, projectID, serverID, linkTo(env))
	stateID, csrf := seedModernConsentChallenge(t, ctx, ti, sessionIssuer.ID, client, serverID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.AuthorizerUserID = state.Subject.ID
	state.AuthorizerImpersonated = new(false)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	seedMetaMemberConnectGrant(t, ctx, ti.conn, sessionIssuer.OrganizationID.String, serverID)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "x/mcp")
	require.NoError(t, err)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "test-backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))

	init := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrf, uuid.NewString(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, map[string]string{})
	require.Contains(t, init.Body.String(), "serverInfo")
	require.Equal(t, envProdInstance, gateway.lastForward().Get("X-Instance-Url"))
}

// A gateway member sends its own environment headers, and a member whose
// environment is misconfigured is isolated rather than failing the gateway.
func TestEnvironmentHeaders_TunneledMetaMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-env-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	goodGateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, challenge: ""}
	goodTunnel, _ := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Good member", "member-good", 0, "")
	goodServer := metaMemberServerID(t, ctx, ti, projectID, meta.ID, 0)
	goodGatewayServer := httptest.NewServer(goodGateway)
	t.Cleanup(goodGatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, goodTunnel.String(), goodGatewayServer.URL, time.Hour))
	goodEnv := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Instance-Url", value: envProdInstance, secret: true})
	linkEnvironment(t, ctx, ti, projectID, goodServer, linkTo(goodEnv))

	badGateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-2", backendSessionID: "backend-session-2", legacy: false, dead: false, challenge: ""}
	badTunnel, _ := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Bad member", "member-bad", 1, "")
	badServer := metaMemberServerID(t, ctx, ti, projectID, meta.ID, 1)
	badGatewayServer := httptest.NewServer(badGateway)
	t.Cleanup(badGatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, badTunnel.String(), badGatewayServer.URL, time.Hour))
	badEnv := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_Cookie", value: envSecretValue, secret: true})
	linkEnvironment(t, ctx, ti, projectID, badServer, linkTo(badEnv))

	subject := createTestUser(t, ctx, ti, "meta-env-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member-good--ping"))
	require.False(t, isError, text)
	require.Equal(t, envProdInstance, goodGateway.forwardFor(`"tools/call"`).Get("X-Instance-Url"))

	text, isError = metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member-bad--ping"))
	require.True(t, isError)
	require.Contains(t, text, "invalid environment header configuration")
	require.NotContains(t, text, envSecretValue)
	require.Zero(t, badGateway.forwardCount())
}

// A remote MCP server sends its environment headers on the direct route,
// replacing a source header of the same field and owning its folded
// spellings, and refuses without an upstream call when the environment is
// misconfigured.
func TestEnvironmentHeaders_RemoteDirect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	var (
		mu   sync.Mutex
		seen []http.Header
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"upstream","version":"1.0"}}}`))
	}))
	t.Cleanup(upstream.Close)
	calls := func() []http.Header {
		mu.Lock()
		defer mu.Unlock()
		return append([]http.Header(nil), seen...)
	}

	endpointSlug := "endpoint-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	mcpServer, remoteServer := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstream.URL, endpointSlug, "public", issuerID)
	createRemoteHeader(t, ctx, ti, projectID, remoteServer.ID, "X-Instance-Url", "source-instance")
	env := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Instance-Url", value: envProdInstance, secret: true})
	linkEnvironment(t, ctx, ti, projectID, mcpServer.ID, linkTo(env))
	token := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, map[string]string{"X_Instance_Url": "client-alias"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := calls()
	require.Len(t, got, 1)
	require.Equal(t, []string{envProdInstance}, got[0].Values("X-Instance-Url"))
	require.Empty(t, got[0].Values("X_Instance_Url"))

	bad := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Gram-Scope-Override", value: envSecretValue, secret: true})
	linkEnvironment(t, ctx, ti, projectID, mcpServer.ID, linkTo(bad))
	w, err = servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, nil)
	if err == nil {
		require.NotEqual(t, http.StatusOK, w.Code)
	} else {
		require.Contains(t, err.Error(), "environment headers are misconfigured")
		require.NotContains(t, err.Error(), envSecretValue)
	}
	require.Len(t, calls(), 1, "a misconfigured environment makes no upstream call")
}

// A remote gateway member sends its own environment headers.
func TestEnvironmentHeaders_RemoteMetaMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-env-remote-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)
	upstream := newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member", "member", 0, upstream.url)
	env := seedEnvironment(t, ctx, ti, projectID, envEntry{name: "MCP_HEADER_X-Instance-Url", value: envProdInstance, secret: true})
	linkEnvironment(t, ctx, ti, projectID, memberID, linkTo(env))

	subject := createTestUser(t, ctx, ti, "meta-env-remote-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)
	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.False(t, isError, text)

	var sawToolCall bool
	for _, req := range upstream.journal() {
		if req.rpcMethod != "" {
			require.Equal(t, envProdInstance, req.header.Get("X-Instance-Url"), req.rpcMethod)
			sawToolCall = sawToolCall || req.rpcMethod == "tools/call"
		}
	}
	require.True(t, sawToolCall)
}
