// The point of per-member credential selection is that a later request to one
// gateway member forwards that member's credential and no other. These tests
// carry a real gateway through consent, authorize, code exchange and persisted
// remote_sessions rows, then hand the result to the production routing
// decision, so the feature is asserted end to end.

package mcp_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// connectedGatewayMember is one member of a gateway whose client has completed
// the whole OAuth round trip.
type connectedGatewayMember struct {
	upstreamURL string
	accessToken string
	clientID    uuid.UUID
	seeded      gatewayMember
	// exchanged holds what this member's authorization server saw on the
	// token POST.
	exchanged *atomic.Value
}

// connectedGateway is a gateway with count remote members, each behind its own
// live authorization server, each with a connected client.
type connectedGateway struct {
	fx           consentActionFixture
	metaServerID uuid.UUID
	mgr          *remotesessions.ChallengeManager
	members      []connectedGatewayMember
}

// resolveTokens runs the production credential resolver for the gateway's
// issuer and subject — the same map the serving path routes from.
func (g connectedGateway) resolveTokens(t *testing.T, ctx context.Context) map[uuid.UUID]remotesessions.UpstreamToken {
	t.Helper()
	tokens, err := g.mgr.ResolveAccessTokens(ctx, g.fx.projectID, g.fx.orgID, g.fx.shared, g.fx.subject)
	require.NoError(t, err)
	return tokens
}

// route drives the real routeUpstreamToken for one backend's upstream
// resource. Nothing here is stubbed: the map comes from the database and the
// selection rule is the one the proxy uses.
func (g connectedGateway) route(t *testing.T, ctx context.Context, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string) (string, error) {
	t.Helper()
	token, err := mcp.RouteUpstreamTokenForTest(ctx, g.fx.ti.logger, tokens, upstreamResource)
	if err != nil {
		return "", fmt.Errorf("route upstream token: %w", err)
	}
	return token, nil
}

// connectGateway seeds a gateway with count remote members and drives each
// client through consent, authorize and code exchange, so every credential is
// persisted the way production writes it. Each member gets its own
// authorization server and is stamped with it — the state the resync produces.
// stamped=false leaves the column NULL, reproducing the pre-backfill fleet.
func connectGateway(t *testing.T, prefix string, count int, stamped bool) (context.Context, connectedGateway) {
	t.Helper()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, prefix+"-gw")

	captured := make([]atomic.Value, count)
	members := make([]connectedGatewayMember, 0, count)
	for i := range count {
		name := fmt.Sprintf("%s-%d", prefix, i)
		accessToken := "token-" + name
		as := newConsentExchangeAS(t, &captured[i], accessToken)

		clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, name, as.URL, []uuid.UUID{fx.shared})
		issuerID := uuid.NullUUID{}
		if stamped {
			issuerID = conv.ToNullUUID(clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID))
		}

		upstream := fmt.Sprintf("https://%s.example.com/mcp", name)
		members = append(members, connectedGatewayMember{
			upstreamURL: upstream,
			accessToken: accessToken,
			clientID:    clientID,
			seeded:      createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, name+"-member", upstream, issuerID, int32(i)),
			exchanged:   &captured[i],
		})
	}

	// Every member exists before any client connects, so consent always sees
	// the whole gateway.
	mgr := newConsentCallbackManager(t, fx.ti)
	for _, member := range members {
		completeRemoteLogin(t, mgr, postConnectAction(t, fx, member.clientID))
	}

	return ctx, connectedGateway{fx: fx, metaServerID: metaServerID, mgr: mgr, members: members}
}

// activeResource reads the RFC 8707 resource persisted on a subject's
// credential straight from the database.
func activeResource(t *testing.T, ctx context.Context, gw connectedGateway, clientID uuid.UUID) string {
	t.Helper()
	sess, err := remotesessions_repo.New(gw.fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            gw.fx.subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)
	return sess.Resource.String
}

