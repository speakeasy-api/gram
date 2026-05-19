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
	aiIntegrationUsageSyncChildRuntime         = 50 * time.Minute
	aiIntegrationUsageSyncConcurrency          = 5
	aiIntegrationUsageSyncCandidateBatchSize   = 25
	aiIntegrationUsageSyncRetryInitialInterval = 5 * time.Second
)

type AIIntegrationUsageSyncClient struct {
	TemporalEnv *tenv.Environment
}

type AIIntegrationUsageSyncConfigInput struct {
	Config  activities.AIIntegrationUsagePollConfig
	EndTime time.Time
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
	config activities.AIIntegrationUsagePollConfig
	child  workflow.ChildWorkflowFuture
	done   bool
	err    error
}

type usageSyncCandidatePager struct {
	cursor    *activities.AIIntegrationUsagePollCursor
	buffer    []activities.AIIntegrationUsagePollConfig
	exhausted bool
}

func syncUsageConfigs(ctx workflow.Context, activityCtx workflow.Context, a *Activities, endTime time.Time) error {
	logger := workflow.GetLogger(ctx)
	selector := workflow.NewSelector(ctx)
	active := make([]*runningUsageSync, 0, aiIntegrationUsageSyncConcurrency)
	ready := make([]*runningUsageSync, 0, aiIntegrationUsageSyncConcurrency)
	pager := &usageSyncCandidatePager{
		cursor:    nil,
		buffer:    nil,
		exhausted: false,
	}

	nextCandidate := func() (activities.AIIntegrationUsagePollConfig, bool, error) {
		for len(pager.buffer) == 0 && !pager.exhausted {
			var configs []activities.AIIntegrationUsagePollConfig
			if err := workflow.ExecuteActivity(activityCtx, a.ListAIIntegrationUsagePollCandidates, activities.ListAIIntegrationUsagePollCandidatesInput{
				Provider: aiintegrations.ProviderCursor,
				EndTime:  endTime,
				Limit:    aiIntegrationUsageSyncCandidateBatchSize,
				Cursor:   pager.cursor,
			}).Get(activityCtx, &configs); err != nil {
				return activities.AIIntegrationUsagePollConfig{}, false, fmt.Errorf("list ai integration usage poll candidates: %w", err)
			}
			if len(configs) == 0 {
				pager.exhausted = true
				break
			}
			last := configs[len(configs)-1]
			pager.cursor = &activities.AIIntegrationUsagePollCursor{
				LastPolledAt:   last.LastPolledAt,
				OrganizationID: last.OrganizationID,
				Provider:       last.Provider,
			}
			pager.buffer = append(pager.buffer, configs...)
		}
		if len(pager.buffer) == 0 {
			return activities.AIIntegrationUsagePollConfig{
				ID:             "",
				OrganizationID: "",
				Provider:       "",
				ProjectID:      "",
				APIKey:         "",
				LastPolledAt:   time.Time{},
			}, false, nil
		}
		cfg := pager.buffer[0]
		pager.buffer = pager.buffer[1:]
		return cfg, true, nil
	}

	startNext := func() (bool, error) {
		for {
			cfg, ok, err := nextCandidate()
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}

			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            aiIntegrationUsageSyncConfigWorkflowID(cfg.Provider, cfg.OrganizationID),
				WorkflowRunTimeout:    aiIntegrationUsageSyncChildRuntime,
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				WaitForCancellation:   true,
			})
			run := &runningUsageSync{
				config: cfg,
				child:  nil,
				done:   false,
				err:    nil,
			}
			run.child = workflow.ExecuteChildWorkflow(childCtx, AIIntegrationUsageSyncConfigWorkflow, AIIntegrationUsageSyncConfigInput{
				Config:  cfg,
				EndTime: endTime,
			})

			if err := run.child.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
					logger.Info("AI integration usage poll child already running; skipping",
						"config_id", cfg.ID,
						"organization_id", cfg.OrganizationID,
						"provider", cfg.Provider,
						"workflow_id", aiIntegrationUsageSyncConfigWorkflowID(cfg.Provider, cfg.OrganizationID),
					)
					continue
				}
				return false, fmt.Errorf("start ai integration usage poll child: %w", err)
			}

			selector.AddFuture(run.child, func(f workflow.Future) {
				if run.done {
					return
				}
				run.done = true
				run.err = f.Get(ctx, nil)
				ready = append(ready, run)
			})
			active = append(active, run)
			return true, nil
		}
	}

	for len(active) < aiIntegrationUsageSyncConcurrency {
		started, err := startNext()
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
			if run.err != nil {
				logger.Error("AI integration usage poll child failed",
					"config_id", run.config.ID,
					"organization_id", run.config.OrganizationID,
					"provider", run.config.Provider,
					"error", run.err.Error(),
				)
			}
			active = removeRunningUsageSync(active, run)
			for len(active) < aiIntegrationUsageSyncConcurrency {
				started, err := startNext()
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

func aiIntegrationUsageSyncConfigWorkflowID(provider string, organizationID string) string {
	return fmt.Sprintf("v1:ai-integration-usage-sync-config:%s:%s", provider, organizationID)
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
