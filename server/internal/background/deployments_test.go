package background

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	backgroundactivities "github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestProcessDeploymentWorkflowUnguardedUsesLegacyCompletionActivity(t *testing.T) {
	t.Parallel()
	testProcessDeploymentWorkflowCompletionActivity(t, false, 0)
}

func TestProcessDeploymentWorkflowGuardedUsesExpectedActiveCompletionActivity(t *testing.T) {
	t.Parallel()
	testProcessDeploymentWorkflowCompletionActivity(t, true, 0)
}

func testProcessDeploymentWorkflowCompletionActivity(t *testing.T, guarded bool, registrationFailures int) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ProcessDeploymentWorkflow)
	env.RegisterWorkflow(ProcessGuardedDeploymentWorkflow)
	env.RegisterWorkflowWithOptions(
		func(workflow.Context, IndexToolsetSweepParams) error { return nil },
		workflow.RegisterOptions{Name: "IndexToolsetSweepWorkflow"},
	)

	projectID := uuid.New()
	deploymentID := uuid.New()
	expectedActiveID := uuid.New()
	var transitionStatuses []string
	guardedCalls := 0

	env.RegisterActivityWithOptions(func(_ context.Context, _ uuid.UUID, _ uuid.UUID, status string) (*backgroundactivities.TransitionDeploymentResult, error) {
		transitionStatuses = append(transitionStatuses, status)
		resultStatus := status
		if status == "completed" {
			resultStatus = "failed"
		}
		return &backgroundactivities.TransitionDeploymentResult{Status: resultStatus, Moved: true}, nil
	}, activity.RegisterOptions{Name: "TransitionDeployment"})
	env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error { return nil }, activity.RegisterOptions{Name: "ValidateDeployment"})
	env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error { return nil }, activity.RegisterOptions{Name: "ProcessDeployment"})
	env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error { return nil }, activity.RegisterOptions{Name: "ProvisionFunctionsAccess"})
	env.RegisterActivityWithOptions(func(context.Context, backgroundactivities.DeployFunctionRunnersRequest) error { return nil }, activity.RegisterOptions{Name: "DeployFunctionRunners"})
	env.RegisterActivityWithOptions(func(_ context.Context, gotProjectID uuid.UUID, gotDeploymentID uuid.UUID, gotExpectedActiveID uuid.UUID) (*backgroundactivities.TransitionDeploymentResult, error) {
		guardedCalls++
		if guardedCalls <= registrationFailures {
			return nil, temporal.NewApplicationError("activity not yet registered on old worker", "ActivityNotRegisteredError")
		}
		require.Equal(t, projectID, gotProjectID)
		require.Equal(t, deploymentID, gotDeploymentID)
		require.Equal(t, expectedActiveID, gotExpectedActiveID)
		return &backgroundactivities.TransitionDeploymentResult{Status: "completed", Moved: true}, nil
	}, activity.RegisterOptions{Name: "CompleteDeploymentWithExpectedActive"})

	params := ProcessDeploymentWorkflowParams{ProjectID: projectID, DeploymentID: deploymentID}
	if guarded {
		params.ExpectedActiveDeploymentID = &expectedActiveID
	}
	if guarded {
		env.ExecuteWorkflow(ProcessGuardedDeploymentWorkflow, params)
	} else {
		env.ExecuteWorkflow(ProcessDeploymentWorkflow, params)
	}

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result ProcessDeploymentWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	if guarded {
		require.Equal(t, "completed", result.Status)
		require.Equal(t, []string{"pending"}, transitionStatuses)
		require.Equal(t, registrationFailures+1, guardedCalls)
	} else {
		require.Equal(t, "failed", result.Status)
		require.Equal(t, []string{"pending", "completed"}, transitionStatuses)
		require.Zero(t, guardedCalls)
	}
}

func TestProcessGuardedDeploymentWorkflowRefusesMissingGuard(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ProcessGuardedDeploymentWorkflow)
	env.ExecuteWorkflow(ProcessGuardedDeploymentWorkflow, ProcessDeploymentWorkflowParams{ProjectID: uuid.New(), DeploymentID: uuid.New()})
	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "MissingDeploymentGuard")
}

func TestProcessGuardedDeploymentWorkflowRetriesDuringRollingRegistration(t *testing.T) {
	t.Parallel()
	testProcessDeploymentWorkflowCompletionActivity(t, true, 6)
}

func TestProcessDeploymentWorkflowRetiredPulseFailsWithoutRetry(t *testing.T) {
	t.Parallel()
	for _, guarded := range []bool{false, true} {
		t.Run(fmt.Sprintf("guarded=%t", guarded), func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterWorkflow(ProcessDeploymentWorkflow)
			env.RegisterWorkflow(ProcessGuardedDeploymentWorkflow)
			var statuses []string
			attempts := 0
			env.RegisterActivityWithOptions(func(_ context.Context, _ uuid.UUID, _ uuid.UUID, status string) (*backgroundactivities.TransitionDeploymentResult, error) {
				statuses = append(statuses, status)
				return &backgroundactivities.TransitionDeploymentResult{Status: status, Moved: true}, nil
			}, activity.RegisterOptions{Name: "TransitionDeployment"})
			env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error { return nil }, activity.RegisterOptions{Name: "ValidateDeployment"})
			env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error {
				attempts++
				return temporal.NewNonRetryableApplicationError("Pulse catalog source is retired", "retired_pulse_catalog", externalmcp.ErrCatalogSourceRetired)
			}, activity.RegisterOptions{Name: "ProcessDeployment"})
			env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID) error { return nil }, activity.RegisterOptions{Name: "ProvisionFunctionsAccess"})
			env.RegisterActivityWithOptions(func(context.Context, backgroundactivities.DeployFunctionRunnersRequest) error { return nil }, activity.RegisterOptions{Name: "DeployFunctionRunners"})
			activationCalls := 0
			env.RegisterActivityWithOptions(func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*backgroundactivities.TransitionDeploymentResult, error) {
				activationCalls++
				return &backgroundactivities.TransitionDeploymentResult{Status: "completed", Moved: true}, nil
			}, activity.RegisterOptions{Name: "CompleteDeploymentWithExpectedActive"})
			params := ProcessDeploymentWorkflowParams{ProjectID: uuid.New(), DeploymentID: uuid.New()}
			if guarded {
				expected := uuid.New()
				params.ExpectedActiveDeploymentID = &expected
				env.ExecuteWorkflow(ProcessGuardedDeploymentWorkflow, params)
			} else {
				env.ExecuteWorkflow(ProcessDeploymentWorkflow, params)
			}
			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())
			var result ProcessDeploymentWorkflowResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, "failed", result.Status)
			require.Equal(t, []string{"pending", "failed"}, statuses)
			require.Equal(t, 1, attempts)
			require.Zero(t, activationCalls)
		})
	}
}
