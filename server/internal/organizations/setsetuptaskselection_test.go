package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestService_SetSetupTaskSelectionControlsVisibleTasks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.SetSetupTaskSelection(ctx, &gen.SetSetupTaskSelectionPayload{
		VisibleTaskKeys: []string{"create-marketplace", "distribute-servers"},
	})
	require.NoError(t, err)
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	require.Equal(t, []string{"create-marketplace", "distribute-servers"}, keys)

	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, result.Tasks, listed.Tasks)
}

func TestService_SetSetupTaskSelectionRejectsUnknownTasks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	result, err := ti.service.SetSetupTaskSelection(ctx, &gen.SetSetupTaskSelectionPayload{VisibleTaskKeys: []string{"unknown-task"}})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_SetSetupTaskSelectionRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	result, err := ti.service.SetSetupTaskSelection(readOnlyCtx, &gen.SetSetupTaskSelectionPayload{VisibleTaskKeys: []string{}})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
}
