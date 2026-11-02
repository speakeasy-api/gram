package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type staleChoiceGateway struct {
	ti       *testInstance
	slug     string
	issuer   uuid.UUID
	client   usersessionsrepo.UserSessionClient
	gateway  uuid.UUID
	source   *catalogUpstream
	other    *catalogUpstream
	serverID uuid.UUID
	project  uuid.UUID
	org      string
	mode     toolfilter.CatalogMode
	disc     metamcp.DiscoveryMode
	router   goahttp.ResolverMuxer
}

func newStaleChoiceGateway(t *testing.T, mode toolfilter.CatalogMode, discovery metamcp.DiscoveryMode) (context.Context, *staleChoiceGateway) {
	t.Helper()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "stale-no-auth-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	source := newCatalogUpstream(t, 1)
	other := newCatalogUpstream(t, 1)
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, source.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other", "other", 2, other.url)
	ti.service.manual = true
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	return ctx, &staleChoiceGateway{router: router, ti: ti, slug: slug, issuer: issuer.ID, client: client, gateway: gateway.ID, source: source, other: other, serverID: serverID, project: *auth.ProjectID, org: auth.ActiveOrganizationID, mode: mode, disc: discovery}
}

func (g *staleChoiceGateway) approve(t *testing.T, ctx context.Context) catalogConnection {
	t.Helper()
	return approveGatewayCatalog(t, ctx, g.ti, g.issuer, g.client, g.gateway, g.slug, g.mode, g.disc, nil)
}

// attachSignIn gives the server a sign-in; typed also records it as an auth client.
func (g *staleChoiceGateway) attachSignIn(t *testing.T, ctx context.Context, typed bool) uuid.UUID {
	t.Helper()
	registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "typed-"+uuid.NewString()[:8], "", []uuid.UUID{g.issuer})
	_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
	require.NoError(t, err)
	if typed {
		fixtures := authclientfixtures.New(g.ti.conn)
		id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: g.org, ProjectID: uuid.NullUUID{UUID: g.project, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: g.project, OrganizationID: g.org, McpServerID: g.serverID, AuthClientID: id, Source: "admin"}))
	}
	return registration
}

func (g *staleChoiceGateway) call(t *testing.T, token, qualified string) *httptest.ResponseRecorder {
	t.Helper()
	name, args := qualified, map[string]any{}
	if g.disc == metamcp.DiscoveryModeProgressive {
		name, args = metamcp.ToolExecuteTool, map[string]any{"name": qualified, "arguments": map[string]any{}}
	}
	return g.post(t, token, name, args)
}

// post goes through the mounted route.
func (g *staleChoiceGateway) post(t *testing.T, token, name string, args map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+g.slug, bytes.NewReader(makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": args}))).WithContext(t.Context())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	g.router.ServeHTTP(w, req)
	return w
}

func (g *staleChoiceGateway) refresh(t *testing.T, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, g.ti, g.slug, "token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {g.client.ClientID}})
}

// enforce turns on the typed resolver for the organization.
func (g *staleChoiceGateway) enforce(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := productfeaturesrepo.New(g.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: g.org, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
}

// sessionWithoutCatalog mints a session on the stale credential with no consent catalog.
func (g *staleChoiceGateway) sessionWithoutCatalog(t *testing.T, ctx context.Context, approved catalogConnection, refreshable bool) string {
	t.Helper()
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(approved.token, urn.NewUserSessionIssuer(g.issuer).String())
	require.NoError(t, err)
	queries := usersessionsrepo.New(g.ti.conn)
	session, err := queries.GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: g.issuer, Jti: claims.ID})
	require.NoError(t, err)
	subject := urn.NewUserSubject(mockidp.MockUserID)
	// A session bound to the exact resource carries no refresh token.
	audience := g.ti.serverURL.JoinPath("mcp", g.slug).String()
	if refreshable {
		audience = claims.Audience[0]
	}
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{Subject: subject, Audience: audience, Issuer: claims.Issuer, Lifetime: time.Hour})
	require.NoError(t, err)
	hash := conv.ToPGText("id-jag:assertion-session")
	if refreshable {
		sum := sha256.Sum256([]byte(jti))
		hash = conv.ToPGText(base64.RawURLEncoding.EncodeToString(sum[:]))
	}
	now := time.Now()
	_, err = queries.CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: g.issuer, SubjectUrn: subject, Jti: jti, RefreshTokenHash: hash,
		ExpiresAt: conv.ToPGTimestamptz(now.Add(time.Hour)), RefreshExpiresAt: conv.ToPGTimestamptz(now.Add(time.Hour)),
		SignInID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, CredentialID: uuid.NullUUID{UUID: session.ID, Valid: true},
	})
	require.NoError(t, err)
	return token
}

