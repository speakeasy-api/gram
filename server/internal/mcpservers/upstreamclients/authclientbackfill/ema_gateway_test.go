package authclientbackfill

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// The organization and project gateways sign users in through the
// organization's sole organization-level user session issuer and chain each
// member in its own project, so a binding for that issuer reaches the
// project's servers through the gateways whatever issuer they gate direct
// requests with.

const gatewayResource = "https://mcp.vendor.example.com/mcp"

// organizationIssuer seeds an organization-level user session issuer.
func (f *fixture) organizationIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedOrganizationUserSessionIssuerFixture(f.t.Context(), SeedOrganizationUserSessionIssuerFixtureParams{
		ID:             id,
		OrganizationID: pgtype.Text{String: f.orgID, Valid: true},
		Slug:           "org-usi-" + id.String()[:8],
	}))
	return id
}

// gatewaySetting records an administrator's gateway decision: an opt-in is a
// soft-deleted flag, turning the gateway off a live one.
func (f *fixture) gatewaySetting(on bool) {
	f.t.Helper()
	deletedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if on {
		deletedAt = pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
	}
	require.NoError(f.t, f.q.SeedOrganizationFeatureFixture(f.t.Context(), SeedOrganizationFeatureFixtureParams{
		OrganizationID: f.orgID,
		FeatureName:    "organization_gateway_disabled",
		DeletedAt:      deletedAt,
	}))
}

// inProject is f seeding into another project of the same organization.
func (f *fixture) inProject() *fixture {
	f.t.Helper()
	other := *f
	other.projectID = uuid.New()
	require.NoError(f.t, f.q.SeedProjectFixture(f.t.Context(), SeedProjectFixtureParams{ID: other.projectID, Slug: "p-" + other.projectID.String()[:8], OrganizationID: f.orgID}))
	return &other
}

func (f *fixture) selections(serverID uuid.UUID) int64 {
	f.t.Helper()
	n, err := f.q.CountServerSelectionsFixture(f.t.Context(), serverID)
	require.NoError(f.t, err)
	return n
}

// gatewayFixture seeds an organization that opted in to its gateway, a
// server gated directly by a project issuer, and an EMA upstream at the
// server's resource.
func gatewayFixture(t *testing.T) (f *fixture, gatewayIssuer, server, clientID, issuerID uuid.UUID) {
	t.Helper()
	f = newFixture(t)
	gatewayIssuer = f.organizationIssuer()
	f.gatewaySetting(true)
	server = f.serverAt(gatewayResource, f.userSessionIssuer())
	clientID, issuerID = f.emaClient()
	return f, gatewayIssuer, server, clientID, issuerID
}

func requireNotMatched(t *testing.T, f *fixture, report Report) {
	t.Helper()
	ambiguous := results(report, KindEMA, OutcomeAmbiguous)
	require.Len(t, ambiguous, 1)
	require.Equal(t, ReasonResourceNotMatched, ambiguous[0].Reason)
	require.Empty(t, f.live(TypeEMA))
}

func TestRun_EMAGatewayIssuerReachesItsProjectsServers(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{"read"})

	dry := f.run(false)
	applied := f.run(true)
	require.Equal(t, dry.Outcomes, applied.Outcomes, "dry run matches apply")
	require.Len(t, results(applied, KindEMA, OutcomeCreated), 1)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server), "the server offers the client its gateway members chain")
	require.Zero(t, f.selections(server), "no credential's choice is made for it")

	rerun := f.run(true)
	require.Zero(t, rerun.ClientsWritten)
	require.Zero(t, rerun.LinksWritten)
	require.Zero(t, rerun.PendingRetiredClients)
	require.Zero(t, rerun.PendingRetiredLinks)
	require.Equal(t, ema, f.live(TypeEMA), "a rerun keeps the client")
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server), "a rerun keeps the link")
}

func TestRun_EMAProjectIssuerDoesNotReachAServerGatedByAnother(t *testing.T) {
	t.Parallel()
	f, _, server, clientID, issuerID := gatewayFixture(t)
	f.binding(f.userSessionIssuer(), issuerID, clientID, gatewayResource, []string{})

	requireNotMatched(t, f, f.run(true))
	require.Empty(t, f.links(server))
}

func TestRun_EMAGatewayNeedsTheSoleOrganizationIssuer(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	f.organizationIssuer()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	requireNotMatched(t, f, f.run(true))
	require.Empty(t, f.links(server), "with several organization issuers no gateway is served")
}

func TestRun_EMAGatewayNeverConfiguredIsNotReached(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.organizationIssuer()
	server := f.serverAt(gatewayResource, f.userSessionIssuer())
	clientID, issuerID := f.emaClient()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	requireNotMatched(t, f, f.run(true))
	require.Empty(t, f.links(server))
}

