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

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type IndexToolsetParams struct {
	ProjectID             uuid.UUID
	ToolsetID             uuid.UUID
	ToolsetSlug           types.Slug
	ToolsetVersion        int64
	DeploymentID          uuid.UUID
	PermanentFailureCount int
}

type IndexToolsetClient struct {
	Temporal client.Client
}

func ExecuteIndexToolset(
	ctx context.Context,
	env *tenv.Environment,
	params IndexToolsetParams,
) (client.WorkflowRun, error) {
	if env == nil {
		return nil, ErrTemporalUnavailable
	}

	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       indexToolsetWorkflowID(params),
		TaskQueue:                string(env.Queue()),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, IndexToolsetWorkflow, params)
}

func indexToolsetWorkflowID(params IndexToolsetParams) string {
	return fmt.Sprintf(
		"v3:index-toolset:%s:%d:%s",
		params.ToolsetID,
		params.ToolsetVersion,
		params.DeploymentID,
	)
}

func IndexToolsetWorkflow(
	ctx workflow.Context,
	params IndexToolsetParams,
) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 45 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	for {
		err := workflow.ExecuteActivity(
			ctx,
			a.GenerateToolsetEmbeddings,
			activities.GenerateToolsetEmbeddingsInput{
				ProjectID:      params.ProjectID,
				ToolsetID:      params.ToolsetID,
				ToolsetSlug:    params.ToolsetSlug,
				ToolsetVersion: params.ToolsetVersion,
				DeploymentID:   params.DeploymentID,
			},
		).Get(ctx, nil)
		if err == nil {
			return nil
		}

		var applicationErr *temporal.ApplicationError
		if errors.As(err, &applicationErr) && applicationErr.Type() == activities.GenerateToolsetEmbeddingsSupersededErrorType {
			return nil
		}
		if !errors.As(err, &applicationErr) || applicationErr.Type() != activities.GenerateToolsetEmbeddingsPermanentErrorType {
			return err
		}

		params.PermanentFailureCount++
		if params.PermanentFailureCount >= indexToolsetPermanentFailureLimit {
			workflow.GetLogger(ctx).Error(
				"toolset indexing suppressed after repeated permanent provider failures",
				"permanent_failure_count", params.PermanentFailureCount,
				"error", err.Error(),
			)
			return nil
		}

		retryDelay := indexToolsetPermanentRetryDelay(params)
		workflow.GetLogger(ctx).Warn(
			"toolset indexing blocked by a permanent provider response; cooling down",
			"retry_delay", retryDelay,
			"permanent_failure_count", params.PermanentFailureCount,
		)
		if err := workflow.Sleep(ctx, retryDelay); err != nil {
			return err
		}
	}
}

const (
	indexToolsetPermanentRetryInitial = time.Hour
	indexToolsetPermanentRetryMaximum = 24 * time.Hour
	indexToolsetPermanentFailureLimit = 10
)

func indexToolsetPermanentRetryDelay(params IndexToolsetParams) time.Duration {
	exponent := min(max(params.PermanentFailureCount-1, 0), 5)

	delay := indexToolsetPermanentRetryInitial * time.Duration(1<<exponent)
	if delay >= indexToolsetPermanentRetryMaximum {
		return indexToolsetPermanentRetryMaximum
	}

	// A stable local hash supplies deterministic positive jitter without using
	// randomness in workflow code. It spreads retries for a failed provider.
	hash := int64(1469598103934665603)
	for _, b := range []byte(fmt.Sprintf("%s:%s:%d", params.ProjectID, params.ToolsetSlug, params.PermanentFailureCount)) {
		hash ^= int64(b)
		hash *= 1099511628211
	}
	jitter := hash % int64(delay/5)
	if jitter < 0 {
		jitter = -jitter
	}
	delay += time.Duration(jitter)
	if delay > indexToolsetPermanentRetryMaximum {
		return indexToolsetPermanentRetryMaximum
	}
	return delay
}