const (
	staleReconnectCopy = `now needs a sign-in. Reconnect this gateway in your AI app and approve the new connection.`
	staleAdminCopy     = `needs your administrator to finish setup or give you access.`
	staleSignInCopy    = `Sign in to this gateway again in your AI app. If that doesn't help, ask your administrator.`
	staleOwnerCopy     = `Ask whoever set this up to update this connection.`
)

// requireStaleError is an ordinary tool error carrying the remedy, with no challenge.
func requireStaleError(t *testing.T, w *httptest.ResponseRecorder, remedy string) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Contains(t, w.Body.String(), `"isError":true`)
	require.Contains(t, w.Body.String(), remedy)
}

func requireExecuted(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "executed live")
}

func TestStaleNoAuthChoiceReconnectsThroughConsent(t *testing.T) {
	t.Parallel()
	for _, discovery := range []metamcp.DiscoveryMode{metamcp.DiscoveryModeDirect, metamcp.DiscoveryModeProgressive} {
		for _, mode := range []toolfilter.CatalogMode{toolfilter.CatalogModeCached, toolfilter.CatalogModeLive} {
			for _, typed := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/typed=%t", discovery, mode, typed), func(t *testing.T) {
					t.Parallel()
					ctx, g := newStaleChoiceGateway(t, mode, discovery)
					connection := g.approve(t, ctx)
					requireExecuted(t, g.call(t, connection.token, "source--tool_000"))

					registration := g.attachSignIn(t, ctx, typed)
					before := g.source.requests.Load()
					requireStaleError(t, g.call(t, connection.token, "source--tool_000"), `Server \"source\" `+staleReconnectCopy)
					require.Equal(t, before, g.source.requests.Load(), "refused before any upstream request")

					requireExecuted(t, g.call(t, connection.token, "other--tool_000"))
					refreshed := g.refresh(t, connection.refresh)
					require.Equal(t, http.StatusOK, refreshed.Code, "refresh is untouched: %s", refreshed.Body.String())
					var rotated struct {
						AccessToken string `json:"access_token"`
					}
					require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &rotated))
					requireExecuted(t, g.call(t, rotated.AccessToken, "other--tool_000"))
					requireStaleError(t, g.call(t, rotated.AccessToken, "source--tool_000"), staleReconnectCopy)

					insertQualifiedRemoteSessionToken(t, ctx, g.ti, g.issuer, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", g.source.url)
					renewed := g.approve(t, ctx)
					requireExecuted(t, g.call(t, renewed.token, "source--tool_000"))
					require.Equal(t, "Bearer fixture-token", g.source.callAuthorization.Load())
				})
			}
		}
	}
}

func TestStaleNoAuthDescribeServerSameError(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeLive, metamcp.DiscoveryModeProgressive)
	connection := g.approve(t, ctx)
	g.attachSignIn(t, ctx, true)
	requireStaleError(t, g.post(t, connection.token, metamcp.ToolDescribeServer, map[string]any{"server": "source"}), staleReconnectCopy)
}

