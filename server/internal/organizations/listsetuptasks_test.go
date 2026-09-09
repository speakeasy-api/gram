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
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/stretchr/testify/require"
)

func TestService_ListSetupTasksProjectsCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)

	// The default board is the guided journey only: the three tasks marked
	// HiddenByDefault stay off it for every org.
	require.Len(t, result.Tasks, 5)
	require.Equal(t, "identity-provider", result.Tasks[0].Key)
	require.Equal(t, "enable-logging", result.Tasks[1].Key)
	require.Equal(t, "additional-agent-config", result.Tasks[4].Key)
	for _, key := range []string{"distribute-servers", "configure-policies", "platform-mcp"} {
		require.Nil(t, setupTask(result.Tasks, key), key)
	}
	for _, task := range result.Tasks {
		require.Empty(t, task.BlockedBy, task.Key)
		require.Equal(t, "todo", task.Status, task.Key)
		require.False(t, task.Hidden, task.Key)
	}
	require.False(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)
	require.False(t, setupTask(result.Tasks, "enable-logging").CompletedByFact)
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
	require.Len(t, result.Tasks, 8)
	require.Equal(t, "platform-mcp", result.Tasks[7].Key)
	for _, key := range []string{"distribute-servers", "configure-policies", "platform-mcp"} {
		require.True(t, setupTask(result.Tasks, key).Hidden, key)
	}
	require.False(t, setupTask(result.Tasks, "identity-provider").Hidden)
}

func TestService_ListSetupTasksMarksLoggingDoneOnceTheBundleIsEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features := productfeaturesrepo.New(ti.conn)
	enable := func(feature productfeatures.Feature) {
		t.Helper()
		_, err := features.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID, FeatureName: string(feature),
		})
		require.NoError(t, err)
	}

	enable(productfeatures.FeatureLogs)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "todo", setupTask(result.Tasks, "enable-logging").Status, "logs alone is not the full bundle")
	require.False(t, setupTask(result.Tasks, "enable-logging").CompletedByFact)

	enable(productfeatures.FeatureToolIOLogs)
	enable(productfeatures.FeatureSessionCapture)
	result, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "done", setupTask(result.Tasks, "enable-logging").Status)
	require.True(t, setupTask(result.Tasks, "enable-logging").CompletedByFact)

	_, err = features.DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureSessionCapture),
	})
	require.NoError(t, err)
	result, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "todo", setupTask(result.Tasks, "enable-logging").Status, "an admin disable reopens the task")
	require.False(t, setupTask(result.Tasks, "enable-logging").CompletedByFact)
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
