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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
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

// attachSignIn is an administrator giving the server a sign-in. typed also
// records it as an auth client, as new setups do; otherwise the association
// is one no auth client records yet.
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
	staleAdminCopy     = `isn't set up for sign-in yet. Ask your administrator.`
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
			g.ti.service.SetAuthClientPermission(func() bool { return false })
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
		"access-only session": {refreshable: false, remedy: staleOwnerCopy},
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
