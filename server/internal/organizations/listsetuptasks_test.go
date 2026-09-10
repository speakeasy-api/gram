package organizations_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestService_ListSetupTasksProjectsCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)

	// The default board is the guided journey only: the four tasks marked
	// HiddenByDefault stay off it for every org.
	require.Len(t, result.Tasks, 5)
	require.Equal(t, "identity-provider", result.Tasks[0].Key)
	require.Equal(t, "litellm", result.Tasks[3].Key)
	require.Equal(t, "additional-agent-config", result.Tasks[4].Key)
	for _, key := range []string{"anthropic-admin-controls", "distribute-servers", "configure-policies", "platform-mcp"} {
		require.Nil(t, setupTask(result.Tasks, key), key)
	}
	for _, task := range result.Tasks {
		require.Empty(t, task.BlockedBy, task.Key)
		require.Equal(t, "todo", task.Status, task.Key)
		require.False(t, task.Hidden, task.Key)
	}
	require.False(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)
	require.False(t, setupTask(result.Tasks, "instrument-agents").CompletedByFact)
}

// A platform admin asking for hidden tasks gets the whole catalog, with the
// default-hidden ones flagged so the board can mark them.
func TestService_ListSetupTasksRevealsDefaultHiddenToPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	platformCtx := contextvalues.SetAuthContext(ctx, &platformAuth)

	includeHidden := true
	result, err := ti.service.ListSetupTasks(platformCtx, &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	require.Len(t, result.Tasks, 9)
	require.Equal(t, "platform-mcp", result.Tasks[8].Key)
	for _, key := range []string{"anthropic-admin-controls", "distribute-servers", "configure-policies", "platform-mcp"} {
		require.True(t, setupTask(result.Tasks, key).Hidden, key)
	}
	require.False(t, setupTask(result.Tasks, "identity-provider").Hidden)
}

func TestService_ListSetupTasksAppliesCompletionFactsWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)

	// Single sign-on alone is not the identity provider outcome: directory
	// sync is part of the same card, so the task stays open until both are
	// configured.
	require.NoError(t, orgrepo.New(ti.conn).SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "todo", setupTask(result.Tasks, "identity-provider").Status)
	require.False(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)

	require.NoError(t, orgrepo.New(ti.conn).SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	result, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "done", setupTask(result.Tasks, "identity-provider").Status)
	require.True(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)
	require.False(t, setupTask(result.Tasks, "instrument-agents").CompletedByFact)

	rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows, "completion projection must not persist catalog defaults or facts")
}

func TestService_ListSetupTasksResolvesEmailAssigneeAndScopesOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	upperEmail := "  " + *authCtx.Email + "  "
	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: nil, Email: &upperEmail},
	})
	require.NoError(t, err)

	otherOrgID := "org_setup_board_isolation"
	require.NoError(t, orgrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: otherOrgID, Name: "Other organization", Slug: "other-organization"}))
	otherAuth := *authCtx
	otherAuth.ActiveOrganizationID = otherOrgID
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)
	otherCtx = authztest.WithExactGrants(t, otherCtx, authz.NewGrant(authz.ScopeOrgRead, otherOrgID))
	otherResult, err := ti.service.ListSetupTasks(otherCtx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Nil(t, setupTask(otherResult.Tasks, "instrument-agents").Assignee)

	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	assignee := setupTask(result.Tasks, "instrument-agents").Assignee
	require.NotNil(t, assignee)
	require.Equal(t, authCtx.UserID, *assignee.UserID)
	require.Equal(t, conv.NormalizeEmail(*authCtx.Email), assignee.Email)
}

func TestService_ListSetupTasksHiddenTaskPlatformVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	platformCtx := contextvalues.SetAuthContext(ctx, &platformAuth)
	hidden := true
	_, err := ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Hidden: &hidden})
	require.NoError(t, err)

	includeHidden := true
	platformResult, err := ti.service.ListSetupTasks(platformCtx, &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	require.True(t, setupTask(platformResult.Tasks, "instrument-agents").Hidden)

	normalResult, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	require.Nil(t, setupTask(normalResult.Tasks, "instrument-agents"))
}

// Restoring a default-hidden task has to actually reveal it: the board offers
// Restore on those cards, and it used to be a no-op that still reported
// success because the catalog default was ORed back over the row.
func TestService_ListSetupTasksRestoresADefaultHiddenTask(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	platformCtx := contextvalues.SetAuthContext(ctx, &platformAuth)

	before, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Nil(t, setupTask(before.Tasks, "configure-policies"), "hidden by default")

	visible := false
	_, err = ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Hidden: &visible})
	require.NoError(t, err)

	after, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	restored := setupTask(after.Tasks, "configure-policies")
	require.NotNil(t, restored, "restore has to reveal it on the ordinary board")
	require.False(t, restored.Hidden)

	// And it can be hidden again.
	hidden := true
	_, err = ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Hidden: &hidden})
	require.NoError(t, err)

	again, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Nil(t, setupTask(again.Tasks, "configure-policies"))
}

func TestService_ListSetupTasksRequiresOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.Nil(t, result)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func setupTask(tasks []*gen.SetupTask, key string) *gen.SetupTask {
	for _, task := range tasks {
		if task.Key == key {
			return task
		}
	}
	return nil
}
