package associationbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// gatewayClient creates a project client bound to userIssuerID and returns it
// with its remote session issuer.
func (f *fixture) gatewayClient(userIssuerID uuid.UUID) (clientID, remoteIssuerID uuid.UUID) {
	f.t.Helper()
	ctx := f.t.Context()
	remoteIssuerID, clientID = uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        remoteIssuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + remoteIssuerID.String()[:8],
		Issuer:    "https://" + remoteIssuerID.String()[:8] + ".example.com",
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(f.t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userIssuerID}))
	return clientID, remoteIssuerID
}

func (f *fixture) gateway(userIssuerID uuid.UUID, memberIDs ...uuid.UUID) {
	f.t.Helper()
	ctx := f.t.Context()
	gatewayID := uuid.New()
	require.NoError(f.t, f.q.SeedGatewayFixture(ctx, SeedGatewayFixtureParams{
		ID:                  gatewayID,
		OrganizationID:      f.orgID,
		ProjectID:           f.projectID,
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
	}))
	for _, memberID := range memberIDs {
		require.NoError(f.t, f.q.SeedGatewayMemberFixture(ctx, SeedGatewayMemberFixtureParams{ProjectID: f.projectID, MetaMcpServerID: gatewayID, McpServerID: memberID}))
	}
}

func (f *fixture) organizationIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedOrganizationIssuerFixture(f.t.Context(), SeedOrganizationIssuerFixtureParams{
		ID:             id,
		OrganizationID: pgtype.Text{String: f.orgID, Valid: true},
		Slug:           "org-usi-" + id.String()[:8],
	}))
	return id
}

// grant records a subject's live grant for clientID under userIssuerID.
func (f *fixture) grant(userIssuerID, clientID uuid.UUID, resource string) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedGrantFixture(f.t.Context(), SeedGrantFixtureParams{
		SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()[:8]),
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		Resource:              pgtype.Text{String: resource, Valid: resource != ""},
	}))
}

func (f *fixture) derive(serverID, remoteIssuerID uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.SetDerivedIssuerFixture(f.t.Context(), SetDerivedIssuerFixtureParams{
		ID:                    serverID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteIssuerID, Valid: true},
	}))
}

func gatewayResultFor(t *testing.T, report Report, serverID uuid.UUID) GatewayResult {
	t.Helper()
	for _, result := range report.GatewayResults {
		if result.MCPServerID == serverID {
			return result
		}
	}
	require.FailNow(t, "gateway pair not decided", serverID.String())
	return GatewayResult{}
}

func TestRunAssociatesGatewayMembersFromGrantsThenDerivedIssuers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()

	granted := f.server(f.userIssuer(), "https://granted.example.com/mcp")
	grantedClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, grantedClient, "https://granted.example.com/mcp/")

	derived := f.server(f.userIssuer(), "https://derived.example.com/mcp")
	derivedClient, derivedIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(derived, derivedIssuer)

	// A grant decides over a derived issuer that agrees with it.
	agreeing := f.server(f.userIssuer(), "https://agreeing.example.com/mcp")
	agreeingClient, agreeingIssuer := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, agreeingClient, "https://agreeing.example.com/mcp")
	f.derive(agreeing, agreeingIssuer)

	conflicting := f.server(f.userIssuer(), "https://conflicting.example.com/mcp")
	conflictGrant, _ := f.gatewayClient(gatewayIssuer)
	_, conflictIssuer := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, conflictGrant, "https://conflicting.example.com/mcp")
	f.derive(conflicting, conflictIssuer)

	unserved := f.server(f.userIssuer(), "https://public.example.com/mcp")

	detached := f.server(f.userIssuer(), "https://detached.example.com/mcp")
	detachedClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, detachedClient, "https://detached.example.com/mcp")
	f.associate(detached, detachedClient, true)

	f.gateway(gatewayIssuer, granted, derived, agreeing, conflicting, unserved, detached)

	dry := f.run(false)
	require.Equal(t, GatewayOutcomeGrant, gatewayResultFor(t, dry, granted).Outcome)
	require.Equal(t, GatewayOutcomeDerived, gatewayResultFor(t, dry, derived).Outcome)
	require.Equal(t, GatewayOutcomeGrant, gatewayResultFor(t, dry, agreeing).Outcome)
	require.Equal(t, GatewayOutcomeConflict, gatewayResultFor(t, dry, conflicting).Outcome)
	require.Equal(t, GatewayOutcomeUnserved, gatewayResultFor(t, dry, unserved).Outcome)
	require.Equal(t, GatewayOutcomeDetached, gatewayResultFor(t, dry, detached).Outcome)
	require.Equal(t, 1, dry.Gaps, "only the conflict needs configuration")
	require.Empty(t, f.associations(granted), "a dry run writes nothing")

	applied := f.run(true)
	require.Equal(t, 3, applied.Written)
	require.Equal(t, []uuid.UUID{grantedClient}, f.associations(granted))
	require.Equal(t, []uuid.UUID{derivedClient}, f.associations(derived))
	require.Equal(t, []uuid.UUID{agreeingClient}, f.associations(agreeing))
	require.Empty(t, f.associations(conflicting))
	require.Empty(t, f.associations(unserved))
	require.Empty(t, f.associations(detached), "an explicit detach is kept")

	again := f.run(true)
	require.Zero(t, again.Written)
	require.Equal(t, GatewayOutcomeConfigured, gatewayResultFor(t, again, granted).Outcome)
}

