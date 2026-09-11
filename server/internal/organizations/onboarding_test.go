package organizations_test

import (
	"encoding/json"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestOnboardingPreservesLegacySelectionUntilExplicitSave(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := orgrepo.New(ti.conn)
	for _, row := range []orgrepo.UpsertOrganizationSetupTaskParams{
		{OrganizationID: ac.ActiveOrganizationID, TaskKey: "identity-provider", Status: "in_progress", AssigneeUserID: conv.ToPGText(ac.UserID)},
		{OrganizationID: ac.ActiveOrganizationID, TaskKey: "anthropic-observability", Status: "todo", HiddenAt: conv.ToPGTimestamptz(time.Now())},
		{OrganizationID: ac.ActiveOrganizationID, TaskKey: "platform-mcp", Status: "awaiting_support"},
	} {
		_, err := queries.UpsertOrganizationSetupTask(ctx, row)
		require.NoError(t, err)
	}
	before, err := queries.ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	config, err := organizations.LoadOnboardingConfiguration(ctx, ti.conn, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Nil(t, config.Preset)
	var visible []string
	for _, task := range config.Tasks {
		if !task.Hidden {
			visible = append(visible, task.Key)
		}
	}
	require.ElementsMatch(t, []string{"identity-provider", "instrument-agents", "additional-agent-config", "platform-mcp"}, visible)
	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Tasks, len(visible))
	for _, key := range visible {
		require.NotNil(t, setupTask(listed.Tasks, key))
	}
	after, err := queries.ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, preset := range config.Presets {
		_, err := organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, preset.VisibleTaskKeys, &preset.Key, urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
		require.NoError(t, err)
		listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
		require.NoError(t, err)
		var keys []string
		for _, task := range listed.Tasks {
			keys = append(keys, task.Key)
		}
		require.ElementsMatch(t, preset.VisibleTaskKeys, keys)
		require.Nil(t, setupTask(listed.Tasks, "identity-provider"), "presets must not duplicate the split identity tasks")
	}
}

func TestOnboardingPreservesRawProgressAndAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := orgrepo.New(ti.conn)
	stored, err := queries.UpsertOrganizationSetupTask(ctx, orgrepo.UpsertOrganizationSetupTaskParams{
		OrganizationID: ac.ActiveOrganizationID, TaskKey: "create-marketplace", Status: "awaiting_support",
		AssigneeUserID: conv.ToPGText(ac.UserID),
		HiddenAt:       conv.ToPGTimestamptz(time.Now().Add(-time.Hour)),
	})
	require.NoError(t, err)
	require.NotNil(t, ac.ProjectID)
	_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *ac.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "onboarding-test",
		MarketplaceToken: conv.ToPGText("test-marketplace-token"),
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")
	for _, keys := range [][]string{{}, {"create-marketplace"}, {}} {
		_, err := organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, keys, new("gateway"), actor, nil)
		require.NoError(t, err)
		row, err := queries.GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{OrganizationID: ac.ActiveOrganizationID, TaskKey: stored.TaskKey})
		require.NoError(t, err)
		require.Equal(t, stored.Status, row.Status)
		require.Equal(t, stored.AssigneeUserID, row.AssigneeUserID)
		require.Equal(t, stored.AssigneeEmail, row.AssigneeEmail)
		if len(keys) > 0 {
			projected, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
			require.NoError(t, err)
			require.Equal(t, "done", setupTask(projected.Tasks, "create-marketplace").Status)
			require.True(t, setupTask(projected.Tasks, "create-marketplace").CompletedByFact)
		}
		if len(keys) == 0 && stored.HiddenAt.Valid {
			require.Equal(t, stored.HiddenAt, row.HiddenAt)
		}
		stored = row
	}
	// The helper has no assignment-email dependency; an unexpected send fails the mock.
	rows, err := queries.ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	_, err = organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, []string{}, nil, actor, nil)
	require.NoError(t, err)
	after, err := queries.ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, rows, after, "no-op reapplication must preserve timestamps")
}

