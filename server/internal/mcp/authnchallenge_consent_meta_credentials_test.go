// The point of per-member credential selection is that a later request to one
// meta MCP member forwards that member's credential and no other. These tests
// carry a real meta MCP through consent, authorize, code exchange and persisted
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
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// connectedMetaMember is one member of a meta MCP whose client has completed
// the whole OAuth round trip.
type connectedMetaMember struct {
	upstreamURL string
	accessToken string
	clientID    uuid.UUID
	seeded      seededMetaMember
	// exchanged holds what this member's authorization server saw on the
	// token POST.
	exchanged *atomic.Value
}

// connectedMeta is a meta MCP with count remote members, each behind its own
// live authorization server, each with a connected client.
type connectedMeta struct {
	fx           consentActionFixture
	metaServerID uuid.UUID
	mgr          *remotesessions.ChallengeManager
	members      []connectedMetaMember
}

// resolveTokens runs the production credential resolver for the meta MCP's
// issuer and subject — the same map the serving path routes from.
func (g connectedMeta) resolveTokens(t *testing.T, ctx context.Context) map[uuid.UUID]remotesessions.UpstreamToken {
	t.Helper()
	tokens, err := g.mgr.ResolveAccessTokens(ctx, g.fx.projectID, g.fx.orgID, g.fx.shared, g.fx.subject)
	require.NoError(t, err)
	return tokens
}

// tokenFor returns the resolved credential for one member's client. The
// selection rule that spends these maps (routeUpstreamToken) is pinned by its
// own unit table in serveendpoint_internal_test.go; these tests assert the
// grant state consent persisted — the writer half of that contract.
func tokenFor(t *testing.T, tokens map[uuid.UUID]remotesessions.UpstreamToken, clientID uuid.UUID) remotesessions.UpstreamToken {
	t.Helper()
	// The map is keyed by remote_session_issuer_id; each entry names the
	// client its credential belongs to.
	for _, token := range tokens {
		if token.RemoteSessionClientID == clientID {
			return token
		}
	}
	require.Failf(t, "no credential resolved", "client %s", clientID)
	return remotesessions.UpstreamToken{}
}

// connectMeta seeds a meta MCP with count remote members and drives each
// client through consent, authorize and code exchange, so every credential is
// persisted the way production writes it. Each member gets its own
// authorization server and is stamped with it — the state the resync produces.
// stamped=false leaves the column NULL, reproducing the pre-backfill fleet.
func connectMeta(t *testing.T, prefix string, count int, stamped bool) (context.Context, connectedMeta) {
	t.Helper()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, prefix+"-gw")

	captured := make([]atomic.Value, count)
	members := make([]connectedMetaMember, 0, count)
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
		members = append(members, connectedMetaMember{
			upstreamURL: upstream,
			accessToken: accessToken,
			clientID:    clientID,
			seeded:      createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, name+"-member", upstream, issuerID, int32(i)),
			exchanged:   &captured[i],
		})
	}

	// Every member exists before any client connects, so consent always sees
	// the whole meta MCP.
	mgr := newConsentCallbackManager(t, fx.ti)
	for _, member := range members {
		completeRemoteLogin(t, mgr, postConnectAction(t, fx, member.clientID))
	}

	return ctx, connectedMeta{fx: fx, metaServerID: metaServerID, mgr: mgr, members: members}
}

// activeResource reads the RFC 8707 resource persisted on a subject's
// credential straight from the database.
func activeResource(t *testing.T, ctx context.Context, gw connectedMeta, clientID uuid.UUID) string {
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
func TestMetaMCPCredentials_ConsentResolutionPersistsOnTheGrant(t *testing.T) {
	t.Parallel()

	ctx, gw := connectMeta(t, "aim87-persist", 2, true)

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
func TestMetaMCPCredentials_EachMemberSelectsItsOwnCredential(t *testing.T) {
	t.Parallel()

	ctx, gw := connectMeta(t, "aim87-select", 3, true)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3)

	resources := map[string]bool{}
	for _, member := range gw.members {
		token := tokenFor(t, tokens, member.clientID)
		require.Equal(t, member.accessToken, token.Token, "member %s must hold its own credential", member.upstreamURL)
		require.Equal(t, member.upstreamURL, token.Resource, "the credential must be qualified to its member")
		resources[token.Resource] = true
	}
	// Every resource is distinct and none matches a backend nobody consented
	// to, so the exact-match selection rule cannot serve an unknown backend.
	require.Len(t, resources, 3)
	require.NotContains(t, resources, "https://aim87-select-unknown.example.com/mcp")
}

// The same meta MCP unstamped records unqualified grants, and routeUpstreamToken
// — untouched by this change — then serves no member at all. The negative that
// gives the positive above its meaning, and the pre-backfill state.
func TestMetaMCPCredentials_UnstampedGrantsFailClosedForEveryMember(t *testing.T) {
	t.Parallel()

	ctx, gw := connectMeta(t, "aim87-unstamped", 3, false)

	for _, member := range gw.members {
		require.Equal(t, consentExchangeCapture{HasResource: false, Resource: ""}, member.exchanged.Load(), "no resource indicator is sent when no member matches")
		require.Empty(t, activeResource(t, ctx, gw, member.clientID), "an unstamped member cannot qualify its grant")
	}

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 3)

	// Several credentials, all unqualified: the selection rule (exact match,
	// lone-token fallback only for a single credential) serves none of them.
	for _, member := range gw.members {
		require.Empty(t, tokenFor(t, tokens, member.clientID).Resource,
			"member %s must not hold a qualified credential", member.upstreamURL)
	}
}

// The complement, and pre-existing routeUpstreamToken behaviour this change
// leaves alone: a lone credential is forwarded even to a member it was not
// minted for. Why qualification matters once a meta MCP holds a second member.
func TestMetaMCPCredentials_LoneUnqualifiedCredentialIsStillForwarded(t *testing.T) {
	t.Parallel()

	ctx, gw := connectMeta(t, "aim87-lone", 1, false)

	tokens := gw.resolveTokens(t, ctx)
	require.Len(t, tokens, 1)

	// One credential, no resource: exactly the state the selection rule's
	// deliberate lone-token pass-through forwards to any backend.
	token := tokenFor(t, tokens, gw.members[0].clientID)
	require.Equal(t, gw.members[0].accessToken, token.Token)
	require.Empty(t, token.Resource)
}

// A grant qualified before a member was detached stays bound to the member it
// was minted for, rather than drifting onto a surviving one — while a fresh
// consent for that same client stops resolving the detached member at all.
func TestMetaMCPCredentials_StaleGrantStaysBoundToItsOwnMember(t *testing.T) {
	t.Parallel()

	ctx, gw := connectMeta(t, "aim87-stale", 2, true)

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

	// The credentials minted before the detach are untouched by it: each
	// stays qualified to the member it was minted for, so the exact-match
	// selection rule keeps them bound rather than drifting.
	tokens := gw.resolveTokens(t, ctx)
	removedToken := tokenFor(t, tokens, removed.clientID)
	require.Equal(t, removed.accessToken, removedToken.Token, "the stale credential stays bound to the member it was minted for")
	require.Equal(t, removed.upstreamURL, removedToken.Resource)
	survivorToken := tokenFor(t, tokens, survivor.clientID)
	require.Equal(t, survivor.accessToken, survivorToken.Token, "and the surviving member keeps its own")
	require.Equal(t, survivor.upstreamURL, survivorToken.Resource)
}
