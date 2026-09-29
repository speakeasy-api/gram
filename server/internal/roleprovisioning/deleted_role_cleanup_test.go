package roleprovisioning_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	rolerepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func (f *fixture) cleanupPlugin(org string, project uuid.UUID, role string) uuid.UUID {
	f.t.Helper()
	plugin := f.cleanupAssignmentTarget(org, project, role)
	f.addCleanupContent(org, project, plugin)
	return plugin
}

func (f *fixture) cleanupAssignmentTarget(org string, project uuid.UUID, role string) uuid.UUID {
	f.t.Helper()
	q := pluginrepo.New(f.db)
	plugin, err := q.CreatePlugin(f.t.Context(), pluginrepo.CreatePluginParams{OrganizationID: org, ProjectID: project, Name: "Manual plugin", Slug: uuid.NewString(), Description: pgtype.Text{String: "Retained description", Valid: true}})
	require.NoError(f.t, err)
	for _, principal := range []string{role, "*", "role:organization:00000000-0000-0000-0000-000000000001"} {
		_, err = q.AddPluginAssignment(f.t.Context(), pluginrepo.AddPluginAssignmentParams{PluginID: plugin.ID, OrganizationID: org, PrincipalUrn: principal})
		require.NoError(f.t, err)
	}
	return plugin.ID
}

func (f *fixture) addCleanupContent(org string, project, plugin uuid.UUID) {
	f.t.Helper()
	toolset, err := testrepo.New(f.db).CreateToolsetFixture(f.t.Context(), testrepo.CreateToolsetFixtureParams{ID: uuid.New(), OrganizationID: org, ProjectID: project, Name: "Manual content", Slug: uuid.NewString()})
	require.NoError(f.t, err)
	_, err = pluginrepo.New(f.db).AddPluginServer(f.t.Context(), pluginrepo.AddPluginServerParams{PluginID: plugin, ToolsetID: uuid.NullUUID{UUID: toolset, Valid: true}, McpServerID: uuid.NullUUID{}, DisplayName: "Retained content", Policy: "optional", SortOrder: 0})
	require.NoError(f.t, err)
}

// Retention includes names, slugs, all content rows, association markers and
// saved intent, not just row counts. Cleanup may only mutate assignments/audit.
func (f *fixture) cleanupRetainedSnapshot() string {
	f.t.Helper()
	snapshot, err := testrepo.New(f.db).GetRoleLifecycleRetentionSnapshotFixture(f.t.Context())
	require.NoError(f.t, err)
	return snapshot
}

func (f *fixture) cleanupAudiences(org string, project, plugin uuid.UUID) []string {
	f.t.Helper()
	principals, err := rolerepo.New(f.db).ListCleanupPluginPrincipals(f.t.Context(), rolerepo.ListCleanupPluginPrincipalsParams{OrganizationID: org, ProjectID: project, PluginID: plugin})
	require.NoError(f.t, err)
	return principals
}

func (f *fixture) cleanupAuditCount() int64 {
	f.t.Helper()
	count, err := audittest.AuditLogCount(f.t.Context(), f.db)
	require.NoError(f.t, err)
	return count
}

func (f *fixture) cleanupPublicationCount() int {
	f.t.Helper()
	rows, err := testrepo.New(f.db).ListPublishOutboxRows(f.t.Context())
	require.NoError(f.t, err)
	count := 0
	for _, row := range rows {
		if row.Topic == "gram.plugins.v1.PublicationRequested" || row.Topic == "gram.plugins.v1.OrganizationPublicationRequested" {
			count++
		}
	}
	return count
}