func TestStaleNoAuthChoiceConsentCannotFix(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		enforced bool
		arrange  func(t *testing.T, ctx context.Context, g *staleChoiceGateway)
	}{
		"client not bound to the gateway's issuer": {arrange: func(t *testing.T, ctx context.Context, g *staleChoiceGateway) {
			t.Helper()
			registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "unbound-"+uuid.NewString()[:8], "", nil)
			_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
			require.NoError(t, err)
		}},
		"pending on a secret header": {arrange: func(t *testing.T, ctx context.Context, g *staleChoiceGateway) {
			t.Helper()
			source, err := mcpserversrepo.New(g.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: g.serverID, ProjectID: g.project})
			require.NoError(t, err)
			_, err = authclientfixtures.New(g.ti.conn).SeedRemoteHeaderFixture(ctx, authclientfixtures.SeedRemoteHeaderFixtureParams{RemoteMcpServerID: source.RemoteMcpServerID.UUID, Name: "X-Api-Key", IsSecret: true, Value: pgtype.Text{String: "sealed", Valid: true}})
			require.NoError(t, err)
		}},
		"enforced with pending auth": {enforced: true, arrange: func(t *testing.T, ctx context.Context, g *staleChoiceGateway) {
			t.Helper()
			g.attachSignIn(t, ctx, false)
		}},
		"enforced with every client denied": {enforced: true, arrange: func(t *testing.T, ctx context.Context, g *staleChoiceGateway) {
			t.Helper()
			g.attachSignIn(t, ctx, true)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
			if tc.enforced {
				g.enforce(t, ctx)
			}
			connection := g.approve(t, ctx)
			tc.arrange(t, ctx, g)
			w := g.call(t, connection.token, "source--tool_000")
			requireStaleError(t, w, staleAdminCopy)
			require.NotContains(t, w.Body.String(), "needs a sign-in")
			requireExecuted(t, g.call(t, connection.token, "other--tool_000"))
			require.Equal(t, http.StatusOK, g.refresh(t, connection.refresh).Code)
		})
	}
}

func TestStaleNoAuthEnforcedReconsentThenCallsAgain(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
	g.enforce(t, ctx)
	g.ti.service.SetAuthClientPermission(func() bool { return true })
	connection := g.approve(t, ctx)
	requireExecuted(t, g.call(t, connection.token, "source--tool_000"))
	registration := g.attachSignIn(t, ctx, true)
	requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleReconnectCopy)
	require.Equal(t, http.StatusOK, g.refresh(t, connection.refresh).Code)

	insertQualifiedRemoteSessionToken(t, ctx, g.ti, g.issuer, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", g.source.url)
	renewed := g.approve(t, ctx)
	for range 2 {
		requireExecuted(t, g.call(t, renewed.token, "source--tool_000"))
	}
	require.Equal(t, "Bearer fixture-token", g.source.callAuthorization.Load())
}

func TestStaleNoAuthWithoutCatalog(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		refreshable bool
		remedy      string
	}{
		"person":              {refreshable: true, remedy: staleReconnectCopy},
		"access-only session": {refreshable: false, remedy: staleSignInCopy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive)
			connection := g.approve(t, ctx)
			g.attachSignIn(t, ctx, true)
			requireStaleError(t, g.call(t, g.sessionWithoutCatalog(t, ctx, connection, tc.refreshable), "source--tool_000"), tc.remedy)
		})
	}
}

func TestValidGatewayChoiceIsUntouched(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
	registration := g.attachSignIn(t, ctx, true)
	insertQualifiedRemoteSessionToken(t, ctx, g.ti, g.issuer, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", g.source.url)
	connection := g.approve(t, ctx)
	requireExecuted(t, g.call(t, connection.token, "source--tool_000"))
	require.Equal(t, http.StatusOK, g.refresh(t, connection.refresh).Code)
}

// attachTypedOnly gives the server a personal auth client with no older association.
func (g *staleChoiceGateway) attachTypedOnly(t *testing.T, ctx context.Context, issuers []uuid.UUID) uuid.UUID {
	t.Helper()
	registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "typed-only-"+uuid.NewString()[:8], "", issuers)
	fixtures := authclientfixtures.New(g.ti.conn)
	id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: g.org, ProjectID: uuid.NullUUID{UUID: g.project, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: g.project, OrganizationID: g.org, McpServerID: g.serverID, AuthClientID: id, Source: "admin"}))
	return registration
}

