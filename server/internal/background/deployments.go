package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ProcessDeploymentWorkflowParams struct {
	ProjectID      uuid.UUID
	DeploymentID   uuid.UUID
	IdempotencyKey string
}

type ProcessDeploymentWorkflowResult struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	Status       string
}

func ExecuteProcessDeploymentWorkflow(ctx context.Context, env *tenv.Environment, params ProcessDeploymentWorkflowParams) (client.WorkflowRun, error) {
	id := conv.Default(params.IdempotencyKey, params.DeploymentID.String())
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("v1:process-deployment:%s", id),
		TaskQueue:                string(env.Queue()),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:       time.Minute * 2,
	}, ProcessDeploymentWorkflow, params)
}

func ProcessDeploymentWorkflow(ctx workflow.Context, params ProcessDeploymentWorkflowParams) (*ProcessDeploymentWorkflowResult, error) {
	// This can stay nil/unassigned. Temporal just uses this to get activity names.
	// The actual activities are registered in the CLI layer (cmd/gram/worker.go).
	var a *Activities

	logger := workflow.GetLogger(ctx)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})

	var pendingTransition activities.TransitionDeploymentResult
	err := workflow.ExecuteActivity(
		ctx,
		a.TransitionDeployment,
		params.ProjectID,
		params.DeploymentID,
		"pending",
	).Get(ctx, &pendingTransition)
	if err != nil {
		return nil, fmt.Errorf("failed to transition deployment: %w", err)
	}

	if !pendingTransition.Moved {
		return nil, temporal.NewNonRetryableApplicationError(
			"Deployment did not move to pending status because of unexpected current state",
			"InvariantViolation",
			nil,
			map[string]string{
				"moved":   fmt.Sprintf("%v", pendingTransition.Moved),
				"want":    "pending",
				"current": pendingTransition.Status,
			},
		)
	}

	finalStatus := "completed"

	// Validate deployment with no retries
	noRetryCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	validationErr := workflow.ExecuteActivity(
		noRetryCtx,
		a.ValidateDeployment,
		params.ProjectID,
		params.DeploymentID,
	).Get(noRetryCtx, nil)
	if validationErr != nil {
		finalStatus = "failed"
		logger.Error(
			"failed to validate deployment",
			"error", validationErr.Error(),
			string(attr.ProjectIDKey), params.ProjectID,
			string(attr.DeploymentIDKey), params.DeploymentID,
		)
	}

	// Only process deployment if validation passed
	if validationErr == nil {
		err = workflow.ExecuteActivity(
			ctx,
			a.ProcessDeployment,
			params.ProjectID,
			params.DeploymentID,
		).Get(ctx, nil)
		if err != nil {
			finalStatus = "failed"
			logger.Error(
				"failed to process deployment",
				"error", err.Error(),
				string(attr.ProjectIDKey), params.ProjectID,
				string(attr.DeploymentIDKey), params.DeploymentID,
			)
		}

		err = workflow.ExecuteActivity(
			ctx,
			a.ProvisionFunctionsAccess,
			params.ProjectID,
			params.DeploymentID,
		).Get(ctx, nil)
		if err != nil {
			finalStatus = "failed"
			logger.Error(
				"failed to provision access credentials for functions",
				"error", err.Error(),
				string(attr.ProjectIDKey), params.ProjectID,
				string(attr.DeploymentIDKey), params.DeploymentID,
			)
		}

		err = workflow.ExecuteActivity(
			ctx,
			a.DeployFunctionRunners,
			activities.DeployFunctionRunnersRequest{
				ProjectID:    params.ProjectID,
				DeploymentID: params.DeploymentID,
			},
		).Get(ctx, nil)
		if err != nil {
			finalStatus = "failed"
			logger.Error(
				"failed to deploy function runners",
				"error", err.Error(),
				string(attr.ProjectIDKey), params.ProjectID,
				string(attr.DeploymentIDKey), params.DeploymentID,
			)
		}
	}

	var finalTransition activities.TransitionDeploymentResult
	err = workflow.ExecuteActivity(
		ctx,
		a.TransitionDeployment,
		params.ProjectID,
		params.DeploymentID,
		finalStatus,
	).Get(ctx, &finalTransition)
	if err != nil {
		return nil, fmt.Errorf("failed to transition deployment: %w", err)
	}

	if !finalTransition.Moved {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("Deployment did not move to %s status because of unexpected current state", finalStatus),
			"InvariantViolation",
			nil,
			map[string]string{
				"moved":   fmt.Sprintf("%v", finalTransition.Moved),
				"want":    finalStatus,
				"current": finalTransition.Status,
			},
		)
	}

	return &ProcessDeploymentWorkflowResult{
		ProjectID:    params.ProjectID,
		DeploymentID: params.DeploymentID,
		Status:       finalTransition.Status,
	}, nil
}