func TestOnboardingSerializesWithTaskUpdates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := orgrepo.New(tx).LockOrganizationForSetupTaskUpdate(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	saved := make(chan error, 1)
	updated := make(chan error, 1)
	go func() {
		_, err := organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, []string{}, new("security"), urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
		saved <- err
	}()
	go func() {
		_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Status: new("in_progress")})
		updated <- err
	}()
	require.Eventually(t, func() bool {
		count, err := orgrepo.New(ti.conn).CountBlockedSetupTaskUpdatesFixture(ctx)
		return err == nil && count == 2
	}, 30*time.Second, 10*time.Millisecond, "both operations must reach the organization lock")
	select {
	case err := <-saved:
		t.Fatalf("configuration did not wait for organization lock: %v", err)
	case err := <-updated:
		t.Fatalf("task update did not wait for organization lock: %v", err)
	default:
	}
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-saved)
	require.NoError(t, <-updated)
	row, err := orgrepo.New(ti.conn).GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{OrganizationID: ac.ActiveOrganizationID, TaskKey: "instrument-agents"})
	require.NoError(t, err)
	require.True(t, row.HiddenAt.Valid)
	require.Equal(t, "in_progress", row.Status)
}

func TestOnboardingAuditsFactCompletionAndResolvedAssignee(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := orgrepo.New(ti.conn).UpsertOrganizationSetupTask(ctx, orgrepo.UpsertOrganizationSetupTaskParams{
		OrganizationID: ac.ActiveOrganizationID, TaskKey: "create-marketplace", Status: "awaiting_support",
		AssigneeUserID: conv.ToPGText(ac.UserID),
	})
	require.NoError(t, err)
	require.NotNil(t, ac.ProjectID)
	_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *ac.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "onboarding-test",
		MarketplaceToken: conv.ToPGText("test-marketplace-token"),
	})
	require.NoError(t, err)
	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	task := setupTask(listed.Tasks, "create-marketplace")
	require.Equal(t, "done", task.Status)
	require.NotNil(t, task.Assignee)
	require.NotEmpty(t, task.Assignee.Email)
	keys := make([]string, 0, len(listed.Tasks))
	for _, current := range listed.Tasks {
		if current.Key != task.Key {
			keys = append(keys, current.Key)
		}
	}
	_, err = organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, keys, nil, urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
	require.NoError(t, err)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	var before, after audit.OrganizationSetupTaskSnapshot
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &after))
	require.Equal(t, task.Key, before.Key)
	require.Equal(t, "done", before.Status)
	require.Equal(t, task.Assignee.UserID, before.Assignee.UserID)
	require.Equal(t, task.Assignee.Email, before.Assignee.Email)
	require.Equal(t, task.Assignee.Name, before.Assignee.Name)
	require.Equal(t, task.Assignee.PhotoURL, before.Assignee.PhotoURL)
	require.False(t, before.Hidden)
	before.Hidden = true
	require.Equal(t, before, after)
}

func TestOnboardingAuditsEffectiveBlockingAfterEntireSelection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")
	_, err := organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, []string{"instrument-agents", "confirm-traffic"}, nil, actor, nil)
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationSetupTask(ctx, orgrepo.UpsertOrganizationSetupTaskParams{
		OrganizationID: ac.ActiveOrganizationID, TaskKey: "confirm-traffic", Status: "in_progress",
	})
	require.NoError(t, err)
	_, err = organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, []string{}, nil, actor, nil)
	require.NoError(t, err)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	var before, after audit.OrganizationSetupTaskSnapshot
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &after))
	require.Equal(t, "confirm-traffic", before.Key)
	require.Equal(t, "todo", before.Status)
	require.Equal(t, []string{"instrument-agents"}, before.BlockedBy)
	require.False(t, before.Hidden)
	require.Equal(t, before.Key, after.Key)
	require.Equal(t, "in_progress", after.Status)
	require.Empty(t, after.BlockedBy)
	require.True(t, after.Hidden)
}

func TestOnboardingAuditFailureRollsBackSelection(t *testing.T) {
	t.Parallel()
	for _, action := range []audit.Action{audit.ActionOrganizationOnboardingUpdated, audit.ActionOrganizationSetupTaskUpdated} {
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestOrganizationsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			before, err := organizations.LoadOnboardingConfiguration(ctx, ti.conn, ac.ActiveOrganizationID)
			require.NoError(t, err)
			require.NoError(t, audittest.RejectAction(ctx, ti.conn, action))
			result, err := organizations.SaveOnboardingConfiguration(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, []string{}, new("gateway"), urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test"), nil)
			require.Error(t, err)
			require.Nil(t, result)
			after, err := organizations.LoadOnboardingConfiguration(ctx, ti.conn, ac.ActiveOrganizationID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
			require.NoError(t, err)
			require.Empty(t, rows)
			count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}
