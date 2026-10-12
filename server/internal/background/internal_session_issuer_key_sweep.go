package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The sweep is a backstop: rotation and management reads already retire
// expired issuer keys. It revokes retired keys past the overlap and retries
// KMS cleanup of every revoked key until it succeeds. Payloads carry no
// organization data.
const (
	internalSessionIssuerKeySweepWorkflowID   = "v1:internal-session-issuer-key-sweep" //nolint:gosec // A workflow ID, not a credential.
	internalSessionIssuerKeySweepInterval     = time.Hour
	internalSessionIssuerKeySweepCatchup      = internalSessionIssuerKeySweepInterval - time.Second
	internalSessionIssuerKeySweepRunTimeout   = 35 * time.Minute
	InternalSessionIssuerKeySweepActivityName = "SweepInternalSessionIssuerKeys"
)

// InternalSessionIssuerKeySweeper retires expired issuer keys and finishes
// pending KMS cleanup across organizations. pending counts revoked keys whose
// KMS version is still enabled after the sweep, or is -1 when unknown.
type InternalSessionIssuerKeySweeper interface {
	SweepAllEMAIssuerKeys(ctx context.Context) (swept, pending int, err error)
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
	pending metric.Int64Gauge
}

func NewInternalSessionIssuerKeySweepActivities(logger *slog.Logger, meterProvider metric.MeterProvider, sweeper InternalSessionIssuerKeySweeper) *InternalSessionIssuerKeySweepActivities {
	pending, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background").Int64Gauge(
		"gram.internal_session_issuer.kms_cleanup.pending",
		metric.WithDescription("Revoked issuer keys whose KMS version is not yet disabled after a sweep"),
		metric.WithUnit("{key}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create issuer kms cleanup gauge", attr.SlogError(err))
	}
	return &InternalSessionIssuerKeySweepActivities{logger: logger, sweeper: sweeper, pending: pending}
}

func (a *InternalSessionIssuerKeySweepActivities) Sweep(ctx context.Context) (int, error) {
	if a.sweeper == nil {
		return 0, nil
	}
	swept, pending, err := a.sweeper.SweepAllEMAIssuerKeys(ctx)
	// The count is fleet-wide and read after the sweep, so it stays accurate
	// when some organizations failed; -1 means it could not be read.
	if a.pending != nil && pending >= 0 {
		a.pending.Record(ctx, int64(pending))
	}
	if pending > 0 {
		a.logger.ErrorContext(ctx, "revoked internal session issuer keys still await kms cleanup", attr.SlogValueInt(pending))
	}
	if err != nil {
		return swept, fmt.Errorf("sweep issuer keys: %w", err)
	}
	if swept > 0 {
		a.logger.InfoContext(ctx, "swept internal session issuer keys", attr.SlogValueInt(swept))
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
			if existing, ok := schedule.Action.(*client.ScheduleWorkflowAction); ok && existing.TaskQueue != string(temporalEnv.Queue()) {
				return nil, fmt.Errorf("schedule %s targets task queue %q, not %q", options.ID, existing.TaskQueue, temporalEnv.Queue())
			}
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
