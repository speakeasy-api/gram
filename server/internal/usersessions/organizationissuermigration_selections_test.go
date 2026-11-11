package usersessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Existing target credentials must learn each newly reachable client, not just
// credentials moved off the source. Shared issuers exercise the path where
// sole-consumer capture cannot maintain the typed associations for us.
func TestOrganizationUserSessionIssuerMigrateReconcilesCredentialChoices(t *testing.T) {
	t.Parallel()
	for _, enforced := range []bool{false, true} {
		t.Run(fmt.Sprintf("enforced=%t", enforced), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			if enforced {
				_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
			}
			target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "selection-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
			require.NoError(t, err)
			targetID := uuid.MustParse(target.ID)
			existing, err := seedUserSession(t, ctx, ti.conn, targetID, urn.NewUserSubject("existing"))
			require.NoError(t, err)
			bystanderID := seedIssuer(t, ctx, ti, "selection-bystander")
			bystander, err := seedUserSession(t, ctx, ti.conn, bystanderID, urn.NewUserSubject("bystander"))
			require.NoError(t, err)
			expired, err := seedUserSessionWithExpiry(t, ctx, ti.conn, targetID, urn.NewUserSubject("expired"), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
			require.NoError(t, err)
			fixtures := selectionbackfill.New(ti.conn)
			grantParams := authclientbackfill.ListAuthClientGrantRowsParams{OrganizationID: auth.ActiveOrganizationID, Scopes: []string{string(authz.ScopeMCPAuth), string(authz.ScopeMCPConnect)}}
			grantsBefore, err := authclientbackfill.New(ti.conn).ListAuthClientGrantRows(ctx, grantParams)
			require.NoError(t, err)
			credentials := []uuid.UUID{migrationCredentialID(existing)}
			servers, clients := []uuid.UUID{}, []uuid.UUID{}
			var firstAuthClient uuid.UUID
			for i := range 2 {
				// The second consolidation adds a server in a different project. The
				// organization target's pre-existing credentials still reach that server.
				projectID := *auth.ProjectID
				if i == 1 {
					projectID = createSiblingProject(t, ctx, ti.conn, "selection-sibling")
				}
				sourceID := seedIssuerInProject(t, ctx, ti.conn, projectID, fmt.Sprintf("selection-source-%d", i))
				serverID, clientID := seedIssuerMigrationOAuthServer(t, ctx, ti, projectID, sourceID)
				moved, err := seedUserSession(t, ctx, ti.conn, sourceID, urn.NewUserSubject(fmt.Sprintf("moved-%d", i)))
				require.NoError(t, err)
				// A choice recorded before typed clients existed must keep its row
				// identity, registration and issuance provenance while gaining the
				// uniquely determined typed client after consolidation.
				mappingRepo := authclientbackfill.New(ti.conn)
				require.NoError(t, mappingRepo.SeedOAuthSelectionFixture(ctx, authclientbackfill.SeedOAuthSelectionFixtureParams{
					ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID,
					CredentialID: migrationCredentialID(moved), McpServerID: serverID, RemoteSessionClientID: clientID,
				}))
				before, err := mappingRepo.ListSelectionMappings(ctx, authclientbackfill.ListSelectionMappingsParams{
					OrganizationID: auth.ActiveOrganizationID, PageSize: 10,
					ScopeUserSessionIssuerID: conv.ToNullUUID(sourceID),
				})
				require.NoError(t, err)
				require.Len(t, before, 1)
				require.False(t, before[0].AuthClientID.Valid)
				credentials = append(credentials, migrationCredentialID(moved))
				servers, clients = append(servers, serverID), append(clients, clientID)
				preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
				require.NoError(t, err)
				_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
				require.NoError(t, err)
				after, err := mappingRepo.ListSelectionMappings(ctx, authclientbackfill.ListSelectionMappingsParams{
					OrganizationID: auth.ActiveOrganizationID, PageSize: 1, OnlyID: conv.ToNullUUID(before[0].ID),
				})
				require.NoError(t, err)
				require.Len(t, after, 1)
				require.Equal(t, before[0].ID, after[0].ID)
				require.Equal(t, before[0].RemoteSessionClientID, after[0].RemoteSessionClientID)
				require.True(t, after[0].AuthClientID.Valid)
				oldChoice, err := mappingRepo.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: migrationCredentialID(moved), McpServerID: serverID})
				require.NoError(t, err)
				require.Equal(t, upstreamclients.SelectionSourceIssuance, oldChoice.Source)
				for _, credentialID := range credentials {
					rows, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID})
					require.NoError(t, err)
					require.Len(t, rows, len(servers))
					for j, server := range servers {
						choice, err := usersessionsrepo.New(ti.conn).GetUpstreamClientSelection(ctx, usersessionsrepo.GetUpstreamClientSelectionParams{UserSessionIssuerID: targetID, OrganizationID: auth.ActiveOrganizationID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID, McpServerID: server})
						require.NoError(t, err)
						require.Equal(t, clients[j], choice.RemoteSessionClientID.UUID)
						typed, err := fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID, McpServerID: server})
						require.NoError(t, err)
						require.True(t, typed.AuthClientID.Valid, "each new choice names its typed auth client")
						if i == 0 && credentialID == credentials[0] {
							firstAuthClient = typed.AuthClientID.UUID
						}
						if i == 1 && j == 0 && credentialID == credentials[0] {
							require.Equal(t, firstAuthClient, typed.AuthClientID.UUID, "an existing typed choice is preserved")
						}
					}
				}
			}
			// A recorded typed choice remains authoritative even after the issuer
			// binds a second eligible registration. A new credential must not guess.
			undecided, err := seedUserSession(t, ctx, ti.conn, targetID, urn.NewUserSubject("undecided"))
			require.NoError(t, err)
			thirdID := seedIssuer(t, ctx, ti, "selection-third")
			_, thirdClient := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, thirdID)
			require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: servers[0], RemoteSessionClientID: thirdClient}))
			_, err = authclientsrepo.New(ti.conn).DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{OrganizationID: auth.ActiveOrganizationID, McpServerID: servers[0], AuthClientID: firstAuthClient})
			require.NoError(t, err)
			_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: thirdID.String(), TargetID: target.ID})
			require.NoError(t, err)
			explicit, err := fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentials[0], McpServerID: servers[0]})
			require.NoError(t, err)
			require.Equal(t, firstAuthClient, explicit.AuthClientID.UUID, "an unusable typed choice is never replaced")
			_, err = fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(undecided), McpServerID: servers[0]})
			require.ErrorIs(t, err, pgx.ErrNoRows, "ambiguous missing choices require reconnection")
			links, err := authclientsrepo.New(ti.conn).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: servers[0]})
			require.NoError(t, err)
			for _, link := range links {
				require.NotEqual(t, firstAuthClient, link.ID, "durable administrator detaches must survive consolidation")
			}
			grantsAfter, err := authclientbackfill.New(ti.conn).ListAuthClientGrantRows(ctx, grantParams)
			require.NoError(t, err)
			require.ElementsMatch(t, grantsBefore, grantsAfter, "consolidation must not create or restore permissions")
			for _, credentialID := range []uuid.UUID{migrationCredentialID(bystander), migrationCredentialID(expired)} {
				rows, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID})
				require.NoError(t, err)
				require.Empty(t, rows, "unaffected or expired credentials must not be backfilled")
			}
		})
	}
}

