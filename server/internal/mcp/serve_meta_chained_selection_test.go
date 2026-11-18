package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const chainedMemberToken = "chained-member-token"

// singleUpstreamChainer serves exactly one upstream, and Acquire succeeds
// for it with a fixed token.
type singleUpstreamChainer struct {
	issuer   uuid.UUID
	upstream string
	usable   bool
	mu       sync.Mutex
	acquired int
}

func (c *singleUpstreamChainer) match(req identitychaining.Request) bool {
	return strings.TrimRight(req.UpstreamResource, "/") == strings.TrimRight(c.upstream, "/")
}

func (c *singleUpstreamChainer) Governs(_ context.Context, req identitychaining.Request) bool {
	return c.match(req)
}

func (c *singleUpstreamChainer) Serves(_ context.Context, req identitychaining.Request) (uuid.UUID, bool) {
	return c.issuer, c.match(req)
}

func (c *singleUpstreamChainer) Configured(context.Context, string, uuid.UUID, uuid.UUID) bool {
	return true
}

func (c *singleUpstreamChainer) HasUsableCredential(_ context.Context, req identitychaining.Request) bool {
	return c.usable && c.match(req)
}

func (c *singleUpstreamChainer) RefreshDecision(_ context.Context, req identitychaining.Request) identitychaining.RefreshVerdict {
	if !c.match(req) {
		return identitychaining.RefreshVerdict{Decision: identitychaining.RefreshNotChained, RemoteSessionIssuerID: uuid.Nil, Provider: "", Reason: ""}
	}
	return identitychaining.RefreshVerdict{Decision: identitychaining.RefreshServed, RemoteSessionIssuerID: c.issuer, Provider: "", Reason: ""}
}

func (c *singleUpstreamChainer) RetireConsentCredentials(context.Context, string, uuid.UUID, string, uuid.UUID) error {
	return nil
}

func (c *singleUpstreamChainer) CheckConsent(context.Context, identitychaining.Request, uuid.UUID) identitychaining.Outcome {
	return identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonSuccess}
}

func (c *singleUpstreamChainer) Acquire(_ context.Context, req identitychaining.Request) (identitychaining.Token, identitychaining.Outcome) {
	c.mu.Lock()
	c.acquired++
	c.mu.Unlock()
	if !c.match(req) {
		return identitychaining.Token{}, identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable}
	}
	return identitychaining.NewTokenForTest(chainedMemberToken, time.Now().Add(time.Hour)), identitychaining.Outcome{Stage: identitychaining.StageRedemption, Reason: identitychaining.ReasonSuccess}
}

func (c *singleUpstreamChainer) acquireCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acquired
}

// chainedOnlyUpstream fronts a recording upstream that accepts only the
// chained token, like a member whose authorization server trusts only the
// Speakeasy-signed identity assertion.
type chainedOnlyUpstream struct {
	url   string
	inner *recordingUpstream
}

