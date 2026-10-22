package mcp_test

import (
	"bytes"
	"context"
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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
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

	featureQuery := featuresrepo.New(ti.conn)
	setting := featuresrepo.EnableFeatureParams{OrganizationID: authCtx.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err := featureQuery.EnableFeature(ctx, setting)
	require.NoError(t, err)
	_, err = featureQuery.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)

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

// mintOrganizationGatewayBearerWithPolicy mints an organization gateway bearer
// whose session carries the connection's consent policy.
func mintOrganizationGatewayBearerWithPolicy(t *testing.T, ctx context.Context, ti *testInstance, fx organizationGatewayFixture, mode metamcp.DiscoveryMode) string {
	t.Helper()

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{
		Resource:  "meta_mcp_server:" + endpoint.MetaMcpServerID.UUID.String(),
		Selection: nil,
		Gateway:   &toolfilter.GatewayOptions{DiscoveryMode: &mode},
	})
	require.NoError(t, err)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  fx.subject,
		Audience: ti.serverURL.String() + "/mcp/gateway/" + fx.organizationSlug,
		Issuer:   ti.serverURL.String() + "/mcp/gateway/" + fx.organizationSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	now := time.Now()
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: fx.issuerID,
		UserSessionClientID: uuid.NullUUID{},
		SubjectUrn:          fx.subject,
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText("test-bearer-" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       policy,
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
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
	// Slugs are plain unless another project's member shares one.
	require.Contains(t, w.Body.String(), `"slug":"`+fx.granted+`"`)
	require.Contains(t, w.Body.String(), `"slug":"`+fx.open+`"`)
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
func TestServeOrganizationGateway_DirectConnectionListsMemberTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	hosted := seedHostedMetaMember(t, ctx, ti, decoy.ID, "Hosted tools", 3, mcpservers.VisibilityPublic, "alpha")
	listTools := makeMetaRPCBody(t, "tools/list", map[string]any{})

	// A connection without a choice stays Progressive.
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"list_servers"`)
	require.NotContains(t, w.Body.String(), hosted.slug+"--alpha")

	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeDirect)
	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"`+hosted.slug+`--alpha"`)
	require.NotContains(t, w.Body.String(), `"name":"list_servers"`)

	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": hosted.slug + "--alpha", "arguments": map[string]any{},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "unknown tool")
}

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
	require.Equal(t, mcproute.OrganizationGateway, login.RouteBase, "the login returns to the gateway's consent page")
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
	mcp.Attach(router, ti.service.Service, nil)

	w := postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusNotFound, w.Code, "without such an endpoint the path names an organization")

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "gateway", issuerID)

	w = postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"client_id"`)
}

// The gateway's consent page lists only the servers the person can reach,
// and a reachable server's connect page shows the connection behind it: a
// client whose only servers are hidden from them stays off every page, along
// with the service behind it.
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

	render := func(query string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+fx.organizationSlug+"/connect?state="+stateID+query, nil)
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(w, req, endpoint))
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}

	grantedServer := fx.granted
	page := render("")
	require.Contains(t, page, `data-gateway-server="`+grantedServer+`"`)
	require.NotContains(t, page, fx.denied)
	require.NotContains(t, page, reachable, "the gateway's page lists servers, not services")

	page = render("&server=" + url.QueryEscape(grantedServer))
	require.Contains(t, page, reachable)
	require.NotContains(t, page, hidden)

	page = render("&server=" + url.QueryEscape(fx.denied))
	require.Contains(t, page, "data-gateway-focus-unavailable", "a hidden server has no connect page")
	require.NotContains(t, page, hidden)

	// A first-party connect opened for a server lands on its page, and the
	// page's link back to every server leaves it.
	focused, err := ti.authnChallengeCache.Get(t.Context(), "authnChallenge:"+stateID)
	require.NoError(t, err)
	focused.FocusServer = grantedServer
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), focused))
	page = render("")
	require.Contains(t, page, reachable)
	require.Contains(t, page, "Go back to your AI client and try again")
	page = render("&server=")
	require.NotContains(t, page, reachable)
	require.Contains(t, page, `data-gateway-server="`+grantedServer+`"`)
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

// connection_status reports the members the user can reach, and links the
// organization gateway's own connect page rather than a project gateway's.
func TestServeOrganizationGateway_ConnectionStatusLinksItsConnectPage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolConnectionStatus, "arguments": map[string]any{}})
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	rpc := decodeRPCResponse(t, w)
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "connection_status must not fail: %s", text)
	var structured struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &structured))

	require.True(t, strings.HasSuffix(structured.StructuredContent.ConnectURL, "/mcp/gateway/"+fx.organizationSlug+"/connect/first-party"),
		"connectUrl %q must be the organization gateway's connect page", structured.StructuredContent.ConnectURL)
	slugs := make([]string, 0, len(structured.StructuredContent.Servers))
	for _, server := range structured.StructuredContent.Servers {
		slugs = append(slugs, server.Slug)
	}
	require.ElementsMatch(t, []string{fx.granted, fx.open}, slugs)
}

// Turning the gateway off answers 404 on its MCP endpoint and its sign-in
// routes from the next request, and turning it back on restores both.
func TestServeOrganizationGateway_TurnedOffIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	metadataPath := "/.well-known/oauth-protected-resource/mcp/gateway/" + fx.organizationSlug
	getMetadata := func() int {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, metadataPath, nil))
		return w.Code
	}
	features := featuresrepo.New(ti.conn)
	setting := featuresrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureOrganizationGatewayDisabled),
	}
	require.Equal(t, http.StatusOK, getMetadata())

	_, err := features.EnableFeature(ctx, setting)
	require.NoError(t, err)

	_, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Equal(t, http.StatusNotFound, getMetadata())

	_, err = features.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
	require.NoError(t, err)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, http.StatusOK, getMetadata())
}
