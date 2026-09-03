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
	"github.com/stretchr/testify/require"
)

func TestService_ListSetupTasksProjectsCatalogAndDependencies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Len(t, result.Tasks, 9)
	require.Equal(t, "connect-idp", result.Tasks[0].Key)
	require.Equal(t, "platform-mcp", result.Tasks[8].Key)
	require.Equal(t, []string{"instrument-agents"}, setupTask(result.Tasks, "confirm-traffic").BlockedBy)
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
	require.NoError(t, orgrepo.New(ti.conn).SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	require.NoError(t, orgrepo.New(ti.conn).SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{WorkosID: org.WorkosID, Enabled: conv.PtrToPGBool(conv.PtrEmpty(true)), WorkosLastEventID: pgtype.Text{}}))
	require.NotNil(t, authCtx.ProjectID)
	_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *authCtx.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "setup-board",
		MarketplaceToken: pgtype.Text{}, PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, "done", setupTask(result.Tasks, "connect-idp").Status)
	require.Equal(t, "done", setupTask(result.Tasks, "directory-sync").Status)
	require.Equal(t, "done", setupTask(result.Tasks, "create-marketplace").Status)
	require.Empty(t, setupTask(result.Tasks, "distribute-servers").BlockedBy)

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
