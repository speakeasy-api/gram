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
	aiIntegrationUsageSyncInterval             = time.Hour
	aiIntegrationUsageSyncRunTimeout           = 55 * time.Minute
	aiIntegrationUsageSyncActivityTimeout      = 30 * time.Second
	aiIntegrationUsageSyncChildRuntime         = 15 * time.Minute
	aiIntegrationUsageSyncLeaseDuration        = 20 * time.Minute
	aiIntegrationUsageSyncConcurrency          = 5
	aiIntegrationUsageSyncRetryInitialInterval = 5 * time.Second
)

type AIIntegrationUsageSyncClient struct {
	TemporalEnv *tenv.Environment
}

type AIIntegrationUsageSyncConfigInput struct {
	Config  activities.AIIntegrationUsagePollConfig
	EndTime time.Time
}

type usageSyncLease struct {
	Owner     string
	ExpiresAt time.Time
}

func newUsageSyncLease(ctx workflow.Context, expiresAt time.Time) usageSyncLease {
	info := workflow.GetInfo(ctx)
	return usageSyncLease{
		Owner:     fmt.Sprintf("%s/%s", info.WorkflowExecution.ID, info.WorkflowExecution.RunID),
		ExpiresAt: expiresAt.UTC(),
	}
}

func (c *AIIntegrationUsageSyncClient) StartAIIntegrationUsageSync(ctx context.Context) (client.WorkflowRun, error) {
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    aiIntegrationUsageSyncWorkflowID,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, AIIntegrationUsageSyncWorkflow)
}

func AIIntegrationUsageSyncWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting AI integration usage polling")

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
	if err := syncUsageConfigs(ctx, activityCtx, a, endTime); err != nil {
		return err
	}

	logger.Info("AI integration usage polling completed")
	return nil
}

type runningUsageSync struct {
	config      activities.AIIntegrationUsagePollConfig
	child       workflow.ChildWorkflowFuture
	cancelChild workflow.CancelFunc
	cancelTimer workflow.CancelFunc
	timedOut    bool
	done        bool
	err         error
}

func syncUsageConfigs(ctx workflow.Context, activityCtx workflow.Context, a *Activities, endTime time.Time) error {
	logger := workflow.GetLogger(ctx)
	selector := workflow.NewSelector(ctx)
	active := make([]*runningUsageSync, 0, aiIntegrationUsageSyncConcurrency)
	ready := make([]*runningUsageSync, 0, aiIntegrationUsageSyncConcurrency)

	claimAndStartNext := func() (bool, error) {
		lease := newUsageSyncLease(ctx, workflow.Now(ctx).UTC().Add(aiIntegrationUsageSyncLeaseDuration))
		var configs []activities.AIIntegrationUsagePollConfig
		if err := workflow.ExecuteActivity(activityCtx, a.ClaimAIIntegrationUsagePolls, activities.ClaimAIIntegrationUsagePollsInput{
			Provider:       aiintegrations.ProviderCursor,
			EndTime:        endTime,
			Limit:          1,
			LeaseOwner:     lease.Owner,
			LeaseExpiresAt: lease.ExpiresAt,
		}).Get(activityCtx, &configs); err != nil {
			return false, fmt.Errorf("claim ai integration usage poll config: %w", err)
		}
		if len(configs) == 0 {
			return false, nil
		}
		cfg := configs[0]

		childCtx, cancelChild := workflow.WithCancel(ctx)
		childCtx = workflow.WithChildOptions(childCtx, workflow.ChildWorkflowOptions{
			WorkflowID:          aiIntegrationUsageSyncConfigWorkflowID(cfg.ID, endTime),
			WaitForCancellation: true,
		})
		timerCtx, cancelTimer := workflow.WithCancel(ctx)

		run := &runningUsageSync{
			config:      cfg,
			cancelChild: cancelChild,
			cancelTimer: cancelTimer,
		}
		run.child = workflow.ExecuteChildWorkflow(childCtx, AIIntegrationUsageSyncConfigWorkflow, AIIntegrationUsageSyncConfigInput{
			Config:  cfg,
			EndTime: endTime,
		})
		timer := workflow.NewTimer(timerCtx, aiIntegrationUsageSyncChildRuntime)

		selector.AddFuture(run.child, func(f workflow.Future) {
			if run.done {
				return
			}
			run.done = true
			run.cancelTimer()
			run.err = f.Get(ctx, nil)
			ready = append(ready, run)
		})
		selector.AddFuture(timer, func(f workflow.Future) {
			if run.done {
				return
			}
			if err := f.Get(ctx, nil); err != nil {
				return
			}
			run.done = true
			run.timedOut = true
			ready = append(ready, run)
		})
		active = append(active, run)
		return true, nil
	}

	for len(active) < aiIntegrationUsageSyncConcurrency {
		started, err := claimAndStartNext()
		if err != nil {
			return err
		}
		if !started {
			break
		}
	}
	if len(active) == 0 {
		logger.Info("No AI integration configs to poll")
		return nil
	}

	for len(active) > 0 {
		ready = ready[:0]
		selector.Select(ctx)

		for _, run := range ready {
			if run.timedOut {
				logger.Warn("AI integration usage poll child timed out; cancelling",
					"config_id", run.config.ID,
					"organization_id", run.config.OrganizationID,
					"provider", run.config.Provider,
				)
				run.cancelChild()
				run.err = run.child.Get(ctx, nil)
			}
			if run.err != nil {
				logger.Error("AI integration usage poll child failed",
					"config_id", run.config.ID,
					"organization_id", run.config.OrganizationID,
					"provider", run.config.Provider,
					"error", run.err.Error(),
				)
			}
			if err := workflow.ExecuteActivity(activityCtx, a.ReleaseAIIntegrationUsagePollLease, activities.ReleaseAIIntegrationUsagePollLeaseInput{
				ConfigID:   run.config.ID,
				LeaseOwner: run.config.LeaseOwner,
			}).Get(activityCtx, nil); err != nil {
				logger.Error("failed to release AI integration usage poll lease",
					"config_id", run.config.ID,
					"organization_id", run.config.OrganizationID,
					"provider", run.config.Provider,
					"error", err.Error(),
				)
			}
			active = removeRunningUsageSync(active, run)
			for len(active) < aiIntegrationUsageSyncConcurrency {
				started, err := claimAndStartNext()
				if err != nil {
					return err
				}
				if !started {
					break
				}
			}
		}
	}
	return nil
}

func AIIntegrationUsageSyncConfigWorkflow(ctx workflow.Context, input AIIntegrationUsageSyncConfigInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: aiIntegrationUsageSyncChildRuntime,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SyncAIIntegrationUsage, activities.SyncAIIntegrationUsageInput{
		Config:  input.Config,
		EndTime: input.EndTime,
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("poll and persist ai integration usage: %w", err)
	}
	return nil
}

func removeRunningUsageSync(active []*runningUsageSync, done *runningUsageSync) []*runningUsageSync {
	for i, run := range active {
		if run == done {
			return append(active[:i], active[i+1:]...)
		}
	}
	return active
}

func aiIntegrationUsageSyncConfigWorkflowID(configID string, endTime time.Time) string {
	return fmt.Sprintf("v1:ai-integration-usage-sync:%s:%d", configID, endTime.UnixMilli())
}

func AddAIIntegrationUsageSyncSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
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
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create ai integration usage polling schedule: %w", err)
	}
	return nil
}