func TestRunLeavesAGatewayMemberSeveralClientsMatchForConfiguration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://shared.example.com/mcp")
	first, _ := f.gatewayClient(gatewayIssuer)
	second, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, first, "https://shared.example.com/mcp")
	f.grant(gatewayIssuer, second, "https://shared.example.com/mcp")
	f.gateway(gatewayIssuer, member)

	report := f.run(true)
	require.Equal(t, GatewayOutcomeAmbiguous, gatewayResultFor(t, report, member).Outcome)
	require.Empty(t, f.associations(member))
}

func TestRunAssociatesOrganizationGatewayServers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	organizationIssuer := f.organizationIssuer()
	server := f.server(f.userIssuer(), "https://org-gateway.example.com/mcp")
	client, _ := f.gatewayClient(organizationIssuer)
	f.grant(organizationIssuer, client, "https://org-gateway.example.com/mcp")

	f.run(true)
	require.Equal(t, []uuid.UUID{client}, f.associations(server))
}

func TestRunIgnoresAnOrganizationIssuerThatIsNotTheOnlyOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	organizationIssuer := f.organizationIssuer()
	f.organizationIssuer()
	server := f.server(f.userIssuer(), "https://org-gateway.example.com/mcp")
	client, _ := f.gatewayClient(organizationIssuer)
	f.grant(organizationIssuer, client, "https://org-gateway.example.com/mcp")

	report := f.run(true)
	require.Empty(t, report.GatewayResults, "with several organization issuers there is no organization gateway")
	require.Empty(t, f.associations(server))
}

func TestReconcileCapturesAndKeepsGatewayAssociations(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	client, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(member, remoteIssuer)
	f.gateway(gatewayIssuer, member)

	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Equal(t, []uuid.UUID{client}, f.associations(member), "a binding change on the gateway issuer captures the member")

	// The client is bound only to the gateway's issuer, not the member's own,
	// and the association survives the prune that follows every binding change.
	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, nil))
	require.Equal(t, []uuid.UUID{client}, f.associations(member))

	require.NoError(t, f.q.DeleteClientBindingsFixture(t.Context(), client))
	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, nil))
	require.Empty(t, f.associations(member), "no issuer reaching the member binds the client any more")
}

func TestReconcileDoesNotCaptureAGatewayMemberAGrantRoutesElsewhere(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	_, remoteIssuer := f.gatewayClient(gatewayIssuer)
	granted, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, granted, "https://member.example.com/mcp")
	f.derive(member, remoteIssuer)
	f.gateway(gatewayIssuer, member)

	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Empty(t, f.associations(member), "the grant names another client, so the derived match proves nothing")
}