// The resolved member must survive the whole chain — consent form, login
// state, token exchange, remote_sessions row — because everything downstream
// routes off the persisted value, not off what consent sent.
func TestGatewayCredentials_ConsentResolutionPersistsOnTheGrant(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-persist", 2, true)

	for _, member := range gw.members {
		require.Equal(t, consentExchangeCapture{HasResource: true, Resource: member.upstreamURL}, member.exchanged.Load(),
			"the code exchange must carry the member as its RFC 8707 resource indicator")
		require.Equal(t, member.upstreamURL, activeResource(t, ctx, gw, member.clientID),
			"remote_sessions.resource must name the member the client authorized for")
	}
	require.NotEqual(t, activeResource(t, ctx, gw, gw.members[0].clientID), activeResource(t, ctx, gw, gw.members[1].clientID),
		"two members must not share a resource")
	require.NotEqual(t, gw.fx.endpoint.UpstreamResource, activeResource(t, ctx, gw, gw.members[0].clientID),
		"the endpoint-level resource must never reach a grant")
}

// The feature's whole purpose: with grants qualified per member, a request to
// each member selects that member's own credential.
func TestGatewayCredentials_EachMemberSelectsItsOwnCredential(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-select", 3, true)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3)

	for _, member := range gw.members {
		got, err := gw.route(t, ctx, tokens, member.upstreamURL)
		require.NoError(t, err, "member %s must resolve a credential", member.upstreamURL)
		require.Equal(t, member.accessToken, got, "member %s must receive its own credential", member.upstreamURL)
	}

	// A backend nobody consented to gets nothing rather than somebody else's.
	got, err := gw.route(t, ctx, tokens, "https://aim87-select-unknown.example.com/mcp")
	require.Error(t, err)
	require.Empty(t, got)
}

// The same gateway unstamped records unqualified grants, and routeUpstreamToken
// — untouched by this change — then serves no member at all. The negative that
// gives the positive above its meaning, and the pre-backfill state.
func TestGatewayCredentials_UnstampedGrantsFailClosedForEveryMember(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-unstamped", 3, false)

	for _, member := range gw.members {
		require.Equal(t, consentExchangeCapture{HasResource: false, Resource: ""}, member.exchanged.Load(), "no resource indicator is sent when no member matches")
		require.Empty(t, activeResource(t, ctx, gw, member.clientID), "an unstamped member cannot qualify its grant")
	}

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3)

	for _, member := range gw.members {
		got, err := gw.route(t, ctx, tokens, member.upstreamURL)
		require.Error(t, err, "member %s must not be served from an unqualified credential", member.upstreamURL)
		require.Empty(t, got)
	}
}

// The complement, and pre-existing routeUpstreamToken behaviour this change
// leaves alone: a lone credential is forwarded even to a member it was not
// minted for. Why qualification matters once a gateway holds a second member.
func TestGatewayCredentials_LoneUnqualifiedCredentialIsStillForwarded(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-lone", 1, false)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 1)

	got, err := gw.route(t, ctx, tokens, "https://aim87-lone-other.example.com/mcp")
	require.NoError(t, err, "the lone-credential pass-through is deliberate")
	require.Equal(t, gw.members[0].accessToken, got)
}

// A grant qualified before a member was detached stays bound to the member it
// was minted for, rather than drifting onto a surviving one — while a fresh
// consent for that same client stops resolving the detached member at all.
func TestGatewayCredentials_StaleGrantStaysBoundToItsOwnMember(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-stale", 2, true)

	removed := gw.members[0]
	_, err := metamcp_repo.New(gw.fx.ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{
		ID:        removed.seeded.memberID,
		ProjectID: gw.fx.projectID,
	})
	require.NoError(t, err)

	// The detach is only observable through a lookup that runs after it, and consent
	// is where that runs. A detached member no longer claims its client's
	// authorization server, so a new grant carries no resource.
	_, hasResource := postConnectAction(t, gw.fx, removed.clientID).Query()["resource"]
	require.False(t, hasResource, "a detached member must stop qualifying its client's credential")
	survivor := gw.members[1]
	require.Equal(t, survivor.upstreamURL, postConnectAction(t, gw.fx, survivor.clientID).Query().Get("resource"),
		"and the surviving member must still qualify its own")

	// The credentials minted before the detach are untouched by it.
	tokens := gw.resolveTokens(t, ctx)
	got, err := gw.route(t, ctx, tokens, removed.upstreamURL)
	require.NoError(t, err)
	require.Equal(t, removed.accessToken, got, "the stale credential stays bound to the member it was minted for")

	got, err = gw.route(t, ctx, tokens, survivor.upstreamURL)
	require.NoError(t, err)
	require.Equal(t, survivor.accessToken, got, "and the surviving member still selects its own")
}