func migrationCredentialID(session usersessionsrepo.UserSession) uuid.UUID {
	if session.CredentialID.Valid {
		return session.CredentialID.UUID
	}
	return session.ID
}

func seedIssuerMigrationOAuthServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := selectionbackfill.New(ti.conn)
	providerID, clientID, backendID, serverID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionIssuerFixture(ctx, selectionbackfill.SeedRemoteSessionIssuerFixtureParams{ID: providerID, ProjectID: conv.ToNullUUID(projectID), Slug: "provider-" + providerID.String(), Issuer: "https://" + providerID.String() + ".example.com"}))
	require.NoError(t, fixtures.SeedRemoteSessionClientFixture(ctx, selectionbackfill.SeedRemoteSessionClientFixtureParams{ID: clientID, ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), RemoteSessionIssuerID: providerID, ClientID: clientID.String()}))
	require.NoError(t, fixtures.SeedClientBindingFixture(ctx, selectionbackfill.SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: issuerID}))
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, selectionbackfill.SeedRemoteMCPServerFixtureParams{ID: backendID, ProjectID: projectID}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, selectionbackfill.SeedMCPServerFixtureParams{ID: serverID, ProjectID: projectID, Slug: conv.ToPGText("server-" + serverID.String()), RemoteMcpServerID: conv.ToNullUUID(backendID), UserSessionIssuerID: conv.ToNullUUID(issuerID)}))
	require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, RemoteSessionClientID: clientID}))
	return serverID, clientID
}

