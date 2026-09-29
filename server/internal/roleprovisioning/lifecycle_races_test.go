package roleprovisioning_test

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	rolerepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

// Replayed lifecycle hints carry identity, not an old-project before-image.
// After C is saved they must clean actual applied B, never historical A.
func TestLifecycleRapidRemapRetriesUseActualCurrent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b, c := f.addProject(f.org, "second"), f.addProject(f.org, "third")
	other := f.addRole(f.org, "Retained audience")
	v := f.configure(0, true)
	aID := f.reconcile(f.role).PluginID
	v = f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &b})
	bID := f.reconcile(f.role).PluginID
	// An administrator restores the origin on historical A. It is no longer
	// current and must not be cleaned again by a duplicate/out-of-order hint.
	f.addAssignment(aID, f.role)
	f.addAssignment(bID, other)
	f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &c})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, pid := f.beginBlocker(ctx)
	require.NoError(t, admission.LockProject(ctx, blocker, c))
	first := startLifecycleReconcile(ctx, f)
	reconcilerPID := f.waitForBlocked(ctx, pid)
	replay := startLifecycleReconcile(ctx, f)
	f.waitForBlocked(ctx, reconcilerPID)
	require.NoError(t, blocker.Commit(ctx))
	one, two := <-first, <-replay
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Empty(t, one.result.Pending)
	require.Empty(t, two.result.Pending)
	require.NotEqual(t, uuid.Nil, one.result.PluginID)
	require.Equal(t, one.result.PluginID, two.result.PluginID)
	require.Equal(t, []string{f.role}, f.audiences(aID))
	require.Equal(t, []string{other}, f.audiences(bID))
	require.Equal(t, []string{f.role}, f.audiences(one.result.PluginID))
	require.Len(t, f.associations(), 3)
	require.True(t, f.association(one.result.PluginID).IsCurrent)
	require.Equal(t, c, f.association(one.result.PluginID).ProjectID.UUID)
}

// Commit mutations after reconciliation starts but before it acquires the
// destination locks. Sequential ABA does not exercise these before-images.
func TestLifecycleRemapWaitsForDestinationMutationAudience(t *testing.T) {
	t.Parallel()
	testLifecycleRemapWaitsForDestinationMutation(t, "audience")
}

func TestLifecycleRemapWaitsForDestinationMutationName(t *testing.T) {
	t.Parallel()
	testLifecycleRemapWaitsForDestinationMutation(t, "name")
}

func TestLifecycleRemapWaitsForDestinationMutationDelete(t *testing.T) {
	t.Parallel()
	testLifecycleRemapWaitsForDestinationMutation(t, "delete")
}

func TestLifecycleRemapWaitsForDestinationMutationGuardFailure(t *testing.T) {
	t.Parallel()
	testLifecycleRemapWaitsForDestinationMutation(t, "guard failure")
}

