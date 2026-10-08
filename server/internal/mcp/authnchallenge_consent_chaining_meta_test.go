package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestConsentPage_MetaGovernedRemoteMemberIsManaged(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-chained")
	const upstream = "https://meta-chained.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-chained-member", upstream, conv.ToNullUUID(issuerID), 0)
	g := setConsentGovernor(t, fx, issuerID, upstream)

	code, page, loc := render(t, fx)
	require.Equal(t, http.StatusOK, code, "a governed sole service must not auto-connect")
	require.Nil(t, loc)
	require.Contains(t, page, consentManagedCopy)
	require.Contains(t, page, "0 of 1 connected")
	require.Contains(t, page, "Connect manually")

	reqs := g.seen()
	require.NotEmpty(t, reqs)
	require.Equal(t, upstream, reqs[0].UpstreamResource)
	require.False(t, reqs[0].RemoteSessionIssuerID.Valid, "the first decision is the runtime's own request for a remote member")
	require.Equal(t, fx.shared, reqs[0].UserSessionIssuerID)
	require.Equal(t, fx.projectID, reqs[0].ProjectID)
}

func TestConsentPage_MetaUngovernedMemberStillConnectsInteractively(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-unchained")
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-unchained-member", "https://meta-unchained.example.com/mcp", conv.ToNullUUID(issuerID), 0)
	setConsentGovernor(t, fx, issuerID)

	code, _, loc := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code)
	require.NotNil(t, loc)
	require.Equal(t, "aim61-meta-unchained-as.example.com", loc.Host)
}

func TestConsentPage_MetaCardWithAnUngovernedMemberIsNotChained(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-partial")
	const governed = "https://meta-partial-a.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-partial-a", governed, conv.ToNullUUID(issuerID), 0)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-partial-b", "https://meta-partial-b.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	setConsentGovernor(t, fx, issuerID, governed)

	code, page, loc := render(t, fx)
	require.NotContains(t, page, consentManagedCopy)
	if code == http.StatusOK {
		require.Contains(t, page, "0 of 1 connected")
		return
	}
	require.Equal(t, http.StatusSeeOther, code)
	require.NotNil(t, loc)
	require.Equal(t, "aim61-meta-partial-as.example.com", loc.Host, "an unchained sole service auto-connects interactively")
}

func TestConsentPage_MetaUnclaimedProviderIsNotChained(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-unclaimed")
	const upstream = "https://meta-unclaimed.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-unclaimed-member", upstream, uuid.NullUUID{}, 0)
	g := setConsentGovernor(t, fx, issuerID, upstream)

	_, page, _ := render(t, fx)
	require.NotContains(t, page, consentManagedCopy, "a served member that does not authenticate against the card's issuer does not chain the card")
	require.Empty(t, g.seen(), "no member claims the card's issuer, so nothing is judged")
}

func TestConsentPage_MetaGovernedTunneledMemberCarriesCardIssuer(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-tunnel")
	const identifier = "urn:gram:tunnel:aim61-meta"
	createTunneledMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-tunnel-member", identifier, conv.ToNullUUID(issuerID), 0)
	g := setConsentGovernor(t, fx, issuerID, identifier)

	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, consentManagedCopy)

	reqs := g.seen()
	require.NotEmpty(t, reqs)
	for _, req := range reqs {
		require.Equal(t, uuid.NullUUID{UUID: issuerID, Valid: true}, req.RemoteSessionIssuerID, "a tunneled member is identified by its own issuer")
	}
}

func TestServeConsentAction_MetaConnectGovernedServiceStillAuthorizes(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "aim61-meta-action")
	const upstream = "https://meta-action.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-action-member", upstream, conv.ToNullUUID(issuerID), 0)
	setConsentGovernor(t, fx, issuerID, upstream)
	fx.clientA = clientID

	loc := postChainedConnect(t, fx)
	require.Equal(t, "aim61-meta-action-as.example.com", loc.Host, "manual connect stays available as a fallback")
}

func TestConsentPage_MetaMemberServedThroughAnotherIssuerIsNotChained(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, _, issuerID := metaConsent(t, "aim61-meta-other")
	const upstream = "https://meta-other.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-other-member", upstream, conv.ToNullUUID(issuerID), 0)
	setConsentGovernor(t, fx, uuid.New(), upstream)

	_, page, _ := render(t, fx)
	require.NotContains(t, page, consentManagedCopy, "the binding chaining selects belongs to another authorization server")
}

func TestServeConsentAction_MetaConnectUngovernedServiceAuthorizesInteractively(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "aim61-meta-action-open")
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "aim61-meta-action-open-member", "https://meta-action-open.example.com/mcp", conv.ToNullUUID(issuerID), 0)
	setConsentGovernor(t, fx, issuerID)
	fx.clientA = clientID

	loc := postChainedConnect(t, fx)
	require.Equal(t, "aim61-meta-action-open-as.example.com", loc.Host)
}
