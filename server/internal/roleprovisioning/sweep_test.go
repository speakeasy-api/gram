package roleprovisioning_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"time"
)

// Drop every lifecycle hint to prove that the scheduled tick alone repairs drift.
func runSafetyNetSweep(t *testing.T, f *fixture) {
	t.Helper()
	require.NoError(t, testrepo.New(f.db).DeleteRoleLifecycleOutboxFixture(t.Context(), "gram.plugins.v1.RoleProvisioningRequested"))
	require.NoError(t, roleprovisioning.RequestSweep(t.Context(), f.db))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 1)
	require.True(t, messages[0].GetGlobalSweep())
	consumer := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, f.service)
	for index := 0; index < len(messages); index++ {
		require.Less(t, index, 50, "fixture sweep must finish bounded continuations")
		require.NoError(t, consumer.HandlePage(t.Context(), messages[index]))
		messages = maintenanceMessages(t, f)
	}
}

func TestSweepRecoversMissingSelectedRoleWithoutLifecycleHint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	role := f.addRole(f.org, "Missed creation")
	runSafetyNetSweep(t, f)
	require.Len(t, f.projectPlugins(f.project), 2)
	var rolePlugins int
	for _, plugin := range f.projectPlugins(f.project) {
		if slices.Contains(f.audiences(plugin.ID), role) {
			rolePlugins++
		}
	}
	require.Equal(t, 1, rolePlugins)
	for _, plugin := range f.projectPlugins(f.project) {
		f.assertEmpty(plugin.ID)
	}
	runSafetyNetSweep(t, f)
	require.Len(t, f.projectPlugins(f.project), 2)
}

func TestSweepRepairsNameAndOriginWithoutLifecycleHint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	plugin := f.reconcile(f.role).PluginID
	role, err := accessrepo.New(f.db).GetOrganizationRoleByID(t.Context(), accessrepo.GetOrganizationRoleByIDParams{OrganizationID: f.org, ID: uuid.MustParse(f.role[len("role:organization:"):])})
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).UpsertOrganizationRole(t.Context(), accessrepo.UpsertOrganizationRoleParams{OrganizationID: f.org, WorkosSlug: role.WorkosSlug, WorkosName: "Recovered name", WorkosDescription: role.WorkosDescription, WorkosCreatedAt: role.WorkosCreatedAt, WorkosUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, WorkosLastEventID: pgtype.Text{}})
	require.NoError(t, err)
	f.removeAssignments(plugin, f.project)
	f.addAssignment(plugin, "*")
	runSafetyNetSweep(t, f)
	require.Equal(t, "Recovered name", f.plugin(plugin).Name)
	require.ElementsMatch(t, []string{f.role, "*"}, f.audiences(plugin))
}

func TestSweepCleansDisabledStaleRoleWithoutLifecycleHint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, false)
	role := f.addRole("", "Missed global deletion")
	manual := f.cleanupPlugin(f.org, f.project, role)
	retained := f.cleanupRetainedSnapshot()
	roles, err := accessrepo.New(f.db).ListGlobalRoles(t.Context())
	require.NoError(t, err)
	for _, candidate := range roles {
		if candidate.ID == uuid.MustParse(role[len("role:global:"):]) {
			_, err = accessrepo.New(f.db).MarkGlobalRoleDeleted(t.Context(), accessrepo.MarkGlobalRoleDeletedParams{WorkosSlug: candidate.WorkosSlug, WorkosDeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, WorkosLastEventID: pgtype.Text{}})
			require.NoError(t, err)
		}
	}
	runSafetyNetSweep(t, f)
	require.NotContains(t, f.audiences(manual), role)
	require.Contains(t, f.audiences(manual), "*")
	require.Equal(t, retained, f.cleanupRetainedSnapshot())
	require.Len(t, f.projectPlugins(f.project), 1)
}

func TestSweepHintFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// Fault injection: PostgreSQL trigger forces enqueue failure inside the sweep transaction.
	f.exec(`CREATE FUNCTION reject_sweep_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected sweep failure'; END $$; CREATE TRIGGER reject_sweep_hint BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_sweep_hint()`)
	require.Error(t, roleprovisioning.RequestSweep(t.Context(), f.db))
	count, err := testrepo.New(f.db).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}