func (g *staleChoiceGateway) postConnect(t *testing.T, ctx context.Context, registration uuid.UUID) (*httptest.ResponseRecorder, error) {
	t.Helper()
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, g.ti, g.issuer, g.client, g.gateway, g.slug)
	endpoint, err := g.ti.service.LoadResolvedMcpEndpointBySlug(ctx, g.ti.logger, g.slug, "mcp")
	require.NoError(t, err)
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"connect"}, "client_id": {registration.String()}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+g.slug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(t.Context())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	return w, g.ti.service.ServeConsentAction(w, req, endpoint)
}

// The reconnect text is only given where Connect gets past its member and issuer checks.
func TestStaleNoAuthReconnectCopyMatchesConnect(t *testing.T) {
	t.Parallel()
	t.Run("associated and bound", func(t *testing.T) {
		t.Parallel()
		ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
		g.enforce(t, ctx)
		g.ti.service.SetAuthClientPermission(func() bool { return true })
		connection := g.approve(t, ctx)
		registration := g.attachSignIn(t, ctx, true)
		requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleReconnectCopy)
		w, err := g.postConnect(t, ctx, registration)
		require.NoError(t, err)
		require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	})
	t.Run("not associated", func(t *testing.T) {
		t.Parallel()
		ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
		g.enforce(t, ctx)
		g.ti.service.SetAuthClientPermission(func() bool { return true })
		connection := g.approve(t, ctx)
		registration := g.attachTypedOnly(t, ctx, []uuid.UUID{g.issuer})
		requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleAdminCopy)
		_, err := g.postConnect(t, ctx, registration)
		require.ErrorContains(t, err, "not configured for any server this gateway serves")
	})
	t.Run("not bound to the issuer", func(t *testing.T) {
		t.Parallel()
		ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
		g.enforce(t, ctx)
		g.ti.service.SetAuthClientPermission(func() bool { return true })
		connection := g.approve(t, ctx)
		registration := g.attachTypedOnly(t, ctx, nil)
		_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
		require.NoError(t, err)
		requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleAdminCopy)
	})
	t.Run("already connected needs no Connect", func(t *testing.T) {
		t.Parallel()
		ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
		g.enforce(t, ctx)
		g.ti.service.SetAuthClientPermission(func() bool { return true })
		connection := g.approve(t, ctx)
		registration := g.attachTypedOnly(t, ctx, nil)
		insertQualifiedRemoteSessionToken(t, ctx, g.ti, g.issuer, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", g.source.url)
		requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleReconnectCopy)
		renewed := g.approve(t, ctx)
		requireExecuted(t, g.call(t, renewed.token, "source--tool_000"))
	})
}

func TestStaleNoAuthCachedDescribeMatchesTheCall(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive)
	connection := g.approve(t, ctx)
	registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "unbound-"+uuid.NewString()[:8], "", nil)
	_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
	require.NoError(t, err)
	before := g.source.requests.Load()
	requireStaleError(t, g.post(t, connection.token, metamcp.ToolDescribeServer, map[string]any{"server": "source"}), staleAdminCopy)
	described := g.post(t, connection.token, metamcp.ToolDescribeTools, map[string]any{"tools": []string{"source--tool_000"}})
	require.Contains(t, described.Body.String(), staleAdminCopy)
	require.NotContains(t, described.Body.String(), "Give consent again")
	require.Equal(t, before, g.source.requests.Load())
}