func newChainedOnlyUpstream(t *testing.T) *chainedOnlyUpstream {
	t.Helper()
	inner := newRecordingUpstream(t, "note")
	target, err := url.Parse(inner.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+chainedMemberToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &chainedOnlyUpstream{url: srv.URL, inner: inner}
}

func (u *chainedOnlyUpstream) toolCalls() int {
	n := 0
	for _, req := range u.inner.journal() {
		if req.rpcMethod == mcpversions.MethodToolsCall {
			n++
		}
	}
	return n
}

// Consent exchanges the identity before approval, so a chained server is a
// verified source. Cached discovery reuses that result without another exchange.
func TestGatewayConsentDiscoversChainedMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-chained-consent-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newChainedOnlyUpstream(t)
	memberSlug := "chained-" + uuid.NewString()[:8]
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Chained", memberSlug, 1, upstream.url)
	upstreamClient := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "chained-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: memberID, RemoteSessionClientID: upstreamClient})
	require.NoError(t, err)
	fixtures := authclientbackfill.New(ti.conn)
	registration := uuid.NullUUID{UUID: upstreamClient, Valid: true}
	member, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	// Backfilled EMA clients need a ready binding: discovery records metadata
	// and reconciles the binding's exact resource before the next consent.
	bindingID, err := fixtures.SeedEMABindingFixture(ctx, authclientbackfill.SeedEMABindingFixtureParams{
		ProjectID:             *auth.ProjectID,
		OrganizationID:        auth.ActiveOrganizationID,
		UserSessionIssuerID:   member.UserSessionIssuerID.UUID,
		RemoteSessionIssuerID: clientRemoteIssuerID(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, upstreamClient),
		RemoteSessionClientID: registration,
		Resource:              upstream.url,
		RequestedScopes:       []string{},
	})
	require.NoError(t, err)
	var typedClientID, emaClientID uuid.UUID
	for _, kind := range []string{"remote_session", "ema"} {
		id, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: kind, Source: "backfill", RemoteSessionClientID: registration})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: memberID, AuthClientID: id, Source: "backfill"}))
		if kind == "remote_session" {
			typedClientID = id
		}
		if kind == "ema" {
			emaClientID = id
			require.NoError(t, fixtures.SeedAuthClientEMAFixture(ctx, authclientbackfill.SeedAuthClientEMAFixtureParams{AuthClientID: id, Resource: upstream.url}))
		}
	}
	// Finish the same reconciliation as a normal EMA preparation writer before
	// consent captures the server's authentication requirement.
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.SyncEMABinding(ctx, tx, auth.ActiveOrganizationID, bindingID))
	require.NoError(t, tx.Commit(ctx))
	ti.service.manual = true
	chainer := &singleUpstreamChainer{issuer: clientRemoteIssuerID(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, upstreamClient), upstream: upstream.url}
	ti.service.SetIdentityChainer(chainer)
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	require.Len(t, inventory.MemberOutcomes, 1)
	require.Equal(t, memberSlug, inventory.MemberOutcomes[0].Slug)
	require.Equal(t, "chained", inventory.MemberOutcomes[0].Status)
	require.Len(t, inventory.Tools, 1)
	require.Equal(t, memberSlug+"--note", inventory.Tools[0].Name)
	require.Positive(t, chainer.acquireCount())
	require.Zero(t, upstream.toolCalls())
	emaState, err := fixtures.GetAuthClientFixture(ctx, emaClientID)
	require.NoError(t, err)
	emaLinks, err := fixtures.ListBackfillLinkedServersFixture(ctx, emaClientID)
	require.NoError(t, err)
	require.False(t, emaState.Deleted, "discovery must preserve the binding-backed EMA client")
	require.Equal(t, []uuid.UUID{memberID}, emaLinks)
	ema, err := fixtures.GetEMAFixture(ctx, emaClientID)
	require.NoError(t, err)
	require.Equal(t, upstream.url, ema.Resource)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(connection.token, urn.NewUserSessionIssuer(issuer.ID).String())
	require.NoError(t, err)
	session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
	require.NoError(t, err)
	choices, err := upstreamclientsrepo.New(ti.conn).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{OrganizationID: auth.ActiveOrganizationID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: session.ID, McpServerIds: []uuid.UUID{memberID}})
	require.NoError(t, err)
	require.Len(t, choices, 1)
	require.Equal(t, uuid.NullUUID{UUID: typedClientID, Valid: true}, choices[0].AuthClientID)
	before := chainer.acquireCount()
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), memberSlug+"--note")
	require.Equal(t, before, chainer.acquireCount(), "cached discovery never exchanges")
	require.NotContains(t, callGatewayTool(t, ti, slug, connection.token, memberSlug+"--note", map[string]any{}), `"isError":true`)
	require.Equal(t, 1, upstream.toolCalls())
	require.Greater(t, chainer.acquireCount(), before, "execution resolves current identity")
}

func TestGatewayConsentReportsUnreachableChainedMemberAsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-chained-down-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	memberSlug := "chained-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Chained", memberSlug, 1, downURL)
	upstreamClient := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "chained-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	ti.service.SetIdentityChainer(&singleUpstreamChainer{issuer: clientRemoteIssuerID(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, upstreamClient), upstream: downURL})
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	outcomes := gatewayOutcomesFromPage(consentGetPageWithContext(t, ctx, ti, slug, stateID))
	require.Len(t, outcomes, 1)
	require.Equal(t, memberSlug, outcomes[0].Slug)
	require.Equal(t, "unavailable", outcomes[0].Status)
}

func (c *singleUpstreamChainer) AcquireConsent(ctx context.Context, req identitychaining.Request, _ uuid.UUID) (identitychaining.Token, identitychaining.Outcome) {
	return c.Acquire(ctx, req)
}
