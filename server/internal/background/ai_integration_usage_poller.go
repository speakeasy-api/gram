package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	aiUsagePollerCoordinatorWorkflowID = "v1:ai-usage-poller-coordinator"
	aiUsagePollerCoordinatorScheduleID = "v1:ai-usage-poller-coordinator-schedule"

	// aiUsagePollerCoordinatorInterval controls how often the
	// coordinator checks for due integrations.
	aiUsagePollerCoordinatorInterval = 5 * time.Minute

	// aiUsagePollerCoordinatorRunTimeout is the total budgeted
	// time for each scheduled coordinator run.
	aiUsagePollerCoordinatorRunTimeout = 8 * time.Hour

	// aiUsagePollerCoordinatorActivityTimeout is the budget for
	// short coordinator activities like candidate listing.
	aiUsagePollerCoordinatorActivityTimeout = 30 * time.Second

	// aiUsagePollerActivityTimeout is the budget for
	// one provider/config usage poll attempt.
	aiUsagePollerActivityTimeout = 2 * time.Hour

	// aiUsagePollerActivityScheduleToCloseTimeout bounds the full
	// provider/config usage poll across all Temporal retries.
	aiUsagePollerActivityScheduleToCloseTimeout = 6 * time.Hour

	// aiUsagePollerCoordinatorChildConcurrency limits how many
	// provider/config child workflows a coordinator starts per batch.
	aiUsagePollerCoordinatorChildConcurrency = 5

	// aiUsagePollerCoordinatorRetryInitialInterval is the first
	// backoff delay for short coordinator activity retries.
	aiUsagePollerCoordinatorRetryInitialInterval = 5 * time.Second

	// aiUsagePollerActivityRetryInitialInterval is the first backoff delay
	// for provider/config usage poll activity retries.
	aiUsagePollerActivityRetryInitialInterval = time.Minute

	// aiUsagePollerActivityRetryMaximumInterval caps backoff between
	// provider/config usage poll activity retries.
	aiUsagePollerActivityRetryMaximumInterval = 15 * time.Minute
)

func AIUsagePollerCoordinatorWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: aiUsagePollerCoordinatorActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    aiUsagePollerCoordinatorRetryInitialInterval,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities

	for {
		var candidates []aiintegrations.UsagePollCandidate
		if err := workflow.ExecuteActivity(activityCtx, a.GetAIIntegrationsCandidates, activities.GetAIIntegrationsCandidatesInput{
			PollDueBefore: workflow.Now(ctx).UTC(),
			Limit:         aiUsagePollerCoordinatorChildConcurrency,
		}).Get(activityCtx, &candidates); err != nil {
			return fmt.Errorf("get ai integrations candidates: %w", err)
		}
		if len(candidates) == 0 {
			break
		}

		type runningPoller struct {
			candidate aiintegrations.UsagePollCandidate
			child     workflow.ChildWorkflowFuture
		}

		batch := make([]runningPoller, 0, len(candidates))
		for _, candidate := range candidates {
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            buildAIUsagePollerWorkflowID(candidate.OrganizationSlug, candidate.ID, candidate.Provider),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				WaitForCancellation:   true,
			})

			child := workflow.ExecuteChildWorkflow(childCtx, AIUsagePollerWorkflow, candidate.ID.String())
			if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				if !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
					return fmt.Errorf("start poller child: %w", err)
				}
				continue
			}

			batch = append(batch, runningPoller{
				candidate: candidate,
				child:     child,
			})
		}

		if len(batch) == 0 {
			break
		}

		selector := workflow.NewSelector(ctx)
		remaining := len(batch)
		for _, run := range batch {
			selector.AddFuture(run.child, func(f workflow.Future) {
				remaining--
			})
		}

		for remaining > 0 {
			selector.Select(ctx)
		}
	}

	return nil
}

func AIUsagePollerWorkflow(ctx workflow.Context, configID string) error {
	taskQueue := AIUsagePollerTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              taskQueue,
		StartToCloseTimeout:    aiUsagePollerActivityTimeout,
		ScheduleToCloseTimeout: aiUsagePollerActivityScheduleToCloseTimeout,
		HeartbeatTimeout:       time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    activities.PollUsageMaxAttempts,
			InitialInterval:    aiUsagePollerActivityRetryInitialInterval,
			BackoffCoefficient: 2,
			MaximumInterval:    aiUsagePollerActivityRetryMaximumInterval,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.PollAIData, configID).Get(ctx, nil); err != nil {
		return fmt.Errorf("poll and persist ai integration usage: %w", err)
	}

	return nil
}

func buildAIUsagePollerWorkflowID(organizationSlug string, configID uuid.UUID, provider string) string {
	return fmt.Sprintf("v1:ai-usage-poller:%s:%s:%s", organizationSlug, configID.String(), provider)
}

type TemporalAIUsagePoller struct {
	TemporalEnv *tenv.Environment
}

func (p *TemporalAIUsagePoller) Poll(ctx context.Context, organizationSlug string, configID uuid.UUID, provider string) error {
	_, err := p.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    buildAIUsagePollerWorkflowID(organizationSlug, configID, provider),
		TaskQueue:             string(p.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, AIUsagePollerWorkflow, configID.String())
	return err
}

func AddAIUsagePollerCoordinatorSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create ai integration usage polling schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, aiUsagePollerCoordinatorScheduleID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			if schedule.Policy == nil {
				schedule.Policy = &client.SchedulePolicies{
					Overlap:        enums.SCHEDULE_OVERLAP_POLICY_SKIP,
					CatchupWindow:  0,
					PauseOnFailure: false,
				}
			}
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update ai integration usage polling schedule: %w", err)
	}
	return nil
}

func buildScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      aiUsagePollerCoordinatorScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: aiUsagePollerCoordinatorInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 aiUsagePollerCoordinatorWorkflowID,
			Workflow:           AIUsagePollerCoordinatorWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: aiUsagePollerCoordinatorRunTimeout,
		},
	}
}