func TestRun_EMAGatewayTurnedOffKeepsItsClient(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)

	f.gatewaySetting(false)
	dry := f.run(false)
	require.Zero(t, dry.PendingRetiredClients, "turning the gateway off is not a configuration change")
	require.Zero(t, dry.PendingRetiredLinks)
	f.run(true)
	require.Equal(t, ema, f.live(TypeEMA))
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server))
}

func TestRun_EMAGatewayClientRetiresWithASecondOrganizationIssuer(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)

	f.organizationIssuer()
	f.run(true)
	require.Empty(t, f.live(TypeEMA), "no gateway reaches the server any more")
	require.Empty(t, f.links(server))
	deleted, err := f.q.GetEMADeletedFixture(t.Context(), ema[0].ID)
	require.NoError(t, err)
	require.True(t, deleted)
}

func TestRun_EMAGatewayStaysInTheBindingsProjectAndResource(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	other := f.inProject()
	otherProject := other.serverAt(gatewayResource, other.userSessionIssuer())
	otherResource := f.serverAt("https://mcp.other.example.com/mcp", f.userSessionIssuer())
	otherResourceSameIssuer := f.serverAt("https://mcp.other.example.com/mcp", gatewayIssuer)
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server))
	require.Empty(t, f.links(otherProject), "a gateway chains a member in its own project only")
	require.Empty(t, f.links(otherResource), "a gateway chains only the binding's resource")
	require.Empty(t, f.links(otherResourceSameIssuer), "the binding's issuer reaches only the binding's resource, directly too")

	for _, serverID := range []uuid.UUID{otherProject, otherResource, otherResourceSameIssuer} {
		n, err := f.q.LinkEMAServer(t.Context(), LinkEMAServerParams{OrganizationID: f.orgID, AuthClientID: ema[0].ID, McpServerID: serverID})
		require.NoError(t, err)
		require.Zero(t, n, "the writer checks the reach itself")
	}
}

func TestRun_EMAGatewayAndDirectBindingsShareOneLink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.organizationIssuer()
	f.gatewaySetting(true)
	direct := f.userSessionIssuer()
	server := f.serverAt(gatewayResource, direct)
	clientID, issuerID := f.emaClient()
	f.binding(direct, issuerID, clientID, gatewayResource, []string{})
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	applied := f.run(true)
	created := results(applied, KindEMA, OutcomeCreated)
	require.Len(t, created, 1)
	require.Len(t, created[0].BindingIDs, 2)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server), "one link however many ways the server is reached")
	require.Equal(t, 1, applied.LinksWritten)
}

func TestRun_AdminDetachedEMAGatewayLinkStaysDetached(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, clientID, issuerID := gatewayFixture(t)
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)

	f.adminDetach(server, ema[0].ID)
	f.requireDetachHolds()
	require.Empty(t, f.links(server))

	n, err := f.q.LinkEMAServer(t.Context(), LinkEMAServerParams{OrganizationID: f.orgID, AuthClientID: ema[0].ID, McpServerID: server})
	require.NoError(t, err)
	require.Zero(t, n, "the write repeats the detach check")
}

// directHeaderChoice seeds a server gated by its own issuer whose source
// carries the given credential header, and a direct-route choice the backfill
// maps onto the header client derived from it.
func directHeaderChoice(t *testing.T, header string) (f *fixture, gatewayIssuer, server, headerClient, credential uuid.UUID) {
	t.Helper()
	f = newFixture(t)
	gatewayIssuer = f.organizationIssuer()
	f.gatewaySetting(true)
	source, server := f.emaSourceAt(gatewayResource, f.sessionIssuer())
	f.header(source, header, true, secretValue, "")
	credential = f.interactiveCredential()
	f.noAuthSelection(credential, server)
	require.Equal(t, 1, f.run(true).SelectionsMapped)
	static := f.live(TypeStaticCredential)
	require.Len(t, static, 1)
	return f, gatewayIssuer, server, static[0].ID, credential
}

// requireDirectChoiceKept checks, across reruns, that a gateway-only EMA
// client is offered beside the direct route's header client, which stays live
// with the direct choice mapped onto it and usable.
func requireDirectChoiceKept(t *testing.T, f *fixture, server, headerClient, credential uuid.UUID) {
	t.Helper()
	for range 2 {
		report := f.run(true)
		require.Zero(t, report.PendingRetiredClients, "the direct route's header client is kept")
		require.Zero(t, report.PendingRetiredLinks)
		require.Empty(t, report.SelectionGaps, "the direct choice stays usable")
		require.Equal(t, 1, report.Selections[ClassInteractive].Outcomes[SelectionMapped])
		require.Equal(t, []uuid.UUID{headerClient}, ids(f.live(TypeStaticCredential)))
		ema := f.live(TypeEMA)
		require.Len(t, ema, 1)
		require.ElementsMatch(t, []uuid.UUID{headerClient, ema[0].ID}, f.links(server))
		row := f.selection(credential, server)
		require.Equal(t, uuid.NullUUID{UUID: headerClient, Valid: true}, row.AuthClientID, "the choice is never moved onto the EMA client")
		require.False(t, row.NoAuth)
	}
}