// project adds another project to the fixture's organization.
func (f *fixture) project() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedProjectFixture(f.t.Context(), SeedProjectFixtureParams{ID: id, Slug: "p-" + id.String()[:8], OrganizationID: f.orgID}))
	return id
}

// serverIn creates a remote-backed server in projectID on userIssuerID.
func (f *fixture) serverIn(projectID, userIssuerID uuid.UUID, upstreamURL string) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	remoteID, serverID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(ctx, SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: projectID, Url: upstreamURL}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(ctx, SeedMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           projectID,
		Slug:                pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remoteID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
	}))
	return serverID
}

func TestRunIgnoresServersAndGatewaysInDeletedProjects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.organizationIssuer()
	server := f.server(issuer, "https://kept.example.com/mcp")
	client := f.client(issuer, "https://kept-as.example.com")

	deleted := f.project()
	f.serverIn(deleted, issuer, "https://gone.example.com/mcp")
	require.NoError(t, f.q.SeedGatewayFixture(t.Context(), SeedGatewayFixtureParams{
		ID:                  uuid.New(),
		OrganizationID:      f.orgID,
		ProjectID:           deleted,
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true},
	}))
	require.NoError(t, f.q.SoftDeleteProjectFixture(t.Context(), deleted))

	report := f.run(true)
	require.Equal(t, OutcomeSoleConsumer, resultFor(t, report, server).Outcome, "a server or gateway in a deleted project consumes nothing")
	require.Zero(t, report.Gaps)
	require.Contains(t, f.associations(server), client)
}

func TestRunIgnoresAGatewayIssuerOfAnotherProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := f.project()
	foreignIssuer := uuid.New()
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:        foreignIssuer,
		ProjectID: uuid.NullUUID{UUID: other, Valid: true},
		Slug:      "usi-" + foreignIssuer.String()[:8],
	}))
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	client, _ := f.gatewayClient(foreignIssuer)
	f.grant(foreignIssuer, client, "https://member.example.com/mcp")
	f.gateway(foreignIssuer, member)

	report := f.run(true)
	require.Empty(t, report.GatewayResults, "a gateway issuer scoped to another project fronts nothing here")
	require.Empty(t, f.associations(member))
}

func TestRunTreatsAServerWhoseSourceIsDeletedAsUnproxied(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.userIssuer()
	server := f.server(issuer, "https://deleted-source.example.com/mcp")
	f.server(issuer, "https://sibling.example.com/mcp")
	f.client(issuer, "https://as.example.com")
	require.NoError(t, f.q.SoftDeleteRemoteMCPServersOfServerFixture(t.Context(), server))

	report := f.run(false)
	require.Equal(t, OutcomeUnproxied, resultFor(t, report, server).Outcome, "a server without a live source serves nothing through a client")
}

// tunnel creates a tunnel-backed server on userIssuerID whose tunnel claims
// resource.
func (f *fixture) tunnel(userIssuerID uuid.UUID, resource string) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	tunnelID, serverID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedTunneledMCPServerFixture(ctx, SeedTunneledMCPServerFixtureParams{
		ID:                 tunnelID,
		ProjectID:          f.projectID,
		Name:               "tunnel-" + tunnelID.String()[:8],
		KeyHash:            "hash-" + tunnelID.String(),
		ResourceIdentifier: pgtype.Text{String: resource, Valid: true},
	}))
	require.NoError(f.t, f.q.SeedTunneledServerFixture(ctx, SeedTunneledServerFixtureParams{
		ID:                  serverID,
		ProjectID:           f.projectID,
		Slug:                pgtype.Text{String: "t-" + serverID.String()[:8], Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
	}))
	return serverID
}

