package background

import (
	"context"
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
	openRouterCreditsMetricsWorkflowID          = "v1:collect-openrouter-credits-metrics"
	openRouterCreditsMetricsScheduleID          = "v1:collect-openrouter-credits-metrics-schedule"
	openRouterCreditsMetricsScheduledWorkflowID = "v1:collect-openrouter-credits-metrics/scheduled"

	// Activity budget: timeout (30s) × max attempts (2) × activities (3:
	// collect, fire, alert) = 3min, which fits comfortably under
	// WorkflowRunTimeout (4min). The upstream OpenRouter `/v1/key` poll and the
	// alert sends are sub-second in practice; 30s is a generous per-activity
	// ceiling that surfaces stalls quickly rather than masking them through
	// retry loops.
	openRouterCreditsMetricsActivityMaxRetries = 2
	openRouterCreditsMetricsActivityTimeout    = 30 * time.Second
	openRouterCreditsMetricsScheduleInterval   = 5 * time.Minute
	openRouterCreditsMetricsWorkflowRunTimeout = 4 * time.Minute
)

// openRouterCreditsMetricsAccountTypes is the allowlist of organization
// account types whose OpenRouter keys are polled for usage. Add a tier here
// (e.g. "pro") to expand coverage — no schema, infra, or Datadog change
// required as long as the per-org monitor selector matches.
var openRouterCreditsMetricsAccountTypes = []string{"enterprise"}

type OpenRouterCreditsMetricsClient struct {
	TemporalEnv *tenv.Environment
}

func (c *OpenRouterCreditsMetricsClient) StartCollectOpenRouterCreditsMetrics(ctx context.Context) (client.WorkflowRun, error) {
	run, err := c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    openRouterCreditsMetricsWorkflowID,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, CollectOpenRouterCreditsMetricsWorkflow)
	if err != nil {
		return nil, fmt.Errorf("execute collect openrouter credits metrics workflow: %w", err)
	}
	return run, nil
}

func CollectOpenRouterCreditsMetricsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: openRouterCreditsMetricsActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: openRouterCreditsMetricsActivityMaxRetries,
		},
	})

	var a *Activities
	var metrics []activities.OpenRouterCreditsMetric
	if err := workflow.ExecuteActivity(
		ctx,
		a.CollectOpenRouterCreditsMetrics,
		activities.CollectOpenRouterCreditsMetricsArgs{AccountTypes: openRouterCreditsMetricsAccountTypes},
	).Get(ctx, &metrics); err != nil {
		return fmt.Errorf("collect openrouter credits metrics: %w", err)
	}

	if err := workflow.ExecuteActivity(ctx, a.FireOpenRouterCreditsMetrics, metrics).Get(ctx, nil); err != nil {
		return fmt.Errorf("fire openrouter credits metrics: %w", err)
	}

	// Threshold alerting is best-effort and must never fail the metrics run:
	// the gauges above are the durable signal, whereas a missed alert self-heals
	// on the next 5-minute tick. Log and swallow so a transient email or DB
	// hiccup does not mark the workflow failed.
	if err := workflow.ExecuteActivity(ctx, a.MaybeSendOpenRouterCreditsAlerts, metrics).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("send openrouter credits alerts", "error", err.Error())
	}

	return nil
}

func AddOpenRouterCreditsMetricsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: openRouterCreditsMetricsScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{Every: openRouterCreditsMetricsScheduleInterval},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 openRouterCreditsMetricsScheduledWorkflowID,
			Workflow:           CollectOpenRouterCreditsMetricsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: openRouterCreditsMetricsWorkflowRunTimeout,
		},
	})
	if err != nil {
		return fmt.Errorf("create openrouter credits metrics schedule: %w", err)
	}

	return nil
}
