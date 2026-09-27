package remotesessions_test

import (
	"testing"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

// Seed the durable claim directly: migration protection must not depend on
// registration selection or on the preparation endpoint being deployed.
func TestIssuerMigration_ActiveBindingsBlockUntilExplicitlyUnlinked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "ema-migration-source")
	target := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "ema-migration-target")
	user := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "ema-migration-human")
	q := repo.New(ti.conn)
	key := repo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: source, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, key))
	// Presentation edits stay available, but binding-sensitive configuration
	// cannot change and global handlers must not inspect tenant bindings.
	name := "Renamed provider"
	_, err := ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: source.String(), Name: &name})
	require.NoError(t, err)
	endpoint := "https://idp.example.com/changed-token"
	_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: source.String(), TokenEndpoint: &endpoint})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.UpdateGlobalIssuer(withAdmin(t, ctx), &adminrsgen.UpdateGlobalIssuerPayload{ID: source.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	deletion, err := ti.service.GetIssuerDeletePreflight(ctx, &orgissuersgen.GetIssuerDeletePreflightPayload{ID: source.String()})
	require.NoError(t, err)
	require.Equal(t, int64(1), deletion.EmaBindingCount)
	requireOopsCode(t, ti.service.DeleteIssuer(ctx, &orgissuersgen.DeleteIssuerPayload{ID: source.String()}), oops.CodeConflict)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(source.String(), target.String()))
	require.NoError(t, err)
	require.Equal(t, int64(1), preflight.EmaBindingCount)
	require.False(t, preflight.CanMigrate)
	_, err = ti.service.MigrateIssuer(ctx, migratePayload(source.String(), target.String()))
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	preflight, err = ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(source.String(), target.String()))
	require.NoError(t, err)
	require.Zero(t, preflight.EmaBindingCount)
	require.True(t, preflight.CanMigrate)
	_, err = ti.service.MigrateIssuer(ctx, migratePayload(source.String(), target.String()))
	require.NoError(t, err)
	count, err := testrepo.New(ti.conn).CountPreparationFixtureBindingByID(ctx, testrepo.CountPreparationFixtureBindingByIDParams{ID: binding.ID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Zero(t, count, "migration removes the explicitly unlinked source claim")
}
