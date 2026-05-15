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

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	cursorUsageMetricsWorkflowID           = "v1:cursor-usage-metrics"
	cursorUsageMetricsScheduleID           = "v1:cursor-usage-metrics-schedule"
	cursorUsageMetricsScheduledWorkflowID  = cursorUsageMetricsScheduleID + "/scheduled"
	cursorUsageMetricsInterval             = time.Hour
	cursorUsageMetricsActivityTimeout      = 5 * time.Minute
	cursorUsageMetricsWorkflowRunTimeout   = 30 * time.Minute
	cursorUsageMetricsRetryInitialInterval = 30 * time.Second
)

type CursorUsageMetricsClient struct {
	TemporalEnv *tenv.Environment
}

func (c *CursorUsageMetricsClient) StartCursorUsageMetrics(ctx context.Context) (client.WorkflowRun, error) {
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    cursorUsageMetricsWorkflowID,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, CursorUsageMetricsWorkflow)
}

func CursorUsageMetricsWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Cursor usage metrics polling")

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: cursorUsageMetricsActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    cursorUsageMetricsRetryInitialInterval,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	var configs []activities.CursorIntegrationConfig
	if err := workflow.ExecuteActivity(ctx, a.ListCursorIntegrationConfigs).Get(ctx, &configs); err != nil {
		return fmt.Errorf("list cursor integration configs: %w", err)
	}
	if len(configs) == 0 {
		logger.Info("No Cursor integration config to poll")
		return nil
	}

	endTime := workflow.Now(ctx).UTC()
	for _, cfg := range configs {
		if err := pollCursorUsageMetricsForConfig(ctx, a, cfg, endTime); err != nil {
			return err
		}
	}

	logger.Info("Cursor usage metrics polling completed successfully")
	return nil
}

func pollCursorUsageMetricsForConfig(ctx workflow.Context, a *Activities, cfg activities.CursorIntegrationConfig, endTime time.Time) error {
	for page := 1; ; page++ {
		var result activities.PollCursorUsageEventsPageOutput
		if err := workflow.ExecuteActivity(ctx, a.PollCursorUsageEventsPage, activities.PollCursorUsageEventsPageInput{
			Config:  cfg,
			EndTime: endTime,
			Page:    page,
		}).Get(ctx, &result); err != nil {
			return fmt.Errorf("poll cursor usage events page %d for org %s: %w", page, cfg.OrganizationID, err)
		}

		if err := workflow.ExecuteActivity(ctx, a.DeduplicateAndWriteCursorEvents, activities.DeduplicateAndWriteCursorEventsInput{
			Config:  cfg,
			EndTime: endTime,
			Events:  result.Events,
		}).Get(ctx, nil); err != nil {
			return fmt.Errorf("write cursor usage events page %d for org %s: %w", page, cfg.OrganizationID, err)
		}

		if !result.HasNextPage {
			break
		}
	}

	if err := workflow.ExecuteActivity(ctx, a.UpdateCursorPollWatermark, activities.UpdateCursorPollWatermarkInput{
		ConfigID: cfg.ID,
		At:       endTime,
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("update cursor poll watermark for org %s: %w", cfg.OrganizationID, err)
	}

	return nil
}

func AddCursorUsageMetricsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      cursorUsageMetricsScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: cursorUsageMetricsInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 cursorUsageMetricsScheduledWorkflowID,
			Workflow:           CursorUsageMetricsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: cursorUsageMetricsWorkflowRunTimeout,
		},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create cursor usage metrics schedule: %w", err)
	}
	return nil
}
