package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	startupSeedWorkflowIDPrefix = "v1:startup-seed"

	// startupSeedActivityTimeout bounds one apply. Seeds write tens of rows,
	// so two minutes only ever expires on a stalled database.
	startupSeedActivityTimeout = 2 * time.Minute

	// A worker from the previous build refuses the task while a rollout is
	// in flight. Fifteen attempts backing off to two minutes retry for about
	// 25 minutes, which outlasts a rollout.
	startupSeedMaxAttempts          = 15
	startupSeedRetryInitialInterval = 15 * time.Second
	startupSeedRetryMaxInterval     = 2 * time.Minute

	// startupSeedWorkflowRunTimeout covers the full retry budget with room
	// for the final attempt to finish.
	startupSeedWorkflowRunTimeout = 45 * time.Minute
)

// StartupSeedWorkflow applies one version of one startup seed.
func StartupSeedWorkflow(ctx workflow.Context, args activities.ApplyStartupSeedArgs) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: startupSeedActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    startupSeedMaxAttempts,
			InitialInterval:    startupSeedRetryInitialInterval,
			MaximumInterval:    startupSeedRetryMaxInterval,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.ApplyStartupSeed, args).Get(ctx, nil); err != nil {
		return fmt.Errorf("apply startup seed %s: %w", args.Name, err)
	}
	return nil
}

func startupSeedWorkflowID(queue tenv.TaskQueueName, seed activities.StartupSeed) string {
	return fmt.Sprintf("%s:%s:%s:%s", startupSeedWorkflowIDPrefix, queue, seed.Name, seed.Version)
}

// KickStartupSeeds starts one run per seed version on this task queue. The
// workflow ID carries the version, so sibling replicas and later restarts on
// the same build collapse into the run that already happened, and only a
// version that failed is started again.
func KickStartupSeeds(ctx context.Context, temporalEnv *tenv.Environment, seeds []activities.StartupSeed) error {
	var errs []error
	for _, seed := range seeds {
		_, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:                    startupSeedWorkflowID(temporalEnv.Queue(), seed),
			TaskQueue:             string(temporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
			WorkflowRunTimeout:    startupSeedWorkflowRunTimeout,
		}, StartupSeedWorkflow, activities.ApplyStartupSeedArgs{Name: seed.Name, Version: seed.Version})
		if _, ok := errors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("start startup seed %s: %w", seed.Name, err))
		}
	}
	return errors.Join(errs...)
}
