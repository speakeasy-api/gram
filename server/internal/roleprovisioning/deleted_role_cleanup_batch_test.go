package roleprovisioning_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func cleanupRemaining(f *fixture, plugins []uuid.UUID, role string) int {
	f.t.Helper()
	remaining := 0
	for _, plugin := range plugins {
		audiences := f.audiences(plugin)
		require.Contains(f.t, audiences, "*")
		require.Contains(f.t, audiences, "role:organization:00000000-0000-0000-0000-000000000001")
		for _, audience := range audiences {
			if audience == role {
				remaining++
			}
		}
	}
	return remaining
}

// Match cleanup's UUID ordering so rich sentinels cover every page, including
// the terminal target. Other targets need only assignments to exercise batching.
func (f *fixture) cleanupBatchTargets(projects []uuid.UUID, role string, count int) []uuid.UUID {
	f.t.Helper()
	targets := make([]uuid.UUID, 0, count)
	for i := range count {
		targets = append(targets, f.cleanupAssignmentTarget(f.org, projects[i%len(projects)], role))
	}
	slices.SortFunc(targets, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for i := 0; i < len(targets); i += 100 {
		plugin := targets[i]
		f.addCleanupContent(f.org, f.plugin(plugin).ProjectID, plugin)
	}
	return targets
}

func TestDeletedRoleCleanupBoundedReplay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// A missing role is also a deletion; no provisioning settings are needed.
	role := "role:organization:" + uuid.NewString()
	projects := []uuid.UUID{f.project, f.addProject(f.org, "second"), f.addProject(f.org, "third")}
	targets := f.cleanupBatchTargets(projects, role, 201)
	before := f.cleanupRetainedSnapshot()
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, f.service)
	request := maintenanceRequest(f.org)
	request.SetRoleUrn(role)
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Equal(t, 101, cleanupRemaining(f, targets, role))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 1)
	require.Equal(t, f.org, messages[0].GetOrganizationId())
	require.Equal(t, role, messages[0].GetRoleUrn())
	require.Empty(t, messages[0].GetAfterRoleUrn())
	// Simulate commit followed by a crash before acknowledgement. Redelivery of
	// the original event drains another page instead of multiplying pending work.
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Equal(t, 1, cleanupRemaining(f, targets, role))
	messages = maintenanceMessages(t, f)
	require.Len(t, messages, 2)
	for _, message := range messages {
		require.NoError(t, h.HandlePage(t.Context(), message))
		require.NoError(t, h.HandlePage(t.Context(), message))
	}
	require.Zero(t, cleanupRemaining(f, targets, role))
	require.Len(t, maintenanceMessages(t, f), 2, "empty replays must not continue")
	require.Equal(t, before, f.cleanupRetainedSnapshot())
}

func TestDeletedRoleCleanupContinuationFailureRollsBackBatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	role := "role:organization:" + uuid.NewString()
	targets := f.cleanupBatchTargets([]uuid.UUID{f.project}, role, 101)
	// Fail after all 100 removals and audits, at the durable handoff boundary.
	f.exec(`CREATE FUNCTION fail_cleanup_continuation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'cleanup continuation failure'; END $$`)
	f.exec(`CREATE TRIGGER fail_cleanup_continuation BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION fail_cleanup_continuation()`)
	_, err := f.service.Reconcile(t.Context(), f.org, role, f.actor)
	require.ErrorContains(t, err, "cleanup continuation failure")
	require.Equal(t, 101, cleanupRemaining(f, targets, role))
	require.Empty(t, maintenanceMessages(t, f))
	f.exec(`DROP TRIGGER fail_cleanup_continuation ON publish_outbox`)
	f.reconcile(role)
	require.Equal(t, 1, cleanupRemaining(f, targets, role))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 1)
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, f.service)
	require.NoError(t, h.HandlePage(t.Context(), messages[0]))
	require.Zero(t, cleanupRemaining(f, targets, role))
}

func TestDeletedRoleCleanupContinuationReactivationIsolation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	role := f.addRole("", "Global batch role")
	targets := f.cleanupBatchTargets([]uuid.UUID{f.project}, role, 101)
	otherOrg := "org_cleanup_batch_other"
	f.addOrganization(otherOrg)
	otherProject := f.addProject(otherOrg, "other")
	other := f.cleanupPlugin(otherOrg, otherProject, role)
	reactivate := f.deleteCleanupRole(role, "workos_deleted_at")
	f.reconcile(role)
	require.Equal(t, 1, cleanupRemaining(f, targets, role))
	require.Contains(t, f.cleanupAudiences(otherOrg, otherProject, other), role)
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 1)
	require.NoError(t, reactivate(t.Context(), f.db))
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, f.service)
	require.NoError(t, h.HandlePage(t.Context(), messages[0]))
	require.NoError(t, h.HandlePage(t.Context(), messages[0]))
	require.Equal(t, 1, cleanupRemaining(f, targets, role), "stale continuation cannot remove live audience")
	require.Contains(t, f.cleanupAudiences(otherOrg, otherProject, other), role)
	require.Len(t, maintenanceMessages(t, f), 1)
}