func TestStaleNoAuthConnectionStatusMatchesTheCall(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mode   toolfilter.CatalogMode
		bound  bool
		status string
		remedy string
	}{
		"cached, consent can fix it": {mode: toolfilter.CatalogModeCached, bound: true, status: metamcp.SignInNeedsConsent, remedy: staleReconnectCopy},
		"cached, consent cannot":     {mode: toolfilter.CatalogModeCached, bound: false, status: metamcp.SignInActionRequired, remedy: staleAdminCopy},
		"live, consent can fix it":   {mode: toolfilter.CatalogModeLive, bound: true, status: metamcp.SignInNeedsConsent, remedy: staleReconnectCopy},
		"live, consent cannot":       {mode: toolfilter.CatalogModeLive, bound: false, status: metamcp.SignInActionRequired, remedy: staleAdminCopy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, g := newStaleChoiceGateway(t, tc.mode, metamcp.DiscoveryModeProgressive)
			connection := g.approve(t, ctx)
			if tc.bound {
				g.attachSignIn(t, ctx, true)
			} else {
				registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "unbound-"+uuid.NewString()[:8], "", nil)
				_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
				require.NoError(t, err)
			}
			w := g.post(t, connection.token, metamcp.ToolConnectionStatus, map[string]any{})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.remedy)
			require.Contains(t, w.Body.String(), `"status":"`+tc.status+`"`)
			require.NotContains(t, w.Body.String(), "To sign in, open", "the connect page can't fix a stale choice")
		})
	}
}

func TestStaleNoAuthCodeModeReachesTheProgram(t *testing.T) {
	t.Parallel()
	for _, mode := range []toolfilter.CatalogMode{toolfilter.CatalogModeCached, toolfilter.CatalogModeLive} {
		for name, tc := range map[string]struct {
			fixable bool
			remedy  string
		}{
			"consent can fix it": {fixable: true, remedy: staleReconnectCopy},
			"consent cannot":     {fixable: false, remedy: staleAdminCopy},
		} {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				t.Parallel()
				ctx, g := newStaleChoiceGateway(t, mode, metamcp.DiscoveryModeCode)
				mcp.SetTestCodeExecutor(g.ti.service.Service, &recordingCodeExecutor{})
				enableCodeMode(t, ctx, g.ti)
				connection := g.approve(t, ctx)
				if tc.fixable {
					g.attachSignIn(t, ctx, true)
				} else {
					registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "unbound-"+uuid.NewString()[:8], "", nil)
					_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: g.serverID, RemoteSessionClientID: registration})
					require.NoError(t, err)
				}
				requireRefusal := func(err error) {
					t.Helper()
					refused, ok := errors.AsType[*codemode.MemberRefusalError](err)
					require.True(t, ok, "got %v", err)
					require.Contains(t, refused.Message, `"source"`)
					require.Contains(t, refused.Message, tc.remedy)
					require.Equal(t, tc.fixable, errors.Is(err, codemode.ErrConsentRequired))
				}
				ran := false
				mcp.SetTestCodeExecutor(g.ti.service.Service, &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
					ran = true
					_, callErr := host.Call(runCtx, "source--tool_000", json.RawMessage(`{}`))
					requireRefusal(callErr)
					_, describeErr := host.Describe(runCtx, "source--tool_000")
					requireRefusal(describeErr)
					search, searchErr := host.Search(runCtx, codemode.SearchArgs{Query: "", Server: "source", Limit: 10})
					require.NoError(t, searchErr)
					require.Len(t, search.FailedMembers, 1)
					require.Contains(t, search.FailedMembers[0].Message, tc.remedy)
					page, serversErr := host.Servers(runCtx, codemode.PageArgs{Server: "source", Verify: true})
					require.NoError(t, serversErr)
					require.Len(t, page.Items, 1)
					connection := page.Items[0].Connection
					require.Contains(t, connection.Instructions, map[bool]string{true: "reconnect this gateway", false: staleAdminCopy}[tc.fixable])
					require.Equal(t, map[bool]string{true: "reauth_required", false: "unknown"}[tc.fixable], connection.Authentication)
					return &codemode.ExecutionResult{}, nil
				}})
				w := g.post(t, connection.token, "execute", map[string]any{"code": "call"})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.True(t, ran, w.Body.String())
			})
		}
	}
}

