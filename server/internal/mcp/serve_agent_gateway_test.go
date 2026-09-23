package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// agentGatewayFixture is one agent, one key, and two private member servers in
// the caller's project: the agent is granted one of them and not the other.
type agentGatewayFixture struct {
	agent          agentsrepo.Agent
	token          string
	granted        string
	grantedProject string
	denied         string
	open           string
}

// mcpServerBySlug resolves the id a proxy-backed member's mcp:connect grant is
// keyed on. Grants for toolset-backed servers key on the toolset instead.
func mcpServerBySlug(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{
		Slug:      conv.ToPGText(slug),
		ProjectID: projectID,
	})
	require.NoError(t, err)
	return server.ID
}

func seedAgentGateway(t *testing.T, ctx context.Context, ti *testInstance) agentGatewayFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.features.SetFlag(feature.FlagAgentManagement, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, authCtx.ActiveOrganizationID, true)

	// Stored membership exists but must not matter: an agent gateway derives
	// its members from grants, so the meta server below is a decoy.
	metaSlug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, metaSlug, uuid.Nil)
	grantedSlug := "granted-" + uuid.NewString()[:8]
	deniedSlug := "denied-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "granted member", grantedSlug, 0, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "denied member", deniedSlug, 1, mcpservers.VisibilityPrivate)
	publicSlug := "openmember-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "public member", publicSlug, 2, mcpservers.VisibilityPublic)

	grantedServer := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, grantedSlug)
	project, perr := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, perr)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// Runtime admission intersects the credential's policy with the agent's own
	// and with its owner's, so a grant the owner lacks admits nothing. Seed
	// both, as the consent fixtures do.
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedServer.String())
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, grantedServer)

	// Credential policy: the immutable ceiling on this one key.
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   grantedServer.String(),
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		},
	}})
	require.NoError(t, err)
	delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)

	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		CreatedByUserID:        authCtx.UserID,
		Name:                   "gateway key " + uuid.NewString()[:8],
		KeyPrefix:              prefix,
		KeyHash:                hash,
		SubjectUrn:             pgtype.Text{String: urn.NewAgentSubject(agent.ID).String(), Valid: true},
		DelegatedGrants:        delegated,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	return agentGatewayFixture{
		agent: agent, token: token,
		granted: grantedSlug, grantedProject: project.Slug,
		denied: deniedSlug, open: publicSlug,
	}
}

// serveAgentGatewayHTTP drives the handler the way the router does, including
// the {agentID} path parameter. The error is returned rather than swallowed:
// a rejected request never writes to the recorder, so its status would
// otherwise read as the recorder's default 200.
func serveAgentGatewayHTTP(t *testing.T, ti *testInstance, agentID, token string, body []byte) (*httptest.ResponseRecorder, error) {
	t.Helper()
	return serveAgentGatewayHTTPOn(t, t.Context(), ti, agentID, token, body)
}

// serveAgentGatewayHTTPOn is serveAgentGatewayHTTP with the request context
// chosen by the caller, for the ingress-surface and custom-domain cases where
// what is in that context is the thing under test.
func serveAgentGatewayHTTPOn(t *testing.T, ctx context.Context, ti *testInstance, agentID, token string, body []byte) (*httptest.ResponseRecorder, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentID", agentID)
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServeAgentGateway(w, r)
	if err != nil {
		return w, fmt.Errorf("serve agent gateway: %w", err)
	}
	return w, nil
}

func requireAgentGatewayCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// The whole point of the surface: membership follows the agent's grants, so a
// server it cannot connect to never appears, even though both are stored
// members of a gateway in the same project.
func TestServeAgentGateway_ListsOnlyGrantedServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), fx.granted)
	require.NotContains(t, w.Body.String(), fx.denied)
	// A public server is anonymously reachable by anyone, so withholding it
	// here would hide something the agent can already reach without Gram.
	// Listing it is deliberate, not an admission leak.
	require.Contains(t, w.Body.String(), fx.open)
}

// An agent gateway answers initialize without an OAuth issuer gate: the agent
// key is the credential, so there is no consent page to send anyone to.
func TestServeAgentGateway_Initialize(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Result)
}

func TestServeAgentGateway_PrivateTunnelReceivesAgentAssertion(t *testing.T) {
	t.Parallel()
	issuer, publicKey := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	fx := seedAgentGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email, "the agent's owner has a human profile")
	projectID := *authCtx.ProjectID
	resource := "https://mcp.internal.example.com/agent/"
	serverID := mcpServerBySlug(t, ctx, ti, projectID, fx.granted)
	tunnel, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: fx.granted,
		KeyHash: "hash-" + fx.granted, KeyPrefix: "test", ResourceIdentifier: conv.ToPGText(resource),
	})
	require.NoError(t, err)
	// Keep the granted wrapper identity and its delegated policy, but serve
	// this member through a tunnel instead of its remote backend.
	_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		ID: serverID, ProjectID: projectID,
		Name: conv.ToPGText(fx.granted), Slug: conv.ToPGText(fx.granted),
		TunneledMcpServerID: conv.ToNullUUID(tunnel.ID), Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnel.ID.String(), upstream.URL, time.Hour))
	response, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "execute_tool",
		"arguments": map[string]any{
			"name": fx.grantedProject + "." + fx.granted + "--ping", "arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
	text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
	require.False(t, isError, text)
	require.Contains(t, text, "pong through the tunnel")
	headers, _ := tunnelForwards(gateway)
	require.GreaterOrEqual(t, len(headers), 3)
	for _, header := range headers {
		token, err := jwt.Parse(header.Get(mcpauthz.Header), func(*jwt.Token) (any, error) { return publicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("https://gram.example"), jwt.WithAudience(resource), jwt.WithExpirationRequired())
		require.NoError(t, err)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(t, ok)
		require.Equal(t, urn.NewAgentSubject(fx.agent.ID).String(), claims["sub"])
		require.Equal(t, "agent", claims["principal_type"])
		require.Equal(t, "mcp_request", claims["purpose"])
		require.Equal(t, authCtx.ActiveOrganizationID, claims["organization_id"])
		require.Equal(t, projectID.String(), claims["project_id"])
		require.Equal(t, serverID.String(), claims["mcp_server_id"])
		require.Equal(t, tunnel.ID.String(), claims["tunneled_mcp_server_id"])
		require.NotContains(t, claims, "email")
		require.NotContains(t, claims, "email_verified")
		require.NotContains(t, claims, "user_id")
	}
}

func TestServeAgentGateway_RejectsMissingAndForeignCredentials(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	other := seedAgentGateway(t, ctx, ti)

	body := makeInitializeBody()

	// No credential at all.
	_, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), "", body)
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)

	// A live key, but for a different agent: the address is wrong, so this must
	// not read as a denial that confirms the gateway exists.
	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), other.token, body)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)

	// A malformed agent id never reaches authentication.
	_, err = serveAgentGatewayHTTP(t, ti, "not-a-uuid", fx.token, body)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}

