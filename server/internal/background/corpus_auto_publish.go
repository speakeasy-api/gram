package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish"
)

// AutoPublishWorkflowParams holds the inputs for the auto-publish cron workflow.
type AutoPublishWorkflowParams struct {
	ProjectID      uuid.UUID
	OrganizationID string
}

// AutoPublishWorkflowResult holds the result of the auto-publish workflow execution.
type AutoPublishWorkflowResult struct {
	Published int
	CommitSHA string
}

// AutoPublishActivities defines the activity interface for auto-publish workflows.
type AutoPublishActivities struct{}

// GetAutoPublishConfig retrieves the auto-publish config for a project.
func (a *AutoPublishActivities) GetAutoPublishConfig(_ context.Context, _ uuid.UUID) (autopublish.Config, error) {
	// Registered on the worker; this stub is for Temporal dispatch.
	return autopublish.Config{}, fmt.Errorf("not implemented")
}

// QueryEligibleDrafts returns IDs of drafts matching the auto-publish filter criteria.
func (a *AutoPublishActivities) QueryEligibleDrafts(_ context.Context, _ uuid.UUID, _ autopublish.Config) ([]uuid.UUID, error) {
	return nil, fmt.Errorf("not implemented")
}

// BatchPublishDrafts publishes the given draft IDs.
func (a *AutoPublishActivities) BatchPublishDrafts(_ context.Context, _ uuid.UUID, _ string, _ []uuid.UUID) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// AutoPublishWorkflow is a Temporal cron workflow that auto-publishes eligible drafts.
func AutoPublishWorkflow(ctx workflow.Context, params AutoPublishWorkflowParams) (*AutoPublishWorkflowResult, error) {
	var a *AutoPublishActivities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	})

	// Step 1: Fetch the auto-publish config for this project.
	var cfg autopublish.Config
	err := workflow.ExecuteActivity(ctx, a.GetAutoPublishConfig, params.ProjectID).Get(ctx, &cfg)
	if err != nil {
		return nil, fmt.Errorf("get auto-publish config: %w", err)
	}

	// Step 2: If disabled, exit early.
	if !cfg.Enabled {
		return &AutoPublishWorkflowResult{Published: 0, CommitSHA: ""}, nil
	}

	// Step 3: Query eligible drafts.
	var draftIDs []uuid.UUID
	err = workflow.ExecuteActivity(ctx, a.QueryEligibleDrafts, params.ProjectID, cfg).Get(ctx, &draftIDs)
	if err != nil {
		return nil, fmt.Errorf("query eligible drafts: %w", err)
	}

	// Step 4: If no eligible drafts, exit early.
	if len(draftIDs) == 0 {
		return &AutoPublishWorkflowResult{Published: 0, CommitSHA: ""}, nil
	}

	// Step 5: Batch publish eligible drafts.
	var commitSHA string
	err = workflow.ExecuteActivity(ctx, a.BatchPublishDrafts, params.ProjectID, params.OrganizationID, draftIDs).Get(ctx, &commitSHA)
	if err != nil {
		return nil, fmt.Errorf("batch publish drafts: %w", err)
	}

	return &AutoPublishWorkflowResult{
		Published: len(draftIDs),
		CommitSHA: commitSHA,
	}, nil
}
