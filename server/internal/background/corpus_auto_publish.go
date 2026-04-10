package background

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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

// QueryEligibleDrafts returns drafts matching the auto-publish filter criteria.
func (a *AutoPublishActivities) QueryEligibleDrafts(_ context.Context, _ uuid.UUID, _ autopublish.Config) ([]uuid.UUID, error) {
	return nil, fmt.Errorf("not implemented")
}

// BatchPublishDrafts publishes the given draft IDs.
func (a *AutoPublishActivities) BatchPublishDrafts(_ context.Context, _ uuid.UUID, _ string, _ []uuid.UUID) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// AutoPublishWorkflow is a Temporal cron workflow that auto-publishes eligible drafts.
func AutoPublishWorkflow(_ workflow.Context, _ AutoPublishWorkflowParams) (*AutoPublishWorkflowResult, error) {
	// TODO: implement
	return &AutoPublishWorkflowResult{Published: 0, CommitSHA: ""}, nil
}
