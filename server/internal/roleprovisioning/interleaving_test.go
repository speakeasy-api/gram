package roleprovisioning_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

func TestFirstEnableUntouchedSettingsSelectsProject(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"seeded", "nullable legacy"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if kind == "seeded" {
				f.exec(`INSERT INTO organization_role_provisioning_settings (organization_id,enabled,project_id,version) VALUES ($1,false,NULL,0)`, f.org)
			} else {
				f.exec(`INSERT INTO organization_role_provisioning_settings (organization_id,enabled,project_id,version) VALUES ($1,NULL,NULL,NULL)`, f.org)
			}
			f.configure(0, true)
			saved, err := f.service.Settings(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, int64(1), saved.Version)
			require.Equal(t, f.project, saved.ProjectID.UUID)
			require.Len(t, saved.Roles, 1)
			require.Equal(t, f.project, saved.Roles[0].ProjectID.UUID)
			created := f.reconcile(f.role)
			require.Empty(t, created.Pending)
			require.NotEqual(t, uuid.Nil, created.PluginID)
		})
	}
}

func TestExplicitPendingDestinationSurvivesFirstEnable(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pending := uuid.Nil
	v, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, Enabled: false, ProjectID: &pending})
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
	// Unlike an untouched default-off row, this is an explicitly saved choice.
	f.configure(v, true)
	saved, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.False(t, saved.ProjectID.Valid)
	require.False(t, saved.Roles[0].ProjectID.Valid)
	require.Equal(t, "choose_project", f.reconcile(f.role).Pending)
}

// waitForBlocked observes a real database wait edge, not a goroutine start or a
// timing assumption. Returning the waiting PID allows a second lock edge to be
// established before releasing the first transaction.
func (f *fixture) waitForBlocked(ctx context.Context, blocker int) int {
	f.t.Helper()
	var waiting int
	require.Eventually(f.t, func() bool {
		err := f.db.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) ORDER BY pid LIMIT 1`, blocker).Scan(&waiting) //nolint:glint // notestingrawsql: observe a deterministic database wait edge
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	return waiting
}

func (f *fixture) beginBlocker(ctx context.Context) (pgx.Tx, int) {
	f.t.Helper()
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: controlled transaction for lock interleaving
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var pid int
	require.NoError(f.t, tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)) //nolint:glint // notestingrawsql: identify the isolated blocking transaction
	return tx, pid
}

func TestSettingsReadsOneCommittedSnapshot(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, pid := f.beginBlocker(ctx)
	_, err := blocker.Exec(ctx, `SELECT id FROM organization_metadata WHERE id=$1 FOR NO KEY UPDATE`, f.org) //nolint:glint // notestingrawsql: reproduce the configuration writer's organization lock
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, `LOCK TABLE role_provisioning_settings IN ACCESS EXCLUSIVE MODE`) //nolint:glint // notestingrawsql: pause the reader strictly between organization and role reads
	require.NoError(t, err)
	type response struct {
		settings roleprovisioning.Settings
		err      error
	}
	done := make(chan response, 1)
	go func() { settings, err := f.service.Settings(ctx, f.org); done <- response{settings, err} }()
	f.waitForBlocked(ctx, pid)
	// The reader has already read version 1. Commit a complete new configuration
	// before its role query resumes; it must still return the version-1 roles.
	_, err = blocker.Exec(ctx, `UPDATE organization_role_provisioning_settings SET enabled=false,version=2 WHERE organization_id=$1`, f.org) //nolint:glint // notestingrawsql: commit the competing version and selections atomically
	require.NoError(t, err)
	_, err = blocker.Exec(ctx, `UPDATE role_provisioning_settings SET enabled=false WHERE organization_id=$1`, f.org) //nolint:glint // notestingrawsql: commit the competing version and selections atomically
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))
	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, int64(1), result.settings.Version)
	require.True(t, result.settings.Enabled)
	require.Len(t, result.settings.Roles, 1)
	require.True(t, result.settings.Roles[0].Enabled)
	latest, err := f.service.Settings(ctx, f.org)
	require.NoError(t, err)
	require.Equal(t, int64(2), latest.Version)
	require.False(t, latest.Enabled)
	require.False(t, latest.Roles[0].Enabled)
}

func TestAudienceRepairWaitsForAdminReplacement(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := f.addRole(f.org, "Other audience")
	f.configure(0, true)
	pluginID := f.reconcile(f.role).PluginID
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	admin, pid := f.beginBlocker(ctx)
	require.NoError(t, admission.LockProject(ctx, admin, f.project))
	locked, err := assignments.Lock(ctx, admin, f.org, f.project, pluginID)
	require.NoError(t, err)
	type response struct {
		result roleprovisioning.Result
		err    error
	}
	done := make(chan response, 1)
	go func() { result, err := f.service.Reconcile(ctx, f.org, f.role, f.actor); done <- response{result, err} }()
	f.waitForBlocked(ctx, pid)
	// The reconciler is already running. Admin replaces the audience while owning
	// the same admission/plugin locks; repair must subsequently merge its origin.
	_, err = assignments.Replace(ctx, admin, audit.NewLogger(), locked, assignments.Input{OrganizationID: f.org, ProjectID: f.project, PluginID: pluginID, PrincipalURNs: []string{other}, Actor: f.actor.Principal}, assignments.Dependencies{Guard: assignments.LegacyGuard})
	require.NoError(t, err)
	require.NoError(t, admin.Commit(ctx))
	repaired := <-done
	require.NoError(t, repaired.err)
	require.Equal(t, pluginID, repaired.result.PluginID)
	require.ElementsMatch(t, []string{f.role, other}, f.audiences(pluginID))
}

func TestConfigureWaitsForRunningReconciliation(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"disable", "remap"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.configure(0, true)
			destination := f.addProject(f.org, "destination")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			blocker, pid := f.beginBlocker(ctx)
			require.NoError(t, admission.LockProject(ctx, blocker, f.project))
			type response struct {
				result roleprovisioning.Result
				err    error
			}
			reconciled := make(chan response, 1)
			go func() {
				result, err := f.service.Reconcile(ctx, f.org, f.role, f.actor)
				reconciled <- response{result, err}
			}()
			// Reconcile owns the organization lock and waits on the admission lock.
			reconcilerPID := f.waitForBlocked(ctx, pid)
			input := roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: 1, Enabled: operation != "disable"}
			if operation == "remap" {
				input.Roles = []roleprovisioning.Selection{{RoleURN: f.role, Enabled: true, ProjectID: &destination}}
			}
			configured := make(chan error, 1)
			go func() { _, err := f.service.Configure(ctx, input); configured <- err }()
			f.waitForBlocked(ctx, reconcilerPID)
			require.NoError(t, blocker.Commit(ctx))
			first := <-reconciled
			require.NoError(t, first.err)
			require.NotEqual(t, uuid.Nil, first.result.PluginID)
			require.NoError(t, <-configured)
			if operation == "disable" {
				f.exec(`DELETE FROM plugin_assignments WHERE plugin_id=$1`, first.result.PluginID)
				require.True(t, f.reconcile(f.role).Skipped)
				require.Empty(t, f.audiences(first.result.PluginID))
				require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins`))
			} else {
				next := f.reconcile(f.role)
				require.NotEqual(t, first.result.PluginID, next.PluginID)
				require.Equal(t, []string{f.role}, f.audiences(next.PluginID))
				require.Empty(t, f.audiences(first.result.PluginID))
				require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, next.PluginID))
			}
		})
	}
}