func TestRunDecidesTunnelsByDerivedIssuerNotClaimedResource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()

	// A tunnel claiming the resource another client holds grants for proves
	// nothing about that client.
	claimed := f.tunnel(f.userIssuer(), "https://shared.internal.example/mcp")
	grantedClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, grantedClient, "https://shared.internal.example/mcp")

	// Its derived authorization server still decides it, whatever grants for
	// its claimed resource say.
	derived := f.tunnel(f.userIssuer(), "https://derived.internal.example/mcp")
	derivedClient, derivedIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(derived, derivedIssuer)
	otherClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, otherClient, "https://derived.internal.example/mcp")

	f.gateway(gatewayIssuer, claimed, derived)

	report := f.run(true)
	require.Equal(t, GatewayOutcomeUnserved, gatewayResultFor(t, report, claimed).Outcome)
	require.Equal(t, GatewayOutcomeDerived, gatewayResultFor(t, report, derived).Outcome)
	require.Empty(t, f.associations(claimed))
	require.Equal(t, []uuid.UUID{derivedClient}, f.associations(derived))
}

func TestReconcileCapturesATunnelWhoseClaimedResourceAnotherClientHoldsGrantsFor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.tunnel(f.userIssuer(), "https://member.internal.example/mcp")
	client, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(member, remoteIssuer)
	other, _ := f.gatewayClient(gatewayIssuer)
	f.grant(gatewayIssuer, other, "https://member.internal.example/mcp")
	f.gateway(gatewayIssuer, member)

	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Equal(t, []uuid.UUID{client}, f.associations(member))
}

func TestReconcileRecapturesAMemberThatRejoinsItsGateway(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	client, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(member, remoteIssuer)
	f.gateway(gatewayIssuer, member)
	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Equal(t, []uuid.UUID{client}, f.associations(member))

	require.NoError(t, f.q.RemoveGatewayMemberFixture(t.Context(), member))
	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Empty(t, f.associations(member), "leaving the gateway prunes the association")

	f.gateway(gatewayIssuer, member)
	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Equal(t, []uuid.UUID{client}, f.associations(member), "a pruned association is not a detach")
}

func TestReconcileKeepsAnExplicitGatewayDetach(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	client, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(member, remoteIssuer)
	f.associate(member, client, true)
	f.gateway(gatewayIssuer, member)

	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Empty(t, f.associations(member))
}

func TestRunSkipsAGatewayWriteWhoseDecisionChanged(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	derivedClient, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(member, remoteIssuer)
	otherClient, _ := f.gatewayClient(gatewayIssuer)
	f.gateway(gatewayIssuer, member)

	decided := gatewayResultFor(t, f.run(false), member)
	require.Equal(t, GatewayOutcomeDerived, decided.Outcome)
	require.Equal(t, derivedClient, decided.Associate.UUID)

	// A grant for another client lands between the read and the write, so a
	// fresh decision would be a conflict.
	f.grant(gatewayIssuer, otherClient, "https://member.example.com/mcp")

	n, err := f.q.BackfillGatewayAssociation(t.Context(), BackfillGatewayAssociationParams{
		UserSessionIssuerID:   gatewayIssuer,
		RemoteSessionClientID: derivedClient,
		McpServerID:           member,
	})
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, f.associations(member))
}

// A grant is one subject's credential for one client, so a grant minted
// through a sibling issuer for a client the gateway binds is evidence too.
func TestRunCountsGrantsMintedThroughAnotherIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	siblingIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	client, _ := f.gatewayClient(gatewayIssuer)
	f.grant(siblingIssuer, client, "https://member.example.com/mcp")

	derivedMember := f.server(f.userIssuer(), "https://derived-member.example.com/mcp")
	_, remoteIssuer := f.gatewayClient(gatewayIssuer)
	f.derive(derivedMember, remoteIssuer)
	otherClient, _ := f.gatewayClient(gatewayIssuer)
	f.grant(siblingIssuer, otherClient, "https://derived-member.example.com/mcp")

	f.gateway(gatewayIssuer, member, derivedMember)

	report := f.run(true)
	require.Equal(t, GatewayOutcomeGrant, gatewayResultFor(t, report, member).Outcome)
	require.Equal(t, []uuid.UUID{client}, f.associations(member))
	require.Equal(t, GatewayOutcomeConflict, gatewayResultFor(t, report, derivedMember).Outcome)

	require.NoError(t, upstreamclients.Reconcile(t.Context(), f.pool, f.orgID, f.projectID, []uuid.UUID{gatewayIssuer}))
	require.Empty(t, f.associations(derivedMember), "the sibling grant routes the member to another client, so capture writes nothing")
}

