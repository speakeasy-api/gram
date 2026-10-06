//go:build demoseed_safety

package demoseed

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
)

// Visitors can leave both completed bindings and in-flight preparation claims.
// Neither is seeded, so ordinary idempotence checks miss their required FKs.
func TestReseedCleansVisitorEMABindings(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	queries := demoseedtest.New(db)
	seedLocalPostgres(ctx, t, db, otherTenantSpec)
	// Seed the other tenant first so its full baseline can be checked unchanged.
	plantBindings := func(spec Spec) {
		t.Helper()
		for _, state := range []string{"ready", "in_progress"} {
			count, err := queries.PlantVisitorEMABinding(ctx, demoseedtest.PlantVisitorEMABindingParams{
				OrganizationID: conv.ToPGText(spec.OrgID),
				ProjectID:      spec.ProjectID(),
				State:          state,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, count, "binding must reference actual seeded parents")
		}
	}
	plantBindings(otherTenantSpec)
	unattached, err := queries.CountVisitorEMABindingsWithoutAttachment(ctx, demoseedtest.CountVisitorEMABindingsWithoutAttachmentParams{
		OrganizationID: otherTenantSpec.OrgID, ProjectID: otherTenantSpec.ProjectID(),
	})
	require.NoError(t, err)
	require.Zero(t, unattached, "bindings must use an attached issuer, not the first tenant issuer")
	before, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)

	seedLocalPostgres(ctx, t, db, DefaultSpec())
	plantBindings(DefaultSpec())
	seedLocalPostgres(ctx, t, db, DefaultSpec())

	count, err := queries.CountVisitorEMABindings(ctx, demoseedtest.CountVisitorEMABindingsParams{
		OrganizationID: DefaultSpec().OrgID,
		ProjectID:      DefaultSpec().ProjectID(),
	})
	require.NoError(t, err)
	require.Zero(t, count, "reseed must remove visitor bindings and active claims")
	after, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	requirePostgresRowsPreserved(t, before, after)
}

// Fixture IDs alone must never authorize mutations, including organization-tier
// rows and client-to-user-issuer links that do not carry their own tenant columns.
func TestRemoteSessionFixturesRejectForeignTenants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "fixture_scope")
	require.NoError(t, err)
	seedLocalPostgres(ctx, t, db, DefaultSpec())
	seedLocalPostgres(ctx, t, db, otherTenantSpec)
	queries := demoseedtest.New(db)
	parents, err := queries.GetVisitorEMAFixtureParents(ctx, demoseedtest.GetVisitorEMAFixtureParentsParams{
		OrganizationID: conv.ToPGText(DefaultSpec().OrgID), ProjectID: DefaultSpec().ProjectID(),
	})
	require.NoError(t, err)
	foreign, err := queries.GetVisitorEMAFixtureParents(ctx, demoseedtest.GetVisitorEMAFixtureParentsParams{
		OrganizationID: conv.ToPGText(otherTenantSpec.OrgID), ProjectID: otherTenantSpec.ProjectID(),
	})
	require.NoError(t, err)
	clientID, issuerID := uuid.UUID(parents.ClientID.Bytes), uuid.UUID(parents.RemoteSessionIssuerID.Bytes)
	project := conv.ToNullUUID(uuid.MustParse(DefaultSpec().ProjectID()))
	organization := conv.ToPGText(DefaultSpec().OrgID)
	q := testrepo.New(db)
	before, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	for _, scope := range []struct {
		name         string
		project      uuid.NullUUID
		organization string
	}{
		{"wrong organization", project, otherTenantSpec.OrgID},
		{"wrong project", conv.ToNullUUID(uuid.MustParse(otherTenantSpec.ProjectID())), DefaultSpec().OrgID},
		{"organization tier cannot match project row", uuid.NullUUID{}, DefaultSpec().OrgID},
	} {
		// Keep these attempts sequential before the snapshot and owner writes.
		t.Log(scope.name)
		{
			org := conv.ToPGText(scope.organization)
			n, err := q.ForceRemoteSessionClientRegistrationFixture(ctx, testrepo.ForceRemoteSessionClientRegistrationFixtureParams{ID: clientID, ProjectID: scope.project, OrganizationID: org})
			require.NoError(t, err)
			require.Zero(t, n)
			n, err = q.ForceRemoteSessionIssuerRegistrationEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerRegistrationEndpointFixtureParams{ClientID: clientID, ProjectID: scope.project, OrganizationID: org, RegistrationEndpoint: conv.ToPGText("https://fixture.example.invalid/register")})
			require.NoError(t, err)
			require.Zero(t, n)
			n, err = q.SoftDeleteRemoteSessionClientFixture(ctx, testrepo.SoftDeleteRemoteSessionClientFixtureParams{ID: clientID, ProjectID: scope.project, OrganizationID: org})
			require.NoError(t, err)
			require.Zero(t, n)
			n, err = q.SoftDeleteRemoteSessionIssuerFixture(ctx, testrepo.SoftDeleteRemoteSessionIssuerFixtureParams{ID: issuerID, ProjectID: scope.project, OrganizationID: org})
			require.NoError(t, err)
			require.Zero(t, n)
		}
	}
	n, err := q.AttachPreparationFixtureInteractiveClient(ctx, testrepo.AttachPreparationFixtureInteractiveClientParams{ID: clientID, UserID: uuid.UUID(foreign.UserSessionIssuerID.Bytes), ProjectID: project})
	require.NoError(t, err)
	require.Zero(t, n, "a valid client scope must not admit a foreign user issuer")
	after, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before, after, "rejected fixture mutations must leave all tenants unchanged")
	n, err = q.ForceRemoteSessionClientRegistrationFixture(ctx, testrepo.ForceRemoteSessionClientRegistrationFixtureParams{ID: clientID, ProjectID: project, OrganizationID: organization})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.ForceRemoteSessionIssuerRegistrationEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerRegistrationEndpointFixtureParams{ClientID: clientID, ProjectID: project, OrganizationID: organization, RegistrationEndpoint: conv.ToPGText("https://fixture.example.invalid/register")})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.SoftDeleteRemoteSessionClientFixture(ctx, testrepo.SoftDeleteRemoteSessionClientFixtureParams{ID: clientID, ProjectID: project, OrganizationID: organization})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.SoftDeleteRemoteSessionIssuerFixture(ctx, testrepo.SoftDeleteRemoteSessionIssuerFixtureParams{ID: issuerID, ProjectID: project, OrganizationID: organization})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}
