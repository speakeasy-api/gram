package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	aiIntegrationUsageSyncWorkflowID           = "v1:ai-integration-usage-sync"
	aiIntegrationUsageSyncScheduleID           = "v1:ai-integration-usage-sync-schedule"
	aiIntegrationUsageSyncScheduledWorkflowID  = aiIntegrationUsageSyncScheduleID + "/scheduled"
	aiIntegrationUsageSyncInterval             = 5 * time.Minute
	aiIntegrationUsageSyncRunTimeout           = 55 * time.Minute
	aiIntegrationUsageSyncActivityTimeout      = 30 * time.Second
	aiIntegrationUsageSyncPollActivityTimeout  = 50 * time.Minute
	aiIntegrationUsageSyncChildConcurrency     = 5
	aiIntegrationUsageSyncRetryInitialInterval = 5 * time.Second
)

type AIIntegrationUsageSyncClient struct {
	TemporalEnv *tenv.Environment
}

type AIIntegrationUsageSyncConfigInput struct {
	ConfigID string
	EndTime  time.Time
}

func (c *AIIntegrationUsageSyncClient) StartAIIntegrationUsageSync(ctx context.Context) (client.WorkflowRun, error) {
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    aiIntegrationUsageSyncWorkflowID,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, AIIntegrationUsageSyncWorkflow)
}

func AIIntegrationUsageSyncWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: aiIntegrationUsageSyncActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    aiIntegrationUsageSyncRetryInitialInterval,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	endTime := workflow.Now(ctx).UTC()
	pollDueBefore := endTime

	for {
		var candidates []aiintegrations.UsagePollCandidate
		if err := workflow.ExecuteActivity(activityCtx, a.GetAIIntegrationsCandidates, activities.GetAIIntegrationsCandidatesInput{
			Provider:      aiintegrations.ProviderCursor,
			PollDueBefore: pollDueBefore,
			Limit:         aiIntegrationUsageSyncChildConcurrency,
		}).Get(activityCtx, &candidates); err != nil {
			return fmt.Errorf("get ai integrations candidates: %w", err)
		}
		if len(candidates) == 0 {
			break
		}

		type runningUsageSync struct {
			candidate aiintegrations.UsagePollCandidate
			child     workflow.ChildWorkflowFuture
		}

		batch := make([]runningUsageSync, 0, len(candidates))
		for _, candidate := range candidates {
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            buildUsageSyncConfigWorkflowID(candidate.Provider, candidate.OrganizationID),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				WaitForCancellation:   true,
			})
			child := workflow.ExecuteChildWorkflow(childCtx, AIIntegrationUsageSyncConfigWorkflow, AIIntegrationUsageSyncConfigInput{
				ConfigID: candidate.ID.String(),
				EndTime:  endTime,
			})
			if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				if !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
					return fmt.Errorf("start ai integration usage poll child: %w", err)
				}
				continue
			}
			batch = append(batch, runningUsageSync{
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

func AIIntegrationUsageSyncConfigWorkflow(ctx workflow.Context, input AIIntegrationUsageSyncConfigInput) error {
	taskQueue := AIIntegrationUsageSyncTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           taskQueue,
		StartToCloseTimeout: aiIntegrationUsageSyncPollActivityTimeout,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: activities.SyncAIIntegrationUsageMaxAttempts,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SyncAIIntegrationUsage, activities.SyncAIIntegrationUsageInput{
		ConfigID: input.ConfigID,
		EndTime:  input.EndTime,
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("poll and persist ai integration usage: %w", err)
	}
	return nil
}

func buildUsageSyncConfigWorkflowID(provider string, organizationID string) string {
	return fmt.Sprintf("v1:ai-integration-usage-sync-config:%s:%s", provider, organizationID)
}

func AddAIIntegrationUsageSyncSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := aiIntegrationUsageSyncScheduleOptions(temporalEnv)
	_, err := scheduleClient.Create(ctx, options)
	if err == nil {
		return nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create ai integration usage polling schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, aiIntegrationUsageSyncScheduleID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			if schedule.Policy == nil {
				schedule.Policy = &client.SchedulePolicies{}
			}
			schedule.Policy.Overlap = options.Overlap
			return &client.ScheduleUpdate{Schedule: &schedule}, nil
		},
	}); err != nil {
		return fmt.Errorf("update ai integration usage polling schedule: %w", err)
	}
	return nil
}

func aiIntegrationUsageSyncScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      aiIntegrationUsageSyncScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: aiIntegrationUsageSyncInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 aiIntegrationUsageSyncScheduledWorkflowID,
			Workflow:           AIIntegrationUsageSyncWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: aiIntegrationUsageSyncRunTimeout,
		},
	}
}