// A platform client is shared by every organization, so its grants minted in
// another organization say nothing about which client serves this one.
func TestRunIgnoresGrantsMintedInAnotherOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	ctx := t.Context()
	gatewayIssuer := f.userIssuer()
	member := f.server(f.userIssuer(), "https://member.example.com/mcp")
	remoteIssuerID, clientID := uuid.New(), uuid.New()
	require.NoError(t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        remoteIssuerID,
		ProjectID: uuid.NullUUID{},
		Slug:      "rsi-" + remoteIssuerID.String()[:8],
		Issuer:    "https://" + remoteIssuerID.String()[:8] + ".example.com",
	}))
	require.NoError(t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: gatewayIssuer}))
	other.grant(other.userIssuer(), clientID, "https://member.example.com/mcp")
	f.gateway(gatewayIssuer, member)

	report := f.run(true)
	require.Equal(t, GatewayOutcomeUnserved, gatewayResultFor(t, report, member).Outcome)
	require.Empty(t, f.associations(member))
}

// boundClient creates a project client on remoteIssuerID bound to every
// userIssuerIDs, so several clients can share one authorization server.
func (f *fixture) boundClient(remoteIssuerID uuid.UUID, userIssuerIDs ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	clientID := uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	for _, userIssuerID := range userIssuerIDs {
		require.NoError(f.t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userIssuerID}))
	}
	return clientID
}

// dedicatedMember creates a server on its own issuer, derived to
// remoteIssuerID, with a dedicated client on that authorization server bound
// to its issuer and gatewayIssuer and associated with it.
func (f *fixture) dedicatedMember(gatewayIssuer, remoteIssuerID uuid.UUID, upstreamURL string) (serverID, clientID uuid.UUID) {
	f.t.Helper()
	issuer := f.userIssuer()
	serverID = f.server(issuer, upstreamURL)
	f.derive(serverID, remoteIssuerID)
	clientID = f.boundClient(remoteIssuerID, issuer, gatewayIssuer)
	f.associate(serverID, clientID, false)
	return serverID, clientID
}

// A gateway fronts several servers on one authorization server, so every
// client on it matches every member's derived issuer. Each member was moved
// from a shared client to a dedicated one: the shared association was
// removed and the dedicated one added. That sole association settles each
// member, whatever grants the detached shared client holds.
func TestRunTreatsAGatewayMemberItsSoleAssociationSettlesAsConfigured(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	_, remoteIssuer := f.gatewayClient(gatewayIssuer)
	shared := f.boundClient(remoteIssuer, gatewayIssuer)
	// A client on another authorization server with no evidence for anyone.
	_, _ = f.gatewayClient(gatewayIssuer)

	// No grants: every client matches by derived issuer alone.
	noGrants, noGrantsClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://one.example.com/mcp")
	f.associate(noGrants, shared, true)

	// Grants for both the detached shared client and the dedicated one.
	bothGranted, bothGrantedClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://two.example.com/mcp")
	f.associate(bothGranted, shared, true)
	f.grant(gatewayIssuer, shared, "https://two.example.com/mcp")
	f.grant(gatewayIssuer, bothGrantedClient, "https://two.example.com/mcp")

	// Grants only for the detached shared client.
	detachedGranted, detachedGrantedClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://three.example.com/mcp")
	f.associate(detachedGranted, shared, true)
	f.grant(gatewayIssuer, shared, "https://three.example.com/mcp")

	// Grants only for the dedicated client.
	ownGranted, ownGrantedClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://four.example.com/mcp")
	f.associate(ownGranted, shared, true)
	f.grant(gatewayIssuer, ownGrantedClient, "https://four.example.com/mcp")

	// The shared client stays associated with one member, which has grants
	// for it.
	keptIssuer := f.userIssuer()
	kept := f.server(keptIssuer, "https://five.example.com/mcp")
	f.derive(kept, remoteIssuer)
	require.NoError(t, f.q.SeedClientBindingFixture(t.Context(), SeedClientBindingFixtureParams{RemoteSessionClientID: shared, UserSessionIssuerID: keptIssuer}))
	f.associate(kept, shared, false)
	f.grant(gatewayIssuer, shared, "https://five.example.com/mcp")

	f.gateway(gatewayIssuer, noGrants, bothGranted, detachedGranted, ownGranted, kept)
	want := map[uuid.UUID]uuid.UUID{
		noGrants:        noGrantsClient,
		bothGranted:     bothGrantedClient,
		detachedGranted: detachedGrantedClient,
		ownGranted:      ownGrantedClient,
		kept:            shared,
	}

	for _, apply := range []bool{false, true, true} {
		report := f.run(apply)
		require.Zero(t, report.Gaps)
		require.Zero(t, report.Decided)
		require.Zero(t, report.Written)
		for serverID, clientID := range want {
			result := gatewayResultFor(t, report, serverID)
			require.Equal(t, GatewayOutcomeConfigured, result.Outcome)
			require.False(t, result.Associate.Valid)
			require.Equal(t, []uuid.UUID{clientID}, f.associations(serverID), "nothing is associated or removed")
		}
	}
}

