package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The integration credentials backfill seals, under each enrolled
// organization's own key, the integration credentials it wrote before it was
// enrolled in organization encryption. Writes after enrolment seal both
// copies themselves, so this only catches up rows written earlier.
//
// Temporal actions/month: ~1,440 per namespace (one schedule start and one
// activity per hour, plus at most one retry per failed run). Fixed: it doesn't
// scale with organizations.
const (
	integrationCredentialsBackfillWorkflowID   = "v1:integration-credentials-backfill"
	integrationCredentialsBackfillInterval     = time.Hour
	integrationCredentialsBackfillCatchup      = integrationCredentialsBackfillInterval - time.Second
	integrationCredentialsBackfillRunTimeout   = 55 * time.Minute
	integrationCredentialsBackfillStartToClose = 25 * time.Minute
	IntegrationCredentialsBackfillActivityName = "BackfillIntegrationCredentials"
)

// IntegrationCredentialsBackfiller seals enrolled organizations' existing
// integration credentials under their own keys.
type IntegrationCredentialsBackfiller interface {
	Run(ctx context.Context) (integrationcredentials.BackfillResult, error)
}

func IntegrationCredentialsBackfillWorkflow(ctx workflow.Context) (integrationcredentials.BackfillResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: integrationCredentialsBackfillStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})

	var result integrationcredentials.BackfillResult
	if err := workflow.ExecuteActivity(ctx, IntegrationCredentialsBackfillActivityName).Get(ctx, &result); err != nil {
		return integrationcredentials.BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}, fmt.Errorf("backfill integration credentials: %w", err)
	}

	return result, nil
}

// IntegrationCredentialsBackfillActivities runs the backfill. A worker
// without a backfiller (organization encryption not configured) does nothing.
type IntegrationCredentialsBackfillActivities struct {
	logger     *slog.Logger
	backfiller IntegrationCredentialsBackfiller
}

func NewIntegrationCredentialsBackfillActivities(logger *slog.Logger, backfiller IntegrationCredentialsBackfiller) *IntegrationCredentialsBackfillActivities {
	return &IntegrationCredentialsBackfillActivities{logger: logger, backfiller: backfiller}
}

func (a *IntegrationCredentialsBackfillActivities) Backfill(ctx context.Context) (integrationcredentials.BackfillResult, error) {
	if a.backfiller == nil {
		return integrationcredentials.BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}, nil
	}

	result, err := a.backfiller.Run(ctx)
	if err != nil {
		return result, fmt.Errorf("backfill integration credentials: %w", err)
	}
	if result.Sealed > 0 {
		a.logger.InfoContext(ctx, "backfilled integration credentials", attr.SlogValueInt(result.Sealed))
	}

	return result, nil
}

func integrationCredentialsBackfillScheduleID(queue string) string {
	return fmt.Sprintf("v1:integration-credentials-backfill:%s", queue)
}

func AddIntegrationCredentialsBackfillSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildIntegrationCredentialsBackfillScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create integration credentials backfill schedule: %w", err)
	}

	// Queue-scoped, so reconciling touches only this deployment's copy and
	// keeps any operator pause.
	if err := scheduleClient.GetHandle(ctx, options.ID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			if existing, ok := schedule.Action.(*client.ScheduleWorkflowAction); ok && existing.TaskQueue != string(temporalEnv.Queue()) {
				return nil, fmt.Errorf("schedule %s targets task queue %q, not %q", options.ID, existing.TaskQueue, temporalEnv.Queue())
			}
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			setScheduleCatchup(&schedule, integrationCredentialsBackfillCatchup)
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update integration credentials backfill schedule: %w", err)
	}

	return nil
}

func buildIntegrationCredentialsBackfillScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	queue := string(temporalEnv.Queue())

	return client.ScheduleOptions{
		CatchupWindow: integrationCredentialsBackfillCatchup,
		ID:            integrationCredentialsBackfillScheduleID(queue),
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: integrationCredentialsBackfillInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 fmt.Sprintf("%s:%s", integrationCredentialsBackfillWorkflowID, queue),
			Workflow:           IntegrationCredentialsBackfillWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: integrationCredentialsBackfillRunTimeout,
		},
	}
}
