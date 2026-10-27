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
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, f.requiresUpstreamAuth(server))
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
	f.binding(gatewayIssuer, issuerID, clientID, gatewayResource, []string{})

	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	require.Equal(t, []uuid.UUID{ema[0].ID}, f.links(server))
	require.Empty(t, f.links(otherProject), "a gateway chains a member in its own project only")
	require.Empty(t, f.links(otherResource), "a gateway chains only the binding's resource")

	for _, serverID := range []uuid.UUID{otherProject, otherResource} {
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