func TestStaleNoAuthDirectServerKeepsReconnectChallenge(t *testing.T) {
	t.Parallel()
	for _, enforced := range []bool{false, true} {
		t.Run(fmt.Sprintf("enforced=%t", enforced), func(t *testing.T) {
			t.Parallel()
			ctx, f := newSelectionFixture(t)
			if enforced {
				_, err := productfeaturesrepo.New(f.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: f.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
			}
			issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
			upstream := newRecordingUpstream(t, "ping")
			slug := "stale-direct-" + uuid.NewString()[:8]
			server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, upstream.url, slug, "public", issuerID)
			subject := createTestUser(t, ctx, f.ti, "stale-direct-"+uuid.NewString())
			bearer := f.issue(t, ctx, slug, issuerID, subject)
			f.associate(t, ctx, server.ID, createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "stale-direct", "", []uuid.UUID{issuerID}))
			router := goahttp.NewMuxer()
			mcp.Attach(router, f.ti.service.Service, nil)
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, bytes.NewReader(makeMetaRPCBody(t, "tools/call", map[string]any{"name": "ping", "arguments": map[string]any{}}))).WithContext(t.Context())
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+bearer)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
		})
	}
}

func TestStaleNoAuthEnforcedRealPermitAllowsReconnect(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect)
	g.enforce(t, ctx)
	connection := g.approve(t, ctx)
	g.attachSignIn(t, ctx, true)
	selectors, err := authz.NewSelector(authz.ScopeMCPAuth, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(g.ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: g.org, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), Scope: string(authz.ScopeMCPAuth), Selectors: selectors})
	require.NoError(t, err)
	requireStaleError(t, g.call(t, connection.token, "source--tool_000"), staleReconnectCopy)
}

func TestConnectionStatusWritesNoChoiceUnderEnforcement(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive)
	g.enforce(t, ctx)
	connection := g.approve(t, ctx)
	token := g.sessionWithoutCatalog(t, ctx, connection, true)
	late := newCatalogUpstream(t, 1)
	lateServer := seedMetaMemberWithUpstream(t, ctx, g.ti.conn, g.project, g.gateway, "Late", "late", 3, late.url)
	choices := func() int { return g.lateChoices(t, ctx, connection.token, lateServer) }
	require.Zero(t, choices())
	w := g.post(t, token, metamcp.ToolConnectionStatus, map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Zero(t, choices(), "a status check writes no choice")
	require.Contains(t, w.Body.String(), "Late: no sign-in needed", "status says what the call will find")
	requireExecuted(t, g.call(t, token, "late--tool_000"))
	require.Equal(t, 1, choices(), "a call still records the new member's choice")
}

func TestStaleNoAuthAgentRouteNamesTheAgentInItsLog(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ti.service.manual = true
	fx := seedAgentGateway(t, ctx, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	upstream := newCatalogUpstream(t, 1)
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: *auth.ProjectID, TransportType: "streamable-http", Url: upstream.url})
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	serverID := mcpServerBySlug(t, ctx, ti, *auth.ProjectID, fx.granted)
	_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: serverID, ProjectID: *auth.ProjectID, Name: conv.ToPGText(fx.granted), Slug: conv.ToPGText(fx.granted), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: mcpservers.VisibilityPrivate})
	require.NoError(t, err)
	subject := urn.NewAgentSubject(fx.agent.ID)
	keys, err := keysrepo.New(ti.conn).ListAgentAPIKeys(ctx, keysrepo.ListAgentAPIKeysParams{OrganizationID: auth.ActiveOrganizationID, SubjectUrn: pgtype.Text{String: subject.String(), Valid: true}})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	_, err = upstreamclientsrepo.New(ti.conn).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keys[0].ID, Source: upstreamclients.SelectionSourceIssuance, OrganizationID: auth.ActiveOrganizationID, AuthClientID: uuid.NullUUID{}, McpServerID: serverID})
	require.NoError(t, err)
	registration := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "agent-"+uuid.NewString()[:8], "", []uuid.UUID{issuerID})
	fixtures := authclientfixtures.New(ti.conn)
	id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: "service_account", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, AuthClientID: id, Source: "admin"}))

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": fx.granted + "--tool_000", "arguments": map[string]any{}}}))
	require.NoError(t, err)
	requireStaleError(t, w, staleOwnerCopy)
	require.True(t, ti.service.StaleNoAuthWarned(subject.String(), serverID, "no_auth_forbidden"), "the warning names the agent")
}