// Capture the live source fields before deletion so the returned writer restores
// that same role through the production upsert, including inside a blocking tx.
func (f *fixture) deleteCleanupRole(role, deletion string) func(context.Context, accessrepo.DBTX) error {
	f.t.Helper()
	id := uuid.MustParse(strings.Split(role, ":")[2])
	q := accessrepo.New(f.db)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if strings.HasPrefix(role, "role:global:") {
		roles, err := q.ListGlobalRoles(f.t.Context())
		require.NoError(f.t, err)
		for _, source := range roles {
			if source.ID != id {
				continue
			}
			if deletion == "deleted_at" {
				err = testrepo.New(f.db).SetGlobalRoleLocalDeletionFixture(f.t.Context(), id)
			} else {
				_, err = q.MarkGlobalRoleDeleted(f.t.Context(), accessrepo.MarkGlobalRoleDeletedParams{WorkosSlug: source.WorkosSlug, WorkosDeletedAt: now, WorkosLastEventID: pgtype.Text{}})
			}
			require.NoError(f.t, err)
			return func(ctx context.Context, db accessrepo.DBTX) error {
				return accessrepo.New(db).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{WorkosSlug: source.WorkosSlug, WorkosName: source.WorkosName, WorkosDescription: source.WorkosDescription, WorkosCreatedAt: source.WorkosCreatedAt, WorkosUpdatedAt: source.WorkosUpdatedAt, WorkosLastEventID: pgtype.Text{}})
			}
		}
		f.t.Fatal("missing global cleanup role")
	}
	source, err := q.GetOrganizationRoleByID(f.t.Context(), accessrepo.GetOrganizationRoleByIDParams{OrganizationID: f.org, ID: id})
	require.NoError(f.t, err)
	if deletion == "deleted_at" {
		_, err = q.MarkOrganizationRoleDeletedLocally(f.t.Context(), accessrepo.MarkOrganizationRoleDeletedLocallyParams{OrganizationID: f.org, WorkosSlug: source.WorkosSlug})
	} else {
		_, err = q.MarkOrganizationRoleDeleted(f.t.Context(), accessrepo.MarkOrganizationRoleDeletedParams{OrganizationID: f.org, WorkosSlug: source.WorkosSlug, WorkosDeletedAt: now, WorkosLastEventID: pgtype.Text{}})
	}
	require.NoError(f.t, err)
	return func(ctx context.Context, db accessrepo.DBTX) error {
		_, err := accessrepo.New(db).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{OrganizationID: f.org, WorkosSlug: source.WorkosSlug, WorkosName: source.WorkosName, WorkosDescription: source.WorkosDescription, WorkosCreatedAt: source.WorkosCreatedAt, WorkosUpdatedAt: source.WorkosUpdatedAt, WorkosLastEventID: pgtype.Text{}})
		if err != nil {
			return fmt.Errorf("reactivate cleanup organization role: %w", err)
		}
		return nil
	}
}

func TestDeletedRoleCleanupAllManualPluginsOrganizationUnconfiguredLocalDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "organization", "unconfigured", "deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsOrganizationDisabledWorkOSDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "organization", "disabled", "workos_deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsOrganizationExcludedLocalDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "organization", "excluded", "deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsOrganizationEnabledWorkOSDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "organization", "enabled", "workos_deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsGlobalUnconfiguredWorkOSDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "global", "unconfigured", "workos_deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsGlobalDisabledLocalDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "global", "disabled", "deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsGlobalExcludedWorkOSDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "global", "excluded", "workos_deleted_at")
}

func TestDeletedRoleCleanupAllManualPluginsGlobalEnabledLocalDeletion(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupAllManualPlugins(t, "global", "enabled", "deleted_at")
}

