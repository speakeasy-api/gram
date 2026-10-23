package mcp_test

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
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
func TestServeConsent_OrganizationGatewayPicksTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	hosted := seedHostedMetaMember(t, ctx, ti, decoy.ID, "Hosted tools", 3, mcpservers.VisibilityPrivate, "alpha", "beta")
	setToolsetMcpPrivate(t, ctx, ti, hosted.toolsetID, *authCtx.ProjectID)
	upstream := newCatalogUpstream(t, 1)
	remoteID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, decoy.ID, "Remote", "remote-counted", 4, upstream.url)
	require.NoError(t, testrepo.New(ti.conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{ID: remoteID, ProjectID: *authCtx.ProjectID, Visibility: mcpservers.VisibilityPrivate}))
	for _, id := range []uuid.UUID{hosted.serverID, hosted.toolsetID, remoteID} {
		seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, fx.subject.ID, id.String())
	}
	// The browser request carries identity, while admission loads persisted grants.
	caller := contextvalues.SetAuthContext(t.Context(), authCtx)
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     fx.issuerID,
		ClientID:                "org-picker-" + uuid.NewString()[:8],
		ClientName:              "org picker",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	pkce := sha256.Sum256([]byte(gatewayConsentVerifier))
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(pkce[:]),
		CodeChallengeMethod: "S256",
		CSRFToken:           csrfToken,
		Subject:             &fx.subject,
		CreatedAt:           time.Now(),
	}))

	page := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(page, httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+fx.organizationSlug+"/connect?state="+stateID, nil).WithContext(caller), endpoint))
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "consent-tools-root", "organization gateway connections can pick tools")

	inventory := gatewayInventoryFromPage(t, page.Body.String())
	attempt := inventory.Attempt
	names := make([]string, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		names = append(names, tool.Name)
	}
	require.Contains(t, names, hosted.slug+"--alpha")
	require.Contains(t, names, hosted.slug+"--beta")
	require.Contains(t, names, "remote-counted--tool_000")

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", attempt)
	form.Set("tool_filtering", "on")
	form.Add("tools", hosted.slug+"--alpha")
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approve := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(approve, req.WithContext(ctx), endpoint))
	require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())
	location, err := url.Parse(approve.Header().Get("Location"))
	require.NoError(t, err)
	code := location.Query().Get("code")
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "gatewayCatalogGrant:"+fx.issuerID.String()+":"+code)
	require.NoError(t, err)
	require.NotNil(t, grant.GatewayPolicy)
	raw, err := json.Marshal(grant.GatewayPolicy)
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.NotNil(t, policy.Selection)
	require.Equal(t, "meta_mcp_server:"+endpoint.MetaMcpServerID.UUID.String(), policy.Selection.Resource)
	require.True(t, policy.Selection.AllowsName(hosted.slug+"--alpha"))
	require.False(t, policy.Selection.AllowsName(hosted.slug+"--beta"))
	require.NotNil(t, policy.Catalog)
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "code_verifier": {gatewayConsentVerifier}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/token", strings.NewReader(tokenForm.Encode())).WithContext(ctx)
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	issued := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(issued, tokenRequest, endpoint))
	require.Equal(t, http.StatusOK, issued.Code, issued.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &tokens))
	tokenCalls := seedExpiredCatalogCredential(t, ctx, ti, fx.issuerID, fx.subject)
	before := upstream.requests.Load()
	for _, name := range []string{"list_servers", "describe_server", "connection_status"} {
		arguments := map[string]any{}
		if name == "describe_server" {
			arguments["server"] = hosted.slug
		}
		body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments})
		listed, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, tokens.AccessToken, body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
		require.NotContains(t, listed.Body.String(), `"isError":true`)
	}
	require.Equal(t, before, upstream.requests.Load(), "organization gateway discovery reuses the consent catalog")
	require.Zero(t, tokenCalls.Load(), "organization gateway discovery never refreshes credentials")
}

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

func TestOrganizationGatewayConsentLockdownBlocksCaptureAndChecks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, decoy.ID, "Remote", "lockdown-remote", 1, upstream.url)
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{UserSessionIssuerID: fx.issuerID, ClientID: "lockdown-" + uuid.NewString(), ClientName: "Fixture", RedirectUris: []string{"http://localhost:3000/callback"}, TokenEndpointAuthMethod: "none"})
	require.NoError(t, err)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, fx.issuerID, client, endpoint.MetaMcpServerID.UUID, fx.organizationSlug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.Endpoint.RouteBase = mcproute.OrganizationGateway
	state.Subject = &fx.subject
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	path := "/mcp/gateway/" + fx.organizationSlug + "/connect"
	page := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(page, httptest.NewRequest(http.MethodGet, path+"?state="+stateID, nil).WithContext(ctx), endpoint))
	first := gatewayInventoryFromPage(t, page.Body.String())
	before := upstream.requests.Load()
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{OrganizationID: auth.ActiveOrganizationID, Domain: "gateway.example.test", ProvisionerKind: "ingress", IpAllowlist: []string{"203.0.113.0/24"}})
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).ActivateVerifiedCustomDomain(ctx, customdomainsrepo.ActivateVerifiedCustomDomainParams{ID: domain.ID, IngressName: pgtype.Text{String: "fixture", Valid: true}, CertSecretName: pgtype.Text{String: "fixture-cert", Valid: true}, ProvisionerKind: "ingress"})
	require.NoError(t, err)
	page = httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(page, httptest.NewRequest(http.MethodGet, path+"?state="+stateID, nil).WithContext(ctx), endpoint))
	require.Contains(t, page.Body.String(), "Reconnect your AI client")
	require.Contains(t, page.Body.String(), `href="https://gateway.example.test/mcp/gateway/`+fx.organizationSlug+`"`)
	var captured any
	require.Error(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":gateway:", &captured), "lockdown invalidates an older tab's capture")
	for _, action := range []string{"identity_chaining_check", "validate", "approve"} {
		form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {action}, "tool_inventory_id": {first.Attempt}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var err error
		if action == "approve" {
			err = ti.service.ServeConsent(httptest.NewRecorder(), req, endpoint)
		} else {
			err = ti.service.ServeConsentAction(httptest.NewRecorder(), req, endpoint)
		}
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeForbidden, denied.Code)
	}
	require.Equal(t, before, upstream.requests.Load(), "no discovery or connection probes from the locked-down origin")
}
