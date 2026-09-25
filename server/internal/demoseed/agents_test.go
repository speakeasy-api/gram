//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAgentGrantsReseed(t *testing.T) {
	t.Parallel()

	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			fixtures := testrepo.New(db)

			seedLocalPostgres(ctx, t, db, otherTenantSpec)
			outside, err := demoseedtest.SnapshotPostgres(ctx, db)
			require.NoError(t, err)
			seedLocalPostgres(ctx, t, db, spec)
			agentsBefore, err := fixtures.ListDemoSeedAgentsFixture(ctx, spec.OrgID)
			require.NoError(t, err)
			require.Len(t, agentsBefore, 10)
			principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agentsBefore[0].ID.String())

			// Use the target agent's URN in both organizations to catch cleanup
			// scoped only by principal, even when IDs ordinarily differ by tenant.
			otherGrant, err := fixtures.InsertDemoSeedPrincipalGrantFixture(ctx, testrepo.InsertDemoSeedPrincipalGrantFixtureParams{
				OrganizationID: otherTenantSpec.OrgID,
				PrincipalUrn:   principal,
			})
			require.NoError(t, err)
			// Human grants within the target organization must survive unchanged.
			humanGrant, err := fixtures.InsertDemoSeedPrincipalGrantFixture(ctx, testrepo.InsertDemoSeedPrincipalGrantFixtureParams{
				OrganizationID: spec.OrgID,
				PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, agentsBefore[0].OwnerUserID),
			})
			require.NoError(t, err)

			for range 2 {
				remote := remoterepo.New(db)
				issuer, err := remote.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{
					OrganizationID: pgtype.Text{String: spec.OrgID, Valid: true}, Slug: "org-owned-cleanup", Issuer: "https://cleanup.example.com",
					ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{},
				})
				require.NoError(t, err)
				client, err := remote.CreateRemoteSessionClient(ctx, remoterepo.CreateRemoteSessionClientParams{
					OrganizationID: pgtype.Text{String: spec.OrgID, Valid: true}, RemoteSessionIssuerID: issuer.ID, ClientID: "org-owned-cleanup",
				})
				require.NoError(t, err)

				// Also plant an orphan: no FK ties grants to the agents table.
				for _, p := range []urn.Principal{principal, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())} {
					_, err = fixtures.InsertDemoSeedPrincipalGrantFixture(ctx, testrepo.InsertDemoSeedPrincipalGrantFixtureParams{
						OrganizationID: spec.OrgID,
						PrincipalUrn:   p,
					})
					require.NoError(t, err)
				}
				seedLocalPostgres(ctx, t, db, spec)
				_, err = remote.GetRemoteSessionClientWithIssuerByID(ctx, client.ID)
				require.ErrorIs(t, err, pgx.ErrNoRows, "reseed must remove organization-owned clients")
				_, err = remote.GetRemoteSessionIssuerByID(ctx, remoterepo.GetRemoteSessionIssuerByIDParams{ID: issuer.ID, OrganizationID: pgtype.Text{String: spec.OrgID, Valid: true}, IncludeOrganizational: true})
				require.ErrorIs(t, err, pgx.ErrNoRows, "reseed must remove organization-owned issuers")

				grants, err := fixtures.CountDemoSeedAgentGrantsFixture(ctx, spec.OrgID)
				require.NoError(t, err)
				require.EqualValues(t, 2, grants, "recreated agents must retain only the two scoped seed policies")
				keys, err := fixtures.CountDemoSeedAPIKeysFixture(ctx, spec.OrgID)
				require.NoError(t, err)
				require.EqualValues(t, 3, keys, "shared SQL leaves only the three expired, inert agent keys checked by postflight")
				agentsAfter, err := fixtures.ListDemoSeedAgentsFixture(ctx, spec.OrgID)
				require.NoError(t, err)
				require.Equal(t, agentsBefore, agentsAfter)
				for _, grant := range []testrepo.GetDemoSeedPrincipalGrantFixtureParams{
					{OrganizationID: spec.OrgID, GrantJson: []byte(humanGrant)},
					{OrganizationID: otherTenantSpec.OrgID, GrantJson: []byte(otherGrant)},
				} {
					preserved, err := fixtures.GetDemoSeedPrincipalGrantFixture(ctx, grant)
					require.NoError(t, err)
					require.JSONEq(t, string(grant.GrantJson), preserved)
				}
				after, err := demoseedtest.SnapshotPostgres(ctx, db)
				require.NoError(t, err)
				requirePostgresRowsPreserved(t, outside, after)
			}
		})
	}
}