func testDeletedRoleCleanupAllManualPlugins(t *testing.T, scope, state, deletion string) {
	t.Helper()
	f := newFixture(t)
	role := f.role
	if scope == "global" {
		role = f.addRole("", "Global cleanup")
	}
	if state != "unconfigured" {
		if state == "excluded" {
			f.configure(0, true, roleprovisioning.Selection{RoleURN: role, Enabled: false, ProjectID: nil})
		} else {
			f.configure(0, state == "enabled")
		}
	}
	second := f.addProject(f.org, "second")
	manual := f.cleanupPlugin(f.org, f.project, role)
	tombstone := f.cleanupPlugin(f.org, second, role)
	f.deletePlugin(tombstone, second)
	_, err := projectrepo.New(f.db).DeleteProject(t.Context(), second)
	require.NoError(t, err)
	// Even a malformed historical cross-tenant org-role assignment is not ours.
	otherOrg := "org_cleanup_other_fixture"
	f.addOrganization(otherOrg)
	otherProject := f.addProject(otherOrg, "other")
	otherPlugin := f.cleanupPlugin(otherOrg, otherProject, role)
	otherAudience := f.cleanupAudiences(otherOrg, otherProject, otherPlugin)
	reactivate := f.deleteCleanupRole(role, deletion)
	before := f.cleanupRetainedSnapshot()
	auditBefore := f.cleanupAuditCount()
	outboxBefore := f.cleanupPublicationCount()
	result := f.reconcile(role)
	require.True(t, result.Skipped)
	require.Empty(t, result.Publication)
	retained := []string{"*", "role:organization:00000000-0000-0000-0000-000000000001"}
	require.Equal(t, retained, f.audiences(manual))
	require.Equal(t, retained, f.audiences(tombstone))
	require.Equal(t, otherAudience, f.cleanupAudiences(otherOrg, otherProject, otherPlugin))
	require.Equal(t, before, f.cleanupRetainedSnapshot())
	require.Equal(t, auditBefore+2, f.cleanupAuditCount())
	require.Equal(t, outboxBefore, f.cleanupPublicationCount(), "assignment removal must not publish package bytes")
	f.reconcile(role)
	require.Equal(t, auditBefore+2, f.cleanupAuditCount(), "duplicate hints are no-ops")
	// Reactivating before a stale delete hint must preserve the manual audience.
	require.NoError(t, reactivate(t.Context(), f.db))
	f.addAssignment(manual, role)
	f.reconcile(role)
	require.Contains(t, f.audiences(manual), role)
}

func TestDeletedRoleCleanupPriorAssociationsWhileDisabled(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	v := f.configure(0, true)
	old := f.reconcile(f.role).PluginID
	next := f.addProject(f.org, "next")
	v = f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &next})
	current := f.reconcile(f.role).PluginID
	f.addAssignment(old, f.role)
	f.addAssignment(old, "*")
	f.addAssignment(current, "*")
	f.configure(v, false)
	f.deleteCleanupRole(f.role, "workos_deleted_at")
	before := f.cleanupRetainedSnapshot()
	f.reconcile(f.role)
	require.Equal(t, []string{"*"}, f.audiences(old))
	require.Equal(t, []string{"*"}, f.audiences(current))
	require.Equal(t, before, f.cleanupRetainedSnapshot())
}

func TestDeletedRoleCleanupSerializesReactivationOrganizationCleanupFirst(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupSerializesReactivation(t, "organization", "cleanup")
}

func TestDeletedRoleCleanupSerializesReactivationOrganizationReactivationFirst(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupSerializesReactivation(t, "organization", "reactivation")
}

func TestDeletedRoleCleanupSerializesReactivationGlobalCleanupFirst(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupSerializesReactivation(t, "global", "cleanup")
}

func TestDeletedRoleCleanupSerializesReactivationGlobalReactivationFirst(t *testing.T) {
	t.Parallel()
	testDeletedRoleCleanupSerializesReactivation(t, "global", "reactivation")
}

