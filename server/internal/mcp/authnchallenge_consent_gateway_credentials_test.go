// The point of consent-time member resolution is that a later request to one
// gateway member forwards that member's credential and no other. These tests
// carry a real gateway all the way through — consent, authorize, code
// exchange, persisted remote_sessions rows — and then hand the result to the
// production routing decision, so the feature is asserted end to end rather
// than at the redirect it starts with.

package mcp_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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
// member's client through consent, the authorize redirect, and the code
// exchange, so every credential is persisted the way production writes it.
//
// discoverable=false makes every member answer 404 at the well-known path,
// reproducing the pre-change state where no grant carries a resource.
func connectGateway(t *testing.T, prefix string, count int, discoverable bool) (context.Context, connectedGateway) {
	t.Helper()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, prefix+"-gw")

	captured := make([]atomic.Value, count)
	members := make([]connectedGatewayMember, 0, count)
	for i := range count {
		name := fmt.Sprintf("%s-%d", prefix, i)
		accessToken := "token-" + name
		as := newConsentExchangeAS(t, &captured[i], accessToken)

		upstream := newUpstreamWithoutMetadata(t).URL + "/mcp"
		if discoverable {
			upstream = newGatewayMemberUpstream(t, as.URL).URL + "/mcp"
		}

		members = append(members, connectedGatewayMember{
			upstreamURL: upstream,
			accessToken: accessToken,
			clientID:    createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, name, as.URL, []uuid.UUID{fx.shared}),
			seeded:      createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, name+"-member", upstream, int32(i)),
			exchanged:   &captured[i],
		})
	}
	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), count)

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

// The negative that gives the positive its meaning: the same three-member
// gateway whose members advertise no metadata records unqualified grants, and
// then no member can be served at all. This is the state before consent-time
// resolution, and it fails closed rather than forwarding an arbitrary token.
func TestGatewayCredentials_UnqualifiedGrantsFailClosedForEveryMember(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-unqualified", 3, false)

	for _, member := range gw.members {
		require.Equal(t, consentExchangeCapture{HasResource: false, Resource: ""}, member.exchanged.Load(), "no resource indicator is sent when discovery misses")
		require.Empty(t, activeResource(t, ctx, gw, member.clientID), "a member that advertises nothing cannot qualify its grant")
	}

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3)

	for _, member := range gw.members {
		got, err := gw.route(t, ctx, tokens, member.upstreamURL)
		require.Error(t, err, "member %s must not be served from an unqualified credential", member.upstreamURL)
		require.Empty(t, got)
	}
}

// The v1 limitation AIM-87 names, pinned so it cannot regress silently: a lone
// credential is forwarded even to a member it was not minted for. It is why
// qualification matters as soon as a gateway holds a second member.
func TestGatewayCredentials_LoneUnqualifiedCredentialIsStillForwarded(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-lone", 1, false)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 1)

	got, err := gw.route(t, ctx, tokens, "https://aim87-lone-other.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, gw.members[0].accessToken, got, "documented v1 limitation: a single credential is forwarded unqualified")
}

// Membership changes after a grant is recorded. The recorded resource is the
// member's identity, not its membership, so a detached member's credential
// stays bound to that member and never becomes another member's.
func TestGatewayCredentials_DetachedMemberDoesNotContestAnother(t *testing.T) {
	t.Parallel()

	ctx, gw := connectGateway(t, "aim87-churn", 3, true)

	removed := gw.members[1]
	_, err := metamcp_repo.New(gw.fx.ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{
		ID:        removed.seeded.memberID,
		ProjectID: gw.fx.projectID,
	})
	require.NoError(t, err)
	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, gw.fx.ti.conn, gw.fx.projectID, gw.metaServerID), 2)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3, "detaching a member does not revoke the subject's credential")

	for _, member := range []connectedGatewayMember{gw.members[0], gw.members[2]} {
		got, err := gw.route(t, ctx, tokens, member.upstreamURL)
		require.NoError(t, err)
		require.Equal(t, member.accessToken, got, "the surviving members keep their own credentials")
	}

	got, err := gw.route(t, ctx, tokens, removed.upstreamURL)
	require.NoError(t, err)
	require.Equal(t, removed.accessToken, got, "the stale credential stays bound to the member it was minted for")
}

// Trailing-slash spellings must survive the round trip: consent trims the
// member URL before recording it while the backend keeps its stored spelling,
// so the two only meet because routing trims too. A second member is present
// so the selection is a real choice rather than the lone-credential rule.
func TestGatewayCredentials_TrailingSlashSpellingsSelectTheSameCredential(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-slash-gw")

	var slashCaptured, plainCaptured atomic.Value
	slashAS := newConsentExchangeAS(t, &slashCaptured, "token-slash")
	plainAS := newConsentExchangeAS(t, &plainCaptured, "token-plain")

	// Advertised with a trailing slash, stored without; the member's own URL
	// carries one too.
	slashed := newGatewayMemberUpstream(t, slashAS.URL+"/").URL + "/mcp/"
	plain := newGatewayMemberUpstream(t, plainAS.URL).URL + "/mcp"
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-slash-member", slashed, 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-slash-plain", plain, 1)

	slashClient := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-slash", slashAS.URL, []uuid.UUID{fx.shared})
	plainClient := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-plain", plainAS.URL, []uuid.UUID{fx.shared})

	mgr := newConsentCallbackManager(t, fx.ti)
	gw := connectedGateway{fx: fx, metaServerID: metaServerID, mgr: mgr, members: nil}
	completeRemoteLogin(t, mgr, postConnectAction(t, fx, slashClient))
	completeRemoteLogin(t, mgr, postConnectAction(t, fx, plainClient))

	require.Equal(t, strings.TrimRight(slashed, "/"), activeResource(t, ctx, gw, slashClient), "the grant records the trimmed form resolveUpstreamResource derives")

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 2)

	// The backend's own resource is its stored URL, slash and all.
	got, err := gw.route(t, ctx, tokens, slashed)
	require.NoError(t, err)
	require.Equal(t, "token-slash", got, "the member's slashed URL must still select its own credential")

	got, err = gw.route(t, ctx, tokens, plain)
	require.NoError(t, err)
	require.Equal(t, "token-plain", got)
}
