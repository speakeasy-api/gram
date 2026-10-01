package plugins_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

// Hold an existing audience row so production cleanup pauses after acquiring
// the role lock. The worker and RoleManager still execute their real transactions.
func pauseRoleSetupDeletion(t *testing.T, ctx context.Context, ti *testInstance, setup roleSetupTarget, role string) (pgx.Tx, uuid.UUID) {
	t.Helper()
	fixtures := testrepo.New(ti.conn)
	pluginID, err := fixtures.SeedRoleDeletionPlugin(ctx, testrepo.SeedRoleDeletionPluginParams{OrganizationID: setup.organizationID, Slug: "deletion-barrier"})
	require.NoError(t, err)
	assignmentID, err := fixtures.SeedRoleDeletionAssignment(ctx, testrepo.SeedRoleDeletionAssignmentParams{PluginID: pluginID, OrganizationID: setup.organizationID, PrincipalUrn: role})
	require.NoError(t, err)
	gate := testenv.BeginTx(t, ctx, ti.conn)
	_, err = testrepo.New(gate).LockRoleDeletionAssignment(ctx, assignmentID)
	require.NoError(t, err)
	return gate, assignmentID
}

func startRoleSetupDeletion(t *testing.T, ctx context.Context, ti *testInstance, setup roleSetupTarget, role string) <-chan error {
	t.Helper()
	provider := workos.NewStubClient()
	const workosOrgID = "org_role_setup_deletion"
	_, err := provider.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{Name: "Engineering", Slug: "Engineering", Description: ""})
	require.NoError(t, err)
	manager := access.NewRoleManager(testenv.NewLogger(t), ti.conn, provider, audit.NewLogger())
	result := make(chan error, 1)
	go func() {
		_, err := manager.DeleteRole(ctx, setup.organizationID, workosOrgID, strings.TrimPrefix(role, "role:organization:"), access.RoleAuditActor{
			Principal: urn.NewSystemPrincipal("role-deletion-test"), DisplayName: nil,
		})
		result <- err
	}()
	return result
}

func assertRoleSetupDeletionState(t *testing.T, ctx context.Context, ti *testInstance, setup roleSetupTarget, assignmentID uuid.UUID, deleted bool) {
	t.Helper()
	fixtures := testrepo.New(ti.conn)
	state, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{OrganizationID: setup.organizationID, WorkosSlug: "Engineering"})
	require.NoError(t, err)
	require.Equal(t, deleted, state.Deleted)
	count, err := fixtures.CountRoleDeletionAssignments(ctx, []uuid.UUID{assignmentID})
	require.NoError(t, err)
	if deleted {
		require.Zero(t, count, "role tombstone and audience removal must commit together")
	} else {
		require.EqualValues(t, 1, count, "uncommitted deletion must preserve the visible audience")
	}
}

func roleDeletionPreservedPlugin(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()
	description := "Administrator content must survive role deletion"
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Preserved", Description: &description})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "preserved-toolset")
	toolsetID := toolset.ID.String()
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &toolsetID, Policy: "required"})
	require.NoError(t, err)
	otherRole := createTestRolePrincipal(t, ctx, ti, "unrelated")
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{"*", otherRole}})
	require.NoError(t, err)
	return uuid.MustParse(plugin.ID)
}

func TestProcessRoleDistributionSetup_DeleteWaitsForSetup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	preservedID := roleDeletionPreservedPlugin(t, ctx, ti)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	setup := roleSetupIdentity(t, ctx, ti, role)
	fixtures := testrepo.New(ti.conn)
	preserved, err := fixtures.RoleDeletionAssignmentSnapshot(ctx, preservedID)
	require.NoError(t, err)
	cleanupGate, assignmentID := pauseRoleSetupDeletion(t, ctx, ti, setup, role)
	gate, gatePID := pauseRoleSetupPluginWrite(t, ctx, ti, "")
	worker := startRoleSetupRun(ctx, t, ti, role)
	setupPID := waitRoleSetupBlocked(t, ctx, ti, gatePID)
	deletion := startRoleSetupDeletion(t, ctx, ti, setup, role)
	// Setup already holds FOR SHARE on the role before reaching its plugin INSERT.
	deletionPID := waitRoleSetupBlocked(t, ctx, ti, setupPID)
	require.NoError(t, gate.Commit(ctx))
	require.Equal(t, 1, readRoleSetupRun(t, ctx, worker))
	require.Equal(t, deletionPID, waitRoleSetupBlocked(t, ctx, ti, int32(cleanupGate.Conn().PgConn().PID())))
	assertRoleSetupDeletionState(t, ctx, ti, setup, assignmentID, false)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	pluginID, err := fixtures.PipelineEngineeringPlugin(ctx, *ac.ProjectID)
	require.NoError(t, err)
	principal, err := fixtures.PipelinePluginPrincipal(ctx, pluginID)
	require.NoError(t, err)
	require.Equal(t, role, principal)
	before, err := fixtures.RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, cleanupGate.Commit(ctx))
	select {
	case err := <-deletion:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertRoleSetupDeletionState(t, ctx, ti, setup, assignmentID, true)
	n, err := processRoleSetup(ctx, ti, setup, plugins.PublicationRequests{})
	require.NoError(t, err)
	require.Zero(t, n, "replaying successful setup must not restore the deleted audience")
	count, err := fixtures.PipelineCountPluginAssignments(ctx, pluginID)
	require.NoError(t, err)
	require.Zero(t, count)
	after, err := fixtures.RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	remaining, err := fixtures.RoleDeletionAssignmentSnapshot(ctx, preservedID)
	require.NoError(t, err)
	require.Equal(t, preserved, remaining)
}

func TestProcessRoleDistributionSetup_DeleteFirstSkipsSetup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	roleDeletionPreservedPlugin(t, ctx, ti)
	role := roleSetupFixture(t, ctx, ti, "Engineering")
	setup := roleSetupIdentity(t, ctx, ti, role)
	fixtures := testrepo.New(ti.conn)
	audiences, err := fixtures.RoleDeletionAllAssignmentsSnapshot(ctx)
	require.NoError(t, err)
	cleanupGate, assignmentID := pauseRoleSetupDeletion(t, ctx, ti, setup, role)
	before, err := fixtures.RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	deletion := startRoleSetupDeletion(t, ctx, ti, setup, role)
	deletionPID := waitRoleSetupBlocked(t, ctx, ti, int32(cleanupGate.Conn().PgConn().PID()))
	assertRoleSetupDeletionState(t, ctx, ti, setup, assignmentID, false)
	worker := startRoleSetupRun(ctx, t, ti, role)
	waitRoleSetupBlocked(t, ctx, ti, deletionPID)
	require.NoError(t, cleanupGate.Commit(ctx))
	select {
	case err := <-deletion:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertRoleSetupDeletionState(t, ctx, ti, setup, assignmentID, true)
	require.Zero(t, readRoleSetupRun(t, ctx, worker))
	n, err := processRoleSetup(ctx, ti, setup, plugins.PublicationRequests{})
	require.NoError(t, err)
	require.Zero(t, n)
	after, err := fixtures.RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "obsolete setup must not create or alter plugins")
	remaining, err := fixtures.RoleDeletionAllAssignmentsSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, audiences, remaining)
}