func testDeletedRoleCleanupSerializesReactivation(t *testing.T, scope, first string) {
	t.Helper()
	f := newFixture(t)
	role := f.role
	if scope == "global" {
		role = f.addRole("", "Global reactivation")
	}
	f.configure(0, true)
	provisioned := f.reconcile(role).PluginID
	manual := f.cleanupPlugin(f.org, f.project, role)
	reactivate := f.deleteCleanupRole(role, "workos_deleted_at")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	writer, writerPID := f.beginBlocker(ctx)
	done := make(chan error, 1)
	if first == "reactivation" {
		// Reconcile waits for the live row, then repairs origin rather than
		// applying a stale deletion instruction to unrelated manual plugins.
		_, err := rolerepo.New(f.db).RemoveDeletedRoleAssignment(ctx, rolerepo.RemoveDeletedRoleAssignmentParams{OrganizationID: f.org, ProjectID: f.project, PluginID: provisioned, RoleUrn: role})
		require.NoError(t, err)
		require.NoError(t, reactivate(ctx, writer))
		go func() { _, err := f.service.Reconcile(ctx, f.org, role, f.actor); done <- err }()
		f.waitForBlocked(ctx, writerPID)
		require.NoError(t, writer.Commit(ctx))
		require.NoError(t, <-done)
		require.Contains(t, f.audiences(manual), role)
		require.Contains(t, f.audiences(provisioned), role)
	} else {
		// Pause cleanup after it locked the deleted role, proving tombstones
		// remain locked until the exact audience removal commits.
		blocker, blockerPID := f.beginBlocker(ctx)
		require.NoError(t, admission.LockProject(ctx, blocker, f.project))
		go func() { _, err := f.service.Reconcile(ctx, f.org, role, f.actor); done <- err }()
		cleanupPID := f.waitForBlocked(ctx, blockerPID)
		written := make(chan error, 1)
		go func() { written <- reactivate(ctx, writer) }()
		require.Equal(t, writerPID, f.waitForBlocked(ctx, cleanupPID))
		require.NoError(t, blocker.Commit(ctx))
		require.NoError(t, <-done)
		require.NotContains(t, f.audiences(manual), role)
		require.NoError(t, <-written)
		require.NoError(t, writer.Commit(ctx))
		result := f.reconcile(role)
		require.Equal(t, provisioned, result.PluginID)
		require.Contains(t, f.audiences(provisioned), role)
		require.NotContains(t, f.audiences(manual), role, "reactivation only maintains provisioned origin")
	}
}

func TestDeletedRoleCleanupAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.cleanupPlugin(f.org, f.project, f.role)
	second := f.cleanupPlugin(f.org, f.addProject(f.org, "second"), f.role)
	f.deleteCleanupRole(f.role, "deleted_at")
	beforeFirst, beforeSecond := f.audiences(first), f.audiences(second)
	// Fail on the second audit write, after the first exact removal and audit
	// succeeded. Nothing may survive the enclosing reconciliation rollback.
	f.exec(`CREATE FUNCTION fail_second_cleanup_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF (SELECT count(*) FROM audit_logs) > 0 THEN RAISE EXCEPTION 'cleanup audit failure'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER fail_second_cleanup_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_second_cleanup_audit()`)
	_, err := f.service.Reconcile(t.Context(), f.org, f.role, f.actor)
	require.ErrorContains(t, err, "cleanup audit failure")
	require.Equal(t, beforeFirst, f.audiences(first))
	require.Equal(t, beforeSecond, f.audiences(second))
	require.Zero(t, f.cleanupAuditCount())
	outboxCount, err := testrepo.New(f.db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, outboxCount)
	f.exec(`DROP TRIGGER fail_second_cleanup_audit ON audit_logs`)
	f.reconcile(f.role)
	require.NotContains(t, f.audiences(first), f.role)
	require.NotContains(t, f.audiences(second), f.role)
}

func TestDeletedRoleCleanupMissingRole(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	missing := "role:global:" + uuid.NewString()
	plugin := f.cleanupPlugin(f.org, f.project, missing)
	f.reconcile(missing)
	require.Equal(t, []string{"*", "role:organization:00000000-0000-0000-0000-000000000001"}, f.audiences(plugin))
}