func ids(rows []ListAuthClientSummariesFixtureRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func TestRun_EMAGatewayReachKeepsADirectAuthorizationChoice(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, headerClient, credential := directHeaderChoice(t, "Authorization")
	clientID, issuerID := f.emaClient()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	requireDirectChoiceKept(t, f, server, headerClient, credential)
}

// Without an Authorization header, a server an EMA client serves directly
// carries its auxiliary credential headers on that client instead of a
// standalone header client. One reached only through the gateways keeps the
// standalone client for its direct route; the EMA client carries copies too.
func TestRun_EMAGatewayReachKeepsADirectAuxiliaryHeaderChoice(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, headerClient, credential := directHeaderChoice(t, "X-Api-Key")
	clientID, issuerID := f.emaClient()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	requireDirectChoiceKept(t, f, server, headerClient, credential)
	headers := f.headers(f.live(TypeEMA)[0].ID)
	require.Len(t, headers, 1)
	require.Equal(t, "X-Api-Key", headers[0].Name)
}

// A choice made through the gateways, whose credential the gateway issuer
// issued, is served by the EMA client there, so reading it as no-auth is stale
// as beside any OAuth-backed client.
func TestRun_EMAGatewayReachLeavesAGatewayNoAuthChoiceStale(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, _, _ := gatewayFixture(t)
	clientID, issuerID := f.emaClient()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	f.sessionIssuerID = gatewayIssuer
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	gaps := selectionGaps(report, SelectionStaleNoAuth)
	require.Len(t, gaps, 1)
	require.Equal(t, credential, gaps[0].CredentialID)
	row := f.selection(credential, server)
	require.True(t, row.NoAuth, "a stale choice is reported, never remapped")
	require.False(t, row.AuthClientID.Valid)
}

// A server an EMA client also reaches directly keeps today's behavior: its
// auxiliary credential headers move onto the EMA client.
func TestRun_EMADirectAndGatewayReachMovesAuxiliaryHeadersOntoTheEMAClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.organizationIssuer()
	f.gatewaySetting(true)
	direct := f.userSessionIssuer()
	source, server := f.emaSourceAt(gatewayResource, direct)
	f.header(source, "X-Api-Key", true, secretValue, "")
	clientID, issuerID := f.emaClient()
	f.binding(direct, issuerID, clientID, gatewayResource, []string{})
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	f.run(true)
	require.Empty(t, f.live(TypeStaticCredential))
	emaID := f.live(TypeEMA)[0].ID
	require.Equal(t, []uuid.UUID{emaID}, f.links(server))
	require.Len(t, f.headers(emaID), 1)
}

// A binding for another exact resource does not change gateway-only reach:
// the direct route keeps its header client and choice.
func TestRun_KeptEMAGatewayClientKeepsADirectAuxiliaryHeaderChoice(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, headerClient, credential := directHeaderChoice(t, "X-Api-Key")
	clientID, issuerID := f.emaClient()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	requireDirectChoiceKept(t, f, server, headerClient, credential)

	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource+"/", []string{})
	report := f.run(false)
	unmatched := results(report, KindEMA, OutcomeAmbiguous)
	require.Len(t, unmatched, 1)
	require.Equal(t, ReasonResourceNotMatched, unmatched[0].Reason)
	requireDirectChoiceKept(t, f, server, headerClient, credential)
}

// Only the gateways' issuer makes a gateway-only EMA client count for a
// choice: a credential another issuer issued, even one with its own binding
// for the upstream, is not served by it on this server.
func TestRun_EMAGatewayReachCountsOnlyForGatewayCredentials(t *testing.T) {
	t.Parallel()
	f, gatewayIssuer, server, headerClient, _ := directHeaderChoice(t, "X-Api-Key")
	clientID, issuerID := f.emaClient()
	other := f.userSessionIssuer()
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})
	f.binding(other, issuerID, clientID, gatewayResource, []string{})
	f.sessionIssuerID = other
	credential := f.interactiveCredential()
	f.noAuthSelection(credential, server)

	report := f.run(true)
	require.Empty(t, selectionGaps(report, SelectionStaleNoAuth))
	require.Equal(t, uuid.NullUUID{UUID: headerClient, Valid: true}, f.selection(credential, server).AuthClientID)
}