func TestOrganizationUserSessionIssuerMigrateScopesExistingTargetButNotMovedCredentials(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "bounded-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)
	first, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, targetID)
	second, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, targetID)
	syncIssuerMigrationTestServers(t, ctx, ti, first, second)
	sourceID := seedIssuer(t, ctx, ti, "bounded-source")
	impact, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, sourceID)
	existing, err := seedUserSession(t, ctx, ti.conn, targetID, urn.NewUserSubject("bounded-existing"))
	require.NoError(t, err)
	moved, err := seedUserSession(t, ctx, ti.conn, sourceID, urn.NewUserSubject("bounded-moved"))
	require.NoError(t, err)
	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.Equal(t, 1, result.SessionsMigrated)
	fixtures := selectionbackfill.New(ti.conn)
	existingChoices, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(existing)})
	require.NoError(t, err)
	require.Len(t, existingChoices, 1, "unaffected pre-existing gaps are not an organization-wide backfill")
	require.Equal(t, impact, existingChoices[0].McpServerID)
	movedChoices, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(moved)})
	require.NoError(t, err)
	require.Len(t, movedChoices, 3, "moved source lineages need all target reach, not only typed maintenance")
	for _, choice := range movedChoices {
		typed, err := fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(moved), McpServerID: choice.McpServerID})
		require.NoError(t, err)
		require.True(t, typed.AuthClientID.Valid)
		require.Equal(t, upstreamclients.SelectionSourceBackfill, choice.Source)
	}
}

func TestOrganizationUserSessionIssuerMigrateMaintainsMovedGatewayMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	targetID := seedIssuer(t, ctx, ti, "gateway-maintenance-target")
	sourceID := seedIssuer(t, ctx, ti, "gateway-maintenance-source")
	otherID := seedIssuer(t, ctx, ti, "gateway-maintenance-other")
	direct, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, targetID)
	member, registration := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, otherID)
	fixtures := selectionbackfill.New(ti.conn)
	require.NoError(t, fixtures.SeedClientBindingFixture(ctx, selectionbackfill.SeedClientBindingFixtureParams{RemoteSessionClientID: registration, UserSessionIssuerID: targetID}))
	syncIssuerMigrationTestServers(t, ctx, ti, direct, member)
	gatewayID := uuid.New()
	require.NoError(t, fixtures.SeedMetaMCPServerFixture(ctx, selectionbackfill.SeedMetaMCPServerFixtureParams{ID: gatewayID, OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, Name: "Moved gateway", UserSessionIssuerID: conv.ToNullUUID(sourceID)}))
	require.NoError(t, fixtures.SeedMetaMCPServerMemberFixture(ctx, selectionbackfill.SeedMetaMCPServerMemberFixtureParams{ProjectID: *auth.ProjectID, MetaMcpServerID: gatewayID, McpServerID: member}))
	existing, err := seedUserSession(t, ctx, ti.conn, targetID, urn.NewUserSubject("gateway-existing"))
	require.NoError(t, err)
	moved, err := seedUserSession(t, ctx, ti.conn, sourceID, urn.NewUserSubject("gateway-moved"))
	require.NoError(t, err)
	rows, err := usersessionsrepo.New(ti.conn).ListMCPServersForUserSessionIssuerMigration(ctx, usersessionsrepo.ListMCPServersForUserSessionIssuerMigrationParams{OrganizationID: auth.ActiveOrganizationID, IssuerIds: []uuid.UUID{sourceID, targetID}, SourceIssuerID: sourceID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.ID == member {
			require.True(t, row.NeedsSync, "a gateway move creates reach even without transferring its registration")
		}
	}
	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	existingChoices, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(existing)})
	require.NoError(t, err)
	require.Len(t, existingChoices, 1)
	require.Equal(t, member, existingChoices[0].McpServerID)
	movedChoices, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(moved)})
	require.NoError(t, err)
	require.Len(t, movedChoices, 2)
}

func syncIssuerMigrationTestServers(t *testing.T, ctx context.Context, ti *testInstance, servers ...uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.LockEMAWriterScope(ctx, tx, auth.ActiveOrganizationID, []uuid.UUID{*auth.ProjectID}))
	require.NoError(t, authclientbackfill.PreflightServerAssociations(ctx, tx, auth.ActiveOrganizationID, servers...))
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, auth.ActiveOrganizationID, servers...))
	require.NoError(t, tx.Commit(ctx))
}
