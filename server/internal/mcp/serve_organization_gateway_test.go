package mcp_test

import (
	"bytes"
	"context"
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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// organizationGatewayFixture is an organization with one organization-owned
// issuer and three member servers in its project: a private one the caller
// is granted, a private one it is not, and a public one.
type organizationGatewayFixture struct {
	organizationSlug string
	projectSlug      string
	issuerID         uuid.UUID
	subject          urn.SessionSubject
	granted          string
	denied           string
	open             string
}

func createOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:     conv.ToPGText(organizationID),
		Slug:               "org-gateway-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func seedOrganizationGateway(t *testing.T, ctx context.Context, ti *testInstance) organizationGatewayFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	// The members' stored gateway is a decoy: an organization gateway derives
	// its members from the caller's grants.
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	grantedSlug := "granted-" + uuid.NewString()[:8]
	deniedSlug := "denied-" + uuid.NewString()[:8]
	publicSlug := "openmember-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "granted member", grantedSlug, 0, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "denied member", deniedSlug, 1, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "public member", publicSlug, 2, mcpservers.VisibilityPublic)

	userID := seedConsentMember(t, ctx, ti, authCtx.ActiveOrganizationID)
	subject := urn.NewUserSubject(userID)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, grantedSlug).String())

	return organizationGatewayFixture{
		organizationSlug: authCtx.OrganizationSlug,
		projectSlug:      *authCtx.ProjectSlug,
		issuerID:         createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID),
		subject:          subject,
		granted:          grantedSlug,
		denied:           deniedSlug,
		open:             publicSlug,
	}
}

func mintOrganizationGatewayBearer(t *testing.T, ti *testInstance, fx organizationGatewayFixture) string {
	t.Helper()

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  fx.subject,
		Audience: ti.serverURL.String() + "/mcp/gateway/" + fx.organizationSlug,
		Issuer:   ti.serverURL.String() + "/mcp/gateway/" + fx.organizationSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fx.issuerID, fx.subject, jti)
	return token
}

// serveOrganizationGatewayHTTP drives the handler the way the router does,
// including the path parameter. The error is returned rather than swallowed:
// a rejected request never writes to the recorder, so its status would
// otherwise read as the recorder's default 200.
func serveOrganizationGatewayHTTP(t *testing.T, ctx context.Context, ti *testInstance, organizationSlug, token string, body []byte) (*httptest.ResponseRecorder, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+organizationSlug, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", organizationSlug)
	r = r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.ServeOrganizationGateway(w, r); err != nil {
		return w, fmt.Errorf("serve organization gateway: %w", err)
	}
	return w, nil
}

func listServersBody(t *testing.T) []byte {
	t.Helper()
	return makeMetaRPCBody(t, "tools/call", map[string]any{"name": "list_servers", "arguments": map[string]any{}})
}

func TestServeOrganizationGateway_ListsServersTheUserCanReach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// Members span projects, so every slug is qualified by its project's.
	require.Contains(t, w.Body.String(), fx.projectSlug+"."+fx.granted)
	require.Contains(t, w.Body.String(), fx.projectSlug+"."+fx.open)
	require.NotContains(t, w.Body.String(), fx.denied)
}

func TestServeOrganizationGateway_NoIssuerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, authCtx.OrganizationSlug, "", makeInitializeBody())

	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServeOrganizationGateway_SeveralIssuersIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	createOrganizationIssuer(t, ctx, ti, authCtx.ActiveOrganizationID)

	_, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))

	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServeOrganizationGateway_UnknownOrganizationIsNotFound(t *testing.T) {
	t.Parallel()
	_, ti := newTestMCPService(t)

	_, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, "no-such-org-"+uuid.NewString()[:8], "", makeInitializeBody())

	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServeOrganizationGateway_UnauthenticatedIsChallenged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, "", listServersBody(t))

	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/gateway/"+fx.organizationSlug)
}

// Slugs are unique per project, so two projects may each hold a server with
// the same slug. Both must be listed, each under its own project's name.
func TestServeOrganizationGateway_SameSlugInTwoProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherMeta := createMetaMcpEndpoint(t, ctx, ti.conn, otherProject.ID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	seedMetaMember(t, ctx, ti.conn, otherProject.ID, otherMeta.ID, "public twin", fx.open, 0, mcpservers.VisibilityPublic)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), fx.projectSlug+"."+fx.open)
	require.Contains(t, w.Body.String(), otherProject.Slug+"."+fx.open)
}

// The OAuth surface resolves the gateway by organization slug under its own
// route base, and every URL it advertises stays under that base.
func TestLoadResolvedMcpEndpointBySlug_OrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")

	require.NoError(t, err)
	require.True(t, endpoint.IsOrganizationGateway())
	require.Equal(t, fx.issuerID, endpoint.UserSessionIssuerID)
	require.Equal(t, uuid.Nil, endpoint.ProjectID)
	root, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.String()+"/mcp/gateway/"+fx.organizationSlug, root)
	callback, err := endpoint.IDPCallbackURL(ti.serverURL.String())
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.String()+"/mcp/idp_callback", callback)
}