// Slugs are unique per project, not per organization, so an agent reaching two
// projects can meet the same slug twice. Qualifying by project is what keeps a
// qualified name pointing at one member.
func TestServeAgentGateway_QualifiesMemberSlugsByProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	second, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "second",
		Slug:           "second-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	// Deliberately the same slug as the first project's granted member, and
	// public so it is admitted without a second set of grants.
	metaTwo := createMetaMcpEndpoint(t, ctx, ti.conn, second.ID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	seedMetaMember(t, ctx, ti.conn, second.ID, metaTwo.ID, "twin member", fx.granted, 0, mcpservers.VisibilityPublic)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}))
	require.NoError(t, err)

	body := w.Body.String()
	require.Contains(t, body, second.Slug+"."+fx.granted)
	require.Contains(t, body, fx.grantedProject+"."+fx.granted)
}

// An org that pins its MCP traffic to a custom domain with an IP allowlist
// enforces that allowlist at its own ingress, so reaching the same servers on
// the platform hostname is exactly the bypass the lockdown exists to close. A
// valid agent key is not an exemption from it.
func TestServeAgentGateway_CustomDomainLockdownBlocksPlatformHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "agent-lockdown-" + uuid.NewString()[:8] + ".example.com",
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)

	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeForbidden)

	// Arriving on the custom domain, the allowlist has already been applied at
	// the edge, so the app must not block a second time.
	domainCtx := customdomains.WithContext(t.Context(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	w, err := serveAgentGatewayHTTPOn(t, domainCtx, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Another organization's domain proves only that organization's allowlist
	// was applied, so it is not an exemption from this one's lockdown.
	foreignCtx := customdomains.WithContext(t.Context(), &customdomains.Context{
		OrganizationID: "org_" + uuid.NewString(),
		Domain:         "foreign-" + uuid.NewString()[:8] + ".example.com",
		DomainID:       uuid.New(),
	})
	_, err = serveAgentGatewayHTTPOn(t, foreignCtx, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeForbidden)
}

// A custom domain with no allowlist is the ordinary dual-serve case and must
// not lock the platform host down.
func TestServeAgentGateway_CustomDomainWithoutAllowlistStillServes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "agent-open-" + uuid.NewString()[:8] + ".example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A member marked private_only is served on the private ingress and nowhere
// else. The gateway is mounted on both listeners, so membership has to be
// matched against the surface the request actually arrived on — otherwise the
// public gateway both advertises and dispatches to a server whose whole point
// is that it is unreachable from there.
func TestServeAgentGateway_ExcludesPrivateOnlyMembersOnPublicIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	qualified := fx.grantedProject + "." + fx.granted

	setMode := func(mode pgtype.Text) {
		t.Helper()
		rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
			NetworkAccessMode: mode,
			ID:                mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.granted),
			ProjectID:         *authCtx.ProjectID,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
	}

	listBody := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	})

	setMode(pgtype.Text{String: string(networkaccess.ModePrivateOnly), Valid: true})

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, listBody)
	require.NoError(t, err)
	require.NotContains(t, w.Body.String(), qualified, "a private_only member must not be listed on the public ingress")

	// Not merely hidden: drill-down must miss identically, or the listing would
	// be the only thing enforcing the mode.
	w, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "describe_server",
		"arguments": map[string]any{"server": qualified},
	}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "unknown server")

	// dual serves both surfaces, so the same member reappears: the filter keys
	// on the mode, not on the column merely being set.
	setMode(pgtype.Text{String: string(networkaccess.ModeDual), Valid: true})

	w, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, listBody)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), qualified)
}

// The inverse of the public case: on the private ingress a private_only member
// is exactly what should be reachable.
func TestServeAgentGateway_ListsPrivateOnlyMembersOnPrivateIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: string(networkaccess.ModePrivateOnly), Valid: true},
		ID:                mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.granted),
		ProjectID:         *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	privateCtx := requestorigin.WithContext(t.Context(), requestorigin.Origin{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://gram.internal.example",
		OrganizationID:   authCtx.ActiveOrganizationID,
		NetworkIngressID: uuid.New(),
		NetworkIdentity:  nil,
	})
	w, err := serveAgentGatewayHTTPOn(t, privateCtx, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), fx.grantedProject+"."+fx.granted)
	// public_only members are the ones excluded here.
	require.NotContains(t, w.Body.String(), fx.open)
}
