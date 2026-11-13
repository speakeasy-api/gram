package background

import (
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// recordPluginProjectPublishOutcome counts logical project publishes, not activity
// attempts. Call only after the publish future resolves, when retries are over.
// The workflow SDK suppresses metrics and logs during replay. Do not add tenant,
// project, workflow/run IDs, repository names, or error strings as metric tags.
func recordPluginProjectPublishOutcome(ctx workflow.Context, result plugins.PublishProjectResult, err error) {
	outcome := "published"
	var appErr *temporal.ApplicationError
	switch {
	case temporal.IsCanceledError(err):
		outcome = "canceled"
	case errors.As(err, &appErr) && appErr.Type() == activities.ErrTypePluginActorNotMember:
		outcome = "rejected"
	case err != nil:
		outcome = "failed"
	case result.Skipped:
		outcome = "skipped"
	}
	workflow.GetMetricsHandler(ctx).WithTags(map[string]string{"outcome": outcome}).
		Counter("plugin_project_publish_outcomes").Inc(1)
	// A fixed message and bounded outcome make this safe to aggregate. Temporal
	// supplies workflow context for drilldown without copying raw upstream errors.
	// Keep unchanged sweeps quiet while still counting their skipped outcomes.
	if outcome != "skipped" {
		workflow.GetLogger(ctx).Info("plugin project publish outcome", "outcome", outcome)
	}
}