// Configuration settles nothing when it does not name exactly one of the
// issuer's clients, or when a client never associated with the member holds
// grants for it: requests route by those grants today.
func TestRunLeavesAGatewayMemberConfigurationDoesNotSettleForConfiguration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.userIssuer()
	_, remoteIssuer := f.gatewayClient(gatewayIssuer)

	// Two associated clients that both match by derived issuer.
	twoAssociated, first := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://one.example.com/mcp")
	second := f.boundClient(remoteIssuer, gatewayIssuer)
	f.associate(twoAssociated, second, false)

	// One associated client, but grants for a client never associated.
	foreignGrant, foreignGrantClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://two.example.com/mcp")
	foreign := f.boundClient(remoteIssuer, gatewayIssuer)
	f.grant(gatewayIssuer, foreign, "https://two.example.com/mcp")

	// One associated client, but grants for two clients never associated.
	twoForeignGrants, twoForeignGrantsClient := f.dedicatedMember(gatewayIssuer, remoteIssuer, "https://three.example.com/mcp")
	f.grant(gatewayIssuer, foreign, "https://three.example.com/mcp")
	f.grant(gatewayIssuer, second, "https://three.example.com/mcp")

	// Only a removed association.
	onlyDetached := f.server(f.userIssuer(), "https://four.example.com/mcp")
	f.derive(onlyDetached, remoteIssuer)
	f.associate(onlyDetached, f.boundClient(remoteIssuer, gatewayIssuer), true)

	f.gateway(gatewayIssuer, twoAssociated, foreignGrant, twoForeignGrants, onlyDetached)
	for range 2 {
		report := f.run(true)
		require.Equal(t, GatewayOutcomeAmbiguous, gatewayResultFor(t, report, twoAssociated).Outcome)
		require.Equal(t, GatewayOutcomeConflict, gatewayResultFor(t, report, foreignGrant).Outcome)
		require.Equal(t, GatewayOutcomeAmbiguous, gatewayResultFor(t, report, twoForeignGrants).Outcome)
		require.Equal(t, GatewayOutcomeAmbiguous, gatewayResultFor(t, report, onlyDetached).Outcome)
		require.Equal(t, 4, report.Gaps)
		require.ElementsMatch(t, []uuid.UUID{first, second}, f.associations(twoAssociated))
		require.Equal(t, []uuid.UUID{foreignGrantClient}, f.associations(foreignGrant))
		require.Equal(t, []uuid.UUID{twoForeignGrantsClient}, f.associations(twoForeignGrants))
		require.Empty(t, f.associations(onlyDetached))
	}
}
