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
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/stretchr/testify/require"
)

func TestService_ListSetupTasksProjectsCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)

	// New boards show every card except the optional LiteLLM setup. A group
	// precedes its cards.
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	require.Equal(t, []string{
		"identity-provider", "enable-logging", "anthropic-observability",
		"agent-observability", "instrument-agents", "confirm-traffic", "additional-agent-config",
		"mcp-distribution", "create-marketplace", "distribute-servers", "platform-mcp",
		"anthropic-admin-controls", "configure-policies",
	}, keys)
	require.Nil(t, setupTask(result.Tasks, "litellm"))
	for _, task := range result.Tasks {
		switch task.Key {
		case "confirm-traffic":
			require.Equal(t, []string{"instrument-agents"}, task.BlockedBy)
		case "distribute-servers":
			require.Equal(t, []string{"create-marketplace"}, task.BlockedBy)
		default:
			require.Empty(t, task.BlockedBy, task.Key)
		}
		require.Equal(t, "todo", task.Status, task.Key)
		require.False(t, task.Hidden, task.Key)
	}
	observe := setupTask(result.Tasks, "agent-observability")
	require.NotNil(t, observe)
	require.True(t, observe.Group)
	require.Nil(t, observe.ParentKey)
	require.Nil(t, observe.Assignee)
	require.False(t, observe.CompletedByFact)
	instrument := setupTask(result.Tasks, "instrument-agents")
	require.NotNil(t, instrument)
	require.NotNil(t, instrument.ParentKey)
	require.Equal(t, "agent-observability", *instrument.ParentKey)
	require.False(t, setupTask(result.Tasks, "identity-provider").Group)
	require.Nil(t, setupTask(result.Tasks, "identity-provider").ParentKey)
	require.False(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)
	require.False(t, setupTask(result.Tasks, "instrument-agents").CompletedByFact)
}

// A platform admin asking for hidden tasks gets the whole catalog, with the
// default-hidden LiteLLM task flagged so the board can mark it.
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
	require.Len(t, result.Tasks, 14)
	require.Equal(t, "configure-policies", result.Tasks[13].Key)
	require.True(t, setupTask(result.Tasks, "litellm").Hidden)
	for _, task := range result.Tasks {
		if task.Key != "litellm" {
			require.False(t, task.Hidden, task.Key)
		}
	}
	require.True(t, setupTask(result.Tasks, "mcp-distribution").Group)
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	require.ElementsMatch(t, []string{
		"identity-provider", "enable-logging",
		"anthropic-observability", "agent-observability", "instrument-agents", "confirm-traffic", "litellm",
		"additional-agent-config", "mcp-distribution", "create-marketplace", "distribute-servers", "platform-mcp",
		"anthropic-admin-controls", "configure-policies",
	}, keys)
	require.Equal(t, []string{"create-marketplace"}, setupTask(result.Tasks, "distribute-servers").BlockedBy)
}

func TestService_ListSetupTasksAppliesCompletionFactsWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)

	// Single sign-on alone is not the identity provider outcome: the card
	// also covers directory sync, so it stays open until both are configured.
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
	require.Nil(t, setupTask(before.Tasks, "litellm"), "hidden by default")

	visible := false
	_, err = ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "litellm", Hidden: &visible})
	require.NoError(t, err)

	after, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	restored := setupTask(after.Tasks, "litellm")
	require.NotNil(t, restored, "restore has to reveal it on the ordinary board")
	require.False(t, restored.Hidden)

	// And it can be hidden again.
	hidden := true
	_, err = ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "litellm", Hidden: &hidden})
	require.NoError(t, err)

	again, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Nil(t, setupTask(again.Tasks, "litellm"))
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

func TestService_ListSetupTasksMarksLoggingDoneOnceTheBundleIsEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, &platformAuth)
	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "enable-logging", Hidden: new(false)})
	require.NoError(t, err)
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

func TestService_ListSetupTasksPreservesBranchCompletionFactsWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, &platformAuth)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)
	require.NoError(t, orgrepo.New(ti.conn).SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	require.NoError(t, orgrepo.New(ti.conn).SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	require.NotNil(t, authCtx.ProjectID)
	_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *authCtx.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "setup-board",
		MarketplaceToken: pgtype.Text{}, PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: new(true)})
	require.NoError(t, err)
	require.False(t, setupTask(result.Tasks, "create-marketplace").CompletedByFact, "a connection without a token is not published")
	require.Equal(t, "todo", setupTask(result.Tasks, "create-marketplace").Status)
	_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *authCtx.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "setup-board",
		MarketplaceToken: conv.ToPGText("synthetic-marketplace-token"), PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)
	result, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: new(true)})
	require.NoError(t, err)
	require.Equal(t, "done", setupTask(result.Tasks, "identity-provider").Status)
	require.Equal(t, "done", setupTask(result.Tasks, "create-marketplace").Status)
	require.True(t, setupTask(result.Tasks, "create-marketplace").CompletedByFact)
	require.False(t, setupTask(result.Tasks, "instrument-agents").CompletedByFact)
	require.True(t, setupTask(result.Tasks, "identity-provider").CompletedByFact)

	rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows, "completion projection must not persist catalog defaults or facts")
}

func TestService_ListSetupTasksReopenedPrerequisiteBlocksProgressedDependent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, &platformAuth)
	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "confirm-traffic", Hidden: new(false)})
	require.NoError(t, err)
	done := "done"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Status: &done})
	require.NoError(t, err)

	inProgress := "in_progress"
	dependent, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "confirm-traffic", Status: &inProgress})
	require.NoError(t, err)
	require.Equal(t, "in_progress", dependent.Status)
	require.Empty(t, dependent.BlockedBy)

	todo := "todo"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Status: &todo})
	require.NoError(t, err)

	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	dependent = setupTask(result.Tasks, "confirm-traffic")
	require.Equal(t, "todo", dependent.Status)
	require.Equal(t, []string{"instrument-agents"}, dependent.BlockedBy)
}

func TestService_ListSetupTasksHiddenPrerequisiteAndPlatformVisibility(t *testing.T) {
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
	require.Empty(t, setupTask(platformResult.Tasks, "confirm-traffic").BlockedBy)

	normalResult, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	require.Nil(t, setupTask(normalResult.Tasks, "instrument-agents"))
}

// Every key supported by either journey must still accept manual completion.
func TestService_UpdateSetupTaskCompletesMergedCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	done := "done"
	for _, key := range []string{
		"create-marketplace", "enable-logging",
		"identity-provider", "anthropic-observability", "anthropic-admin-controls",
		"instrument-agents", "litellm", "additional-agent-config", "confirm-traffic",
		"distribute-servers", "configure-policies", "platform-mcp",
	} {
		task, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: key, Status: &done})
		require.NoError(t, err, key)
		require.Equal(t, "done", task.Status, key)
		require.Empty(t, task.BlockedBy, key)
	}

	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Len(t, result.Tasks, 13)
	for _, task := range result.Tasks {
		require.Equal(t, "done", task.Status, task.Key)
	}
	// Groups cannot be marked by hand; their cards decide.
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "agent-observability", Status: &done})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