// lateChoices counts the choices recorded for serverID on token's credential.
func (g *staleChoiceGateway) lateChoices(t *testing.T, ctx context.Context, token string, serverID uuid.UUID) int {
	t.Helper()
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(token, urn.NewUserSessionIssuer(g.issuer).String())
	require.NoError(t, err)
	queries := usersessionsrepo.New(g.ti.conn)
	session, err := queries.GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: g.issuer, Jti: claims.ID})
	require.NoError(t, err)
	credential := session.ID
	if session.CredentialID.Valid {
		credential = session.CredentialID.UUID
	}
	rows, err := queries.ListUpstreamClientSelections(ctx, usersessionsrepo.ListUpstreamClientSelectionsParams{UserSessionIssuerID: g.issuer, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credential, McpServerIds: []uuid.UUID{serverID}, OrganizationID: g.org})
	require.NoError(t, err)
	return len(rows)
}

func TestReadOnlyPreviewResolvesANewMembersReadyClient(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive)
	g.enforce(t, ctx)
	g.ti.service.SetAuthClientPermission(func() bool { return true })
	connection := g.approve(t, ctx)
	token := g.sessionWithoutCatalog(t, ctx, connection, true)
	late := newCatalogUpstream(t, 1)
	lateServer := seedMetaMemberWithUpstream(t, ctx, g.ti.conn, g.project, g.gateway, "Late", "late", 3, late.url)
	registration := createConsentRemoteClient(t, ctx, g.ti.conn, g.project, g.org, "late-"+uuid.NewString()[:8], "", []uuid.UUID{g.issuer})
	_, err := upstreamclientsrepo.New(g.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: g.org, ProjectID: g.project, McpServerID: lateServer, RemoteSessionClientID: registration})
	require.NoError(t, err)
	fixtures := authclientfixtures.New(g.ti.conn)
	id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: g.org, ProjectID: uuid.NullUUID{UUID: g.project, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: g.project, OrganizationID: g.org, McpServerID: lateServer, AuthClientID: id, Source: "admin"}))
	insertQualifiedRemoteSessionToken(t, ctx, g.ti, g.issuer, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", late.url)

	status := g.post(t, token, metamcp.ToolConnectionStatus, map[string]any{})
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.Contains(t, status.Body.String(), "Late: signed in", "the preview resolves to the ready client")
	described := g.post(t, token, metamcp.ToolDescribeServer, map[string]any{"server": "late"})
	require.Contains(t, described.Body.String(), "late--tool_000")
	require.Zero(t, g.lateChoices(t, ctx, connection.token, lateServer), "reading records no choice")

	requireExecuted(t, g.call(t, token, "late--tool_000"))
	require.Equal(t, "Bearer fixture-token", late.callAuthorization.Load())
	require.Equal(t, 1, g.lateChoices(t, ctx, connection.token, lateServer))
}

func TestCodeModeVerifyWritesNoChoice(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode)
	g.enforce(t, ctx)
	mcp.SetTestCodeExecutor(g.ti.service.Service, &recordingCodeExecutor{})
	enableCodeMode(t, ctx, g.ti)
	token := mintGatewayTestSession(t, ctx, g.ti, g.gateway, g.issuer, g.slug, metamcp.DiscoveryModeCode)
	ran := false
	var lateServer uuid.UUID
	mcp.SetTestCodeExecutor(g.ti.service.Service, &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		ran = true
		// Joining after admission, so only the verify below can record it.
		lateServer = seedMetaMemberWithUpstream(t, ctx, g.ti.conn, g.project, g.gateway, "Late", "late", 3, newCatalogUpstream(t, 1).url)
		_, err := host.Servers(runCtx, codemode.PageArgs{Server: "", Verify: true})
		require.NoError(t, err)
		return &codemode.ExecutionResult{}, nil
	}})
	w := g.post(t, token, "execute", map[string]any{"code": "verify"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, ran, w.Body.String())
	require.Zero(t, g.lateChoices(t, ctx, token, lateServer))
}
