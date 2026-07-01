package background

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	attributeMetricsBackfillWorkflowTimeout = 2 * time.Hour
	attributeMetricsBackfillActivityTimeout = 45 * time.Minute
)

type BackfillAttributeMetricsSummariesParams struct {
	OrganizationID string `json:"organization_id"`
}

type BackfillAttributeMetricsSummariesResult = activities.BackfillAttributeMetricsSummariesResult

func ExecuteBackfillAttributeMetricsSummariesWorkflow(ctx context.Context, env *tenv.Environment, params BackfillAttributeMetricsSummariesParams) (client.WorkflowRun, error) {
	params = normalizeBackfillAttributeMetricsSummariesParams(params)
	id := fmt.Sprintf("v1:backfill-attribute-metrics-summaries:%s:%s", params.OrganizationID, activities.AttributeMetricsBackfillCutoff().Format("20060102T150405Z"))
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowRunTimeout:    attributeMetricsBackfillWorkflowTimeout,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, BackfillAttributeMetricsSummariesWorkflow, params)
}

func BackfillAttributeMetricsSummariesWorkflow(ctx workflow.Context, params BackfillAttributeMetricsSummariesParams) (*BackfillAttributeMetricsSummariesResult, error) {
	params = normalizeBackfillAttributeMetricsSummariesParams(params)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: attributeMetricsBackfillActivityTimeout,
		HeartbeatTimeout:    5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			MaximumInterval:    2 * time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	})

	var a *Activities
	var result activities.BackfillAttributeMetricsSummariesResult
	if err := workflow.ExecuteActivity(ctx, a.BackfillAttributeMetricsSummaries, activities.BackfillAttributeMetricsSummariesParams(params)).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("backfill attribute metrics summaries: %w", err)
	}
	return &result, nil
}

func normalizeBackfillAttributeMetricsSummariesParams(params BackfillAttributeMetricsSummariesParams) BackfillAttributeMetricsSummariesParams {
	params.OrganizationID = strings.TrimSpace(params.OrganizationID)
	return params
}