// The gateway's consent page reaches clients in every project, and a client
// owned by another project starts its login in that project's tenancy, which
// is where the grant it produces is checked and stored.
func TestServeConsentAction_OrganizationGatewayConnectsAnotherProjectsClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	// A public server in that project authenticates through the client, so
	// the person reaches it and the client is theirs to connect.
	otherMeta := createMetaMcpEndpoint(t, ctx, ti.conn, otherProject.ID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	otherSlug := "other-public-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, otherProject.ID, otherMeta.ID, "other public", otherSlug, 0, mcpservers.VisibilityPublic)
	otherServer, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(otherSlug), ProjectID: otherProject.ID})
	require.NoError(t, err)
	client := createConsentRemoteClient(t, ctx, ti.conn, otherProject.ID, authCtx.ActiveOrganizationID, "orggw-"+uuid.NewString()[:8], "", []uuid.UUID{otherServer.UserSessionIssuerID.UUID, fx.issuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, authCtx.ActiveOrganizationID, otherProject.ID, []uuid.UUID{otherServer.UserSessionIssuerID.UUID}))

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:    "org-gateway-mcp-client",
		RedirectURI: "http://example.com/cb",
		CSRFToken:   "csrf-token",
		Subject:     &fx.subject,
		CreatedAt:   time.Now(),
	}))

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", client.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsentAction(w, req, endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())

	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	login, err := loginCache.Get(t.Context(), "remoteLogin:"+location.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, client, login.RemoteSessionClientID)
	require.Equal(t, otherProject.ID, login.ProjectID, "the login runs in the project that owns the client")
	require.Equal(t, "mcp/gateway", login.RouteBase, "the login returns to the gateway's consent page")
	require.Equal(t, fx.organizationSlug, login.McpSlug)
}

// Only people sign in to an organization gateway: an agent key is refused
// with the same challenge as no credential at all.
func TestServeOrganizationGateway_AgentKeyIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, ti.createTestAPIKey(ctx, t), listServersBody(t))

	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/gateway/"+fx.organizationSlug)
}

// A workload's assertion grant signs in a workload, so the gateway's token
// endpoint does not offer it.
func TestServeToken_OrganizationGatewayRefusesWorkloadAssertion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)

	form := url.Values{}
	form.Set("grant_type", oauthwire.GrantTypeJWTBearer)
	form.Set("assertion", "not-a-real-assertion")
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(w, req, endpoint))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unsupported_grant_type")
}

func postRegistration(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"client_name":"shadowed-endpoint","redirect_uris":["http://127.0.0.1:53682/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// An endpoint whose slug is "gateway" keeps its own POST routes, which the
// organization gateway's fixed segment would otherwise capture.
func TestOrganizationGatewayRoutes_ShadowedEndpointKeepsItsRoutes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service, nil)

	w := postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusNotFound, w.Code, "without such an endpoint the path names an organization")

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "gateway", issuerID)

	w = postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"client_id"`)
}

// The gateway's consent page lists only the connections behind servers the
// person can reach: a client whose only servers are hidden from them stays
// off the page, along with the service behind it.
func TestServeConsent_OrganizationGatewayListsOnlyReachableServersClients(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	serverIssuer := func(slug string) uuid.UUID {
		server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(slug), ProjectID: projectID})
		require.NoError(t, err)
		return server.UserSessionIssuerID.UUID
	}
	grantedIssuer := serverIssuer(fx.granted)
	deniedIssuer := serverIssuer(fx.denied)
	reachable := "reachable-" + uuid.NewString()[:8]
	hidden := "hidden-" + uuid.NewString()[:8]
	createConsentRemoteClient(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, reachable, "", []uuid.UUID{grantedIssuer, fx.issuerID})
	createConsentRemoteClient(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, hidden, "", []uuid.UUID{deniedIssuer, fx.issuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, authCtx.ActiveOrganizationID, projectID, []uuid.UUID{grantedIssuer, deniedIssuer}))

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		RedirectURI:     "http://example.com/cb",
		CSRFToken:       "csrf-token",
		Subject:         &fx.subject,
		CreatedAt:       time.Now(),
		FirstParty:      true,
		AutoConnectDone: true,
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+fx.organizationSlug+"/connect?state="+stateID, nil)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(w, req, endpoint))

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), reachable)
	require.NotContains(t, w.Body.String(), hidden)
}

// An organization whose custom domain locks its servers to an IP allowlist is
// refused on the platform host, so it is not served a gateway there.
func TestServeOrganizationGateway_CustomDomainLockdownIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "mcp-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)

	_, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))

	requireOopsCode(t, err, oops.CodeNotFound)
}
