package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestService_DeleteRole_PluginAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	roleID := seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_cleanup", "Cleanup", "cleanup", ""))
	principal := "role:organization:" + roleID
	otherRoleID := seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_keep", "Keep", "keep", ""))
	otherPrincipal := "role:organization:" + otherRoleID
	const otherOrg = "org_plugin_cleanup_other"
	seedOrganization(t, ctx, ti.conn, otherOrg)
	foreignRoleID := seedRole(t, ctx, ti.conn, otherOrg, mockRole("role_foreign", "Cleanup", "cleanup", ""))
	foreignPrincipal := "role:organization:" + foreignRoleID

	pluginID := seedRoleDeletionPlugin(t, ctx, ti.conn, ac.ActiveOrganizationID, []string{principal, otherPrincipal, "*"})
	roleOnlyID := seedRoleDeletionPlugin(t, ctx, ti.conn, ac.ActiveOrganizationID, []string{principal})
	archivedID := seedRoleDeletionPlugin(t, ctx, ti.conn, ac.ActiveOrganizationID, []string{principal, "*"})
	err := testrepo.New(ti.conn).ArchiveRoleDeletionPlugin(ctx, archivedID)
	require.NoError(t, err)
	// Even an independent assignment to this exact URN in another tenant must
	// remain untouched by organization-scoped deletion.
	foreignID := seedRoleDeletionPlugin(t, ctx, ti.conn, otherOrg, []string{principal, foreignPrincipal, "*"})
	before := roleDeletionPluginSnapshot(t, ctx, ti.conn)
	foreignBefore := roleDeletionAssignmentSnapshot(t, ctx, ti.conn, foreignID)
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "cleanup").Return(nil).Once()

	require.NoError(t, ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID}))
	for _, id := range []uuid.UUID{pluginID, roleOnlyID, archivedID} {
		count, err := testrepo.New(ti.conn).CountRoleDeletionPrincipalAssignments(ctx, testrepo.CountRoleDeletionPrincipalAssignmentsParams{PluginID: id, PrincipalUrn: principal})
		require.NoError(t, err)
		require.Zero(t, count)
	}
	remaining, err := testrepo.New(ti.conn).ListRoleDeletionPrincipals(ctx, pluginID)
	require.NoError(t, err)
	require.Equal(t, []string{"*", otherPrincipal}, remaining)
	require.Equal(t, before, roleDeletionPluginSnapshot(t, ctx, ti.conn))
	require.Equal(t, foreignBefore, roleDeletionAssignmentSnapshot(t, ctx, ti.conn, foreignID))

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	expectedAudiences := map[uuid.UUID][]string{
		pluginID:   {"*", otherPrincipal},
		roleOnlyID: {},
		archivedID: {"*"},
	}
	for _, id := range []uuid.UUID{pluginID, roleOnlyID, archivedID} {
		records, err := testrepo.New(ti.conn).ListRoleDeletionPluginAudits(ctx, testrepo.ListRoleDeletionPluginAuditsParams{PluginID: id, Action: string(audit.ActionPluginAssignmentsSet)})
		require.NoError(t, err)
		require.Len(t, records, 1)
		record := records[0]
		require.Equal(t, ac.ActiveOrganizationID, record.OrganizationID)
		require.True(t, record.ProjectID.Valid)
		require.Equal(t, record.PluginProjectID, record.ProjectID.UUID)
		require.Equal(t, ac.UserID, record.ActorID)
		require.Equal(t, "user", record.ActorType)
		require.Equal(t, id.String(), record.SubjectID)
		require.Equal(t, "plugin", record.SubjectType)
		metadata, err := audittest.DecodeAuditData(record.Metadata)
		require.NoError(t, err)
		require.Contains(t, metadata, "principal_urns")
		require.ElementsMatch(t, expectedAudiences[id], metadata["principal_urns"])
	}
	// Repeating local deletion retains the API's not-found contract.
	requireOopsCode(t, ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID}), oops.CodeNotFound)
	repeatedCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Equal(t, count, repeatedCount)

}

func TestService_DeleteRole_PluginCleanupFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	roleID := seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_cleanup", "Cleanup", "cleanup", ""))
	pluginID := seedRoleDeletionPlugin(t, ctx, ti.conn, ac.ActiveOrganizationID, []string{"role:organization:" + roleID, "*"})
	before := roleDeletionAssignmentSnapshot(t, ctx, ti.conn, pluginID)
	err := testrepo.New(ti.conn).InstallRoleDeletionFailure(ctx)
	require.NoError(t, err)
	require.Error(t, ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID}))
	role, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "cleanup"})
	require.NoError(t, err)
	require.False(t, role.Deleted)
	require.Equal(t, before, roleDeletionAssignmentSnapshot(t, ctx, ti.conn, pluginID))
}

func TestService_DeleteRole_PluginAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	roleID := seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_cleanup", "Cleanup", "cleanup", ""))
	pluginID := seedRoleDeletionPlugin(t, ctx, ti.conn, ac.ActiveOrganizationID, []string{"role:organization:" + roleID, "*"})
	before := roleDeletionAssignmentSnapshot(t, ctx, ti.conn, pluginID)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet))
	require.Error(t, ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID}))
	role, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "cleanup"})
	require.NoError(t, err)
	require.False(t, role.Deleted)
	require.Equal(t, before, roleDeletionAssignmentSnapshot(t, ctx, ti.conn, pluginID))
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Zero(t, count)
}

func seedRoleDeletionPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, principals []string) uuid.UUID {
	t.Helper()
	q := testrepo.New(conn)
	pluginID, err := q.SeedRoleDeletionPlugin(ctx, testrepo.SeedRoleDeletionPluginParams{OrganizationID: orgID, Slug: uuid.NewString()})
	require.NoError(t, err)
	for _, principal := range principals {
		_, err := q.SeedRoleDeletionAssignment(ctx, testrepo.SeedRoleDeletionAssignmentParams{PluginID: pluginID, OrganizationID: orgID, PrincipalUrn: principal})
		require.NoError(t, err)
	}
	return pluginID
}

func roleDeletionPluginSnapshot(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()
	snapshot, err := testrepo.New(conn).RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	return snapshot
}

func roleDeletionAssignmentSnapshot(t *testing.T, ctx context.Context, conn *pgxpool.Pool, pluginID uuid.UUID) string {
	t.Helper()
	snapshot, err := testrepo.New(conn).RoleDeletionAssignmentSnapshot(ctx, pluginID)
	require.NoError(t, err)
	return snapshot
}