func testLifecycleRemapWaitsForDestinationMutation(t *testing.T, mutation string) {
	t.Helper()
	f := newFixture(t)
	b := f.addProject(f.org, "second")
	other := f.addRole(f.org, "Retained audience")
	v := f.configure(0, true)
	aID := f.reconcile(f.role).PluginID
	if mutation == "guard failure" {
		f.attachDirectRemote(aID, f.project)
	}
	v = f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &b})
	bID := f.reconcile(f.role).PluginID
	f.addAssignment(bID, other)
	// Same origin, unrelated project: remap must not mutate this manual plugin.
	isolated := f.addProject(f.org, "isolated")
	manualPlugin, err := pluginrepo.New(f.db).CreatePlugin(t.Context(), pluginrepo.CreatePluginParams{OrganizationID: f.org, ProjectID: isolated, Name: "Manual", Slug: "manual", Description: pgtype.Text{}})
	require.NoError(t, err)
	manual := manualPlugin.ID
	f.addAssignment(manual, f.role)
	f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &f.project})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	admin, pid := f.beginBlocker(ctx)
	require.NoError(t, admission.LockProject(ctx, admin, f.project))
	locked, err := assignments.Lock(ctx, admin, f.org, f.project, aID)
	require.NoError(t, err)
	done := startLifecycleReconcile(ctx, f)
	f.waitForBlocked(ctx, pid)
	switch mutation {
	case "audience", "guard failure":
		// Only the independent admin command uses LegacyGuard; reconciliation
		// still uses the real fail-closed admission guard.
		_, err = assignments.Replace(ctx, admin, audit.NewLogger(), locked, assignments.Input{OrganizationID: f.org, ProjectID: f.project, PluginID: aID, PrincipalURNs: []string{other}, Actor: f.actor.Principal, ActorDisplayName: nil, ActorSlug: nil}, assignments.Dependencies{Guard: assignments.LegacyGuard, BeforeReplace: nil})
	case "name":
		_, err = pluginrepo.New(admin).UpdatePlugin(ctx, pluginrepo.UpdatePluginParams{ID: aID, OrganizationID: f.org, ProjectID: f.project, Name: "Manual override", Slug: locked.Slug, Description: pgtype.Text{}})
		require.NoError(t, err)
		err = rolerepo.New(admin).SetAutomaticName(ctx, rolerepo.SetAutomaticNameParams{Name: pgtype.Text{}, PluginID: aID, ProjectID: f.project, OrganizationID: f.org})
	case "delete":
		err = pluginrepo.New(admin).DeletePlugin(ctx, pluginrepo.DeletePluginParams{ID: aID, OrganizationID: f.org, ProjectID: f.project})
	}
	require.NoError(t, err)
	require.NoError(t, admin.Commit(ctx))
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, []string{f.role}, f.audiences(manual))
	require.Equal(t, isolated, f.plugin(manual).ProjectID)
	require.Equal(t, "Manual", f.plugin(manual).Name)
	require.False(t, f.plugin(manual).DeletedAt.Valid)
	if mutation == "guard failure" {
		require.Equal(t, "admission_unavailable", got.result.Pending)
		require.ElementsMatch(t, []string{f.role, other}, f.audiences(bID))
		require.Equal(t, []string{other}, f.audiences(aID))
		require.True(t, f.association(bID).IsCurrent)
		require.False(t, f.association(aID).IsCurrent)
		servers, err := pluginrepo.New(f.db).ListPluginServers(t.Context(), aID)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.Equal(t, f.project, f.roleSetting().ProjectID.UUID)
		require.Equal(t, "admission_unavailable", f.roleSetting().LastErrorCode.String)
		return
	}
	require.Empty(t, got.result.Pending)
	require.Equal(t, []string{other}, f.audiences(bID))
	require.True(t, f.association(got.result.PluginID).IsCurrent)
	switch mutation {
	case "audience":
		require.Equal(t, aID, got.result.PluginID)
		require.ElementsMatch(t, []string{f.role, other}, f.audiences(aID))
	case "name":
		require.Equal(t, aID, got.result.PluginID)
		require.Equal(t, "Manual override", f.plugin(aID).Name)
		require.False(t, f.association(aID).LastAutomaticName.Valid)
	case "delete":
		require.NotEqual(t, aID, got.result.PluginID)
		require.NotEqual(t, uuid.Nil, got.result.PluginID)
		f.assertEmpty(got.result.PluginID)
		require.Equal(t, []string{f.role}, f.audiences(got.result.PluginID))
		require.True(t, f.association(aID).RetiredAt.Valid)
		require.False(t, f.association(aID).IsCurrent)
		require.True(t, f.plugin(aID).DeletedAt.Valid)
	}
}

// The inverse of ConfigureWaitsForRunningReconciliation: a retry already
// running behind a disabling writer must observe disabled intent, not repair
// drift or apply the previously saved remap.
func TestLifecycleRemapRetryWaitsForDisableOrganization(t *testing.T) {
	t.Parallel()
	testLifecycleRemapRetryWaitsForDisable(t, "organization")
}

func TestLifecycleRemapRetryWaitsForDisableRole(t *testing.T) {
	t.Parallel()
	testLifecycleRemapRetryWaitsForDisable(t, "role")
}

func testLifecycleRemapRetryWaitsForDisable(t *testing.T, scope string) {
	t.Helper()
	f := newFixture(t)
	destination := f.addProject(f.org, "destination")
	v := f.configure(0, true)
	old := f.reconcile(f.role).PluginID
	f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &destination})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	writer, pid := f.beginBlocker(ctx)
	_, err := rolerepo.New(writer).LockOrganization(ctx, f.org)
	require.NoError(t, err)
	done := startLifecycleReconcile(ctx, f)
	f.waitForBlocked(ctx, pid)
	if scope == "organization" {
		settings, readErr := rolerepo.New(writer).GetSettings(ctx, f.org)
		require.NoError(t, readErr)
		_, err = rolerepo.New(writer).SaveSettings(ctx, rolerepo.SaveSettingsParams{OrganizationID: f.org, Enabled: pgtype.Bool{Bool: false, Valid: true}, ProjectID: settings.ProjectID, Version: pgtype.Int8{Int64: settings.Version + 1, Valid: true}})
	} else {
		err = rolerepo.New(writer).SaveRoleSetting(ctx, rolerepo.SaveRoleSettingParams{OrganizationID: pgtype.Text{String: f.org, Valid: true}, RoleUrn: f.role, Enabled: false, ProjectID: uuid.NullUUID{UUID: destination, Valid: true}})
	}
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	got := <-done
	require.NoError(t, got.err)
	require.True(t, got.result.Skipped)
	require.Equal(t, []string{f.role}, f.audiences(old))
	require.True(t, f.association(old).IsCurrent)
	require.Empty(t, f.projectPlugins(destination))
	require.Equal(t, destination, f.roleSetting().ProjectID.UUID)
}

type lifecycleReconcileResponse struct {
	result roleprovisioning.Result
	err    error
}

func startLifecycleReconcile(ctx context.Context, f *fixture) <-chan lifecycleReconcileResponse {
	done := make(chan lifecycleReconcileResponse, 1)
	go func() {
		result, err := f.service.Reconcile(ctx, f.org, f.role, f.actor)
		done <- lifecycleReconcileResponse{result: result, err: err}
	}()
	return done
}
