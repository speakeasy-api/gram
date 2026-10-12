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
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The sweep is a backstop: rotation and management reads already retire
// expired issuer keys. It revokes retired keys past the overlap and disables
// their KMS versions. Payloads carry no organization data.
const (
	internalSessionIssuerKeySweepWorkflowID   = "v1:internal-session-issuer-key-sweep" //nolint:gosec // A workflow ID, not a credential.
	internalSessionIssuerKeySweepInterval     = time.Hour
	internalSessionIssuerKeySweepCatchup      = internalSessionIssuerKeySweepInterval - time.Second
	internalSessionIssuerKeySweepRunTimeout   = 30 * time.Minute
	InternalSessionIssuerKeySweepActivityName = "SweepInternalSessionIssuerKeys"
)

// InternalSessionIssuerKeySweeper retires expired issuer keys across organizations.
type InternalSessionIssuerKeySweeper interface {
	SweepAllEMAIssuerKeys(ctx context.Context) (int, error)
}

func internalSessionIssuerKeySweepScheduleID(queue string) string {
	return fmt.Sprintf("v1:internal-session-issuer-key-sweep:%s", queue)
}

func InternalSessionIssuerKeySweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})
	var swept int
	if err := workflow.ExecuteActivity(ctx, InternalSessionIssuerKeySweepActivityName).Get(ctx, &swept); err != nil {
		return fmt.Errorf("sweep internal session issuer keys: %w", err)
	}
	return nil
}

// InternalSessionIssuerKeySweepActivities runs the sweep. A worker without a
// sweeper (no KMS provisioning) sweeps nothing.
type InternalSessionIssuerKeySweepActivities struct {
	logger  *slog.Logger
	sweeper InternalSessionIssuerKeySweeper
}

func NewInternalSessionIssuerKeySweepActivities(logger *slog.Logger, sweeper InternalSessionIssuerKeySweeper) *InternalSessionIssuerKeySweepActivities {
	return &InternalSessionIssuerKeySweepActivities{logger: logger, sweeper: sweeper}
}

func (a *InternalSessionIssuerKeySweepActivities) Sweep(ctx context.Context) (int, error) {
	if a.sweeper == nil {
		return 0, nil
	}
	swept, err := a.sweeper.SweepAllEMAIssuerKeys(ctx)
	if err != nil {
		return swept, fmt.Errorf("sweep issuer keys: %w", err)
	}
	if swept > 0 {
		a.logger.InfoContext(ctx, "swept expired internal session issuer keys", attr.SlogValueInt(swept))
	}
	return swept, nil
}

func AddInternalSessionIssuerKeySweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildInternalSessionIssuerKeySweepScheduleOptions(temporalEnv)
	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create internal session issuer key sweep schedule: %w", err)
	}
	// Queue-scoped, so reconciling touches only this deployment's copy and
	// keeps any operator pause.
	if err := scheduleClient.GetHandle(ctx, options.ID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			setScheduleCatchup(&schedule, internalSessionIssuerKeySweepCatchup)
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update internal session issuer key sweep schedule: %w", err)
	}
	return nil
}

func buildInternalSessionIssuerKeySweepScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	queue := string(temporalEnv.Queue())
	return client.ScheduleOptions{
		CatchupWindow: internalSessionIssuerKeySweepCatchup,
		ID:            internalSessionIssuerKeySweepScheduleID(queue),
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: internalSessionIssuerKeySweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 fmt.Sprintf("%s:%s", internalSessionIssuerKeySweepWorkflowID, queue),
			Workflow:           InternalSessionIssuerKeySweepWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: internalSessionIssuerKeySweepRunTimeout,
		},
	}
}
