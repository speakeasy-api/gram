package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestBackfillWorkOSWorkflow_BackfillsAllOrganizations(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(context.Context) ([]string, error) {
			return []string{"org_01", "org_02"}, nil
		},
		activity.RegisterOptions{Name: "ListWorkOSOrganizations"},
	)

	var globalRolesBackfilled bool
	env.RegisterActivityWithOptions(
		func(context.Context) error {
			globalRolesBackfilled = true
			return nil
		},
		activity.RegisterOptions{Name: "BackfillWorkOSGlobalRoles"},
	)

	var backfilledOrgIDs []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.BackfillWorkOSOrganizationParams) error {
			backfilledOrgIDs = append(backfilledOrgIDs, params.WorkOSOrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "BackfillWorkOSOrganization"},
	)

	env.ExecuteWorkflow(BackfillWorkOSWorkflow, BackfillWorkOSParams{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, globalRolesBackfilled)
	require.Equal(t, []string{"org_01", "org_02"}, backfilledOrgIDs)
}

func TestBackfillWorkOSWorkflow_BackfillsSingleOrganization(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var globalRolesBackfilled bool
	env.RegisterActivityWithOptions(
		func(context.Context) error {
			globalRolesBackfilled = true
			return nil
		},
		activity.RegisterOptions{Name: "BackfillWorkOSGlobalRoles"},
	)

	var backfilledOrgIDs []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.BackfillWorkOSOrganizationParams) error {
			backfilledOrgIDs = append(backfilledOrgIDs, params.WorkOSOrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "BackfillWorkOSOrganization"},
	)

	env.ExecuteWorkflow(BackfillWorkOSWorkflow, BackfillWorkOSParams{WorkOSOrganizationID: "org_01TARGET"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, globalRolesBackfilled)
	require.Equal(t, []string{"org_01TARGET"}, backfilledOrgIDs)
}
