package activities_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSyncedOrganizationRoleDeletionPluginAssignments(t *testing.T) {
	t.Parallel()
	testSyncedRoleDeletionPluginAssignments(t, "organization")
}

func TestSyncedGlobalRoleDeletionPluginAssignments(t *testing.T) {
	t.Parallel()
	testSyncedRoleDeletionPluginAssignments(t, "global")
}

func testSyncedRoleDeletionPluginAssignments(t *testing.T, scope string) {
	t.Helper()
	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "synced_role_plugin_delete_"+scope)
	logger := testenv.NewLogger(t)
	q := testrepo.New(conn)
	const orgID = "synced_plugin_role_org"
	const otherOrgID = "synced_plugin_role_other"
	const workosOrgID = "org_synced_plugin_role"
	seedWorkOSOrganization(t, ctx, conn, orgID, workosOrgID)
	seedWorkOSOrganization(t, ctx, conn, otherOrgID, "org_synced_plugin_other")
	var roleID uuid.UUID
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	if scope == "organization" {
		roleID = seedOrganizationRole(t, ctx, conn, orgID, "plugin-role").ID
	} else {
		id, err := q.SeedRoleDeletionGlobalRole(ctx, conv.ToPGTimestamptz(now))
		roleID = id
		require.NoError(t, err)
	}
	principal := "role:" + scope + ":" + roleID.String()
	// The same UUID with the other scope must never be treated as the deleted role.
	otherScope := "global"
	if scope == "global" {
		otherScope = "organization"
	}
	audiences := []string{"user:retained", "group:retained", "role:" + scope + ":" + uuid.NewString(), "role:" + otherScope + ":" + roleID.String()}
	affectedPlugins := make(map[uuid.UUID]string)
	var removedIDs []uuid.UUID
	var retainedIDs []uuid.UUID
	for _, tenant := range []string{orgID, otherOrgID} {
		pluginID, err := q.SeedRoleDeletionPlugin(ctx, testrepo.SeedRoleDeletionPluginParams{OrganizationID: tenant, Slug: uuid.NewString()})
		require.NoError(t, err)
		for _, audience := range append([]string{principal}, audiences...) {
			id, err := q.SeedRoleDeletionAssignment(ctx, testrepo.SeedRoleDeletionAssignmentParams{PluginID: pluginID, OrganizationID: tenant, PrincipalUrn: audience})
			require.NoError(t, err)
			if audience == principal && (scope == "global" || tenant == orgID) {
				removedIDs = append(removedIDs, id)
				affectedPlugins[pluginID] = tenant
			} else {
				retainedIDs = append(retainedIDs, id)
			}
		}
	}
	// Corrupt cross-tenant rows must not be swept: both assignment and plugin tenant must match.
	mismatchedPlugin, err := q.SeedRoleDeletionPlugin(ctx, testrepo.SeedRoleDeletionPluginParams{OrganizationID: otherOrgID, Slug: uuid.NewString()})
	require.NoError(t, err)
	mismatchedID, err := q.SeedRoleDeletionAssignment(ctx, testrepo.SeedRoleDeletionAssignmentParams{PluginID: mismatchedPlugin, OrganizationID: orgID, PrincipalUrn: principal})
	require.NoError(t, err)
	retainedIDs = append(retainedIDs, mismatchedID)

	contentBefore, err := q.RoleDeletionContentSnapshot(ctx)
	require.NoError(t, err)
	assignmentsBefore, err := q.RoleDeletionAllAssignmentsSnapshot(ctx)
	require.NoError(t, err)
	eventType := "organization_role.deleted"
	if scope == "global" {
		eventType = "role.deleted"
	}
	payload, err := json.Marshal(map[string]any{"organization_id": workosOrgID, "slug": "plugin-role", "updated_at": now, "deleted_at": now})
	require.NoError(t, err)
	deliver := func(eventID string) {
		t.Helper()
		stub := newWorkOSClientWithEvents([][]events.Event{{{ID: eventID, Event: eventType, CreatedAt: now, Data: payload}}})
		if scope == "organization" {
			result, err := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub, cache.NoopCache, nil).Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
			require.NoError(t, err)
			require.Equal(t, eventID, result.LastEventID)
		} else {
			result, err := activities.NewProcessWorkOSGlobalRoleEvents(logger, conn, stub).Do(ctx, activities.ProcessWorkOSGlobalRoleEventsParams{})
			require.NoError(t, err)
			require.Equal(t, eventID, result.LastEventID)
		}
	}
	deliver("event_00OLD")
	assignmentsAfterStale, err := q.RoleDeletionAllAssignmentsSnapshot(ctx)
	require.NoError(t, err)
	require.JSONEq(t, assignmentsBefore, assignmentsAfterStale)
	staleAuditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Zero(t, staleAuditCount)
	for range 2 {
		deliver("event_01DELETE")
		removedCount, err := q.CountRoleDeletionAssignments(ctx, removedIDs)
		require.NoError(t, err)
		require.Zero(t, removedCount)
		retainedCount, err := q.CountRoleDeletionAssignments(ctx, retainedIDs)
		require.NoError(t, err)
		require.EqualValues(t, len(retainedIDs), retainedCount)
		state, err := q.GetRoleDeletionState(ctx, testrepo.GetRoleDeletionStateParams{ID: roleID, Scope: scope})
		require.NoError(t, err)
		require.True(t, state.WorkosDeleted)
		require.True(t, state.WorkosLastEventID.Valid)
		require.Equal(t, "event_01DELETE", state.WorkosLastEventID.String)
		contentAfter, err := q.RoleDeletionContentSnapshot(ctx)
		require.NoError(t, err)
		require.JSONEq(t, contentBefore, contentAfter)
		auditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionPluginAssignmentsSet)
		require.NoError(t, err)
		require.EqualValues(t, len(removedIDs), auditCount)
		for pluginID, tenant := range affectedPlugins {
			records, err := q.ListRoleDeletionPluginAudits(ctx, testrepo.ListRoleDeletionPluginAuditsParams{PluginID: pluginID, Action: string(audit.ActionPluginAssignmentsSet)})
			require.NoError(t, err)
			require.Len(t, records, 1)
			record := records[0]
			require.Equal(t, tenant, record.OrganizationID)
			require.True(t, record.ProjectID.Valid)
			require.Equal(t, record.PluginProjectID, record.ProjectID.UUID)
			require.Equal(t, "workos-role-sync", record.ActorID)
			require.Equal(t, "system", record.ActorType)
			require.Equal(t, pluginID.String(), record.SubjectID)
			require.Equal(t, "plugin", record.SubjectType)
			metadata, err := audittest.DecodeAuditData(record.Metadata)
			require.NoError(t, err)
			require.Contains(t, metadata, "principal_urns")
			require.ElementsMatch(t, audiences, metadata["principal_urns"])
		}
	}
}
