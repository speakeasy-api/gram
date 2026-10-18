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
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The sweep is a backstop: rotation already retires expired issuer keys. It
// activates pending keys past the activation delay (finishing a routine
// rotation within the hour), revokes retired keys past the overlap and
// retries KMS cleanup of every revoked key until it succeeds. Each activity sweeps
// for a fixed budget and returns a cursor; the workflow continues from it for
// a bounded number of pages and returns where it stopped. The next hourly run
// resumes there through the schedule's last completion result, so slow or
// failing organizations early in the order can't starve later ones; the
// cursor wraps only after a full traversal. Payloads carry an organization id
// cursor, never organization data.
//
// Temporal actions/month: ~2,200 per namespace (one schedule start and one
// activity per hour); each extra page costs one activity, at most
// internalSessionIssuerKeySweepMaxPages-1 more per hour (~4,400 worst case,
// plus one retry per failed page).
// Carrying the cursor costs no actions. Fixed: it doesn't scale with orgs.
const (
	internalSessionIssuerKeySweepWorkflowID = "v1:internal-session-issuer-key-sweep" //nolint:gosec // A workflow ID, not a credential.
	internalSessionIssuerKeySweepInterval   = time.Hour
	internalSessionIssuerKeySweepCatchup    = internalSessionIssuerKeySweepInterval - time.Second
	internalSessionIssuerKeySweepRunTimeout = 55 * time.Minute
	// A page stops starting organizations after its budget; the last one
	// started may then run its full share. Budget plus share stays a minute
	// inside StartToClose, and every page with its retry fits the run timeout:
	// 3 pages x 2 attempts x 8m plus backoff is about 50m.
	internalSessionIssuerKeySweepPageBudget   = 5 * time.Minute
	internalSessionIssuerKeySweepStartToClose = 8 * time.Minute
	internalSessionIssuerKeySweepMaxPages     = 3
	InternalSessionIssuerKeySweepActivityName = "SweepInternalSessionIssuerKeys"
)

// InternalSessionIssuerKeySweeper retires expired issuer keys and finishes
// pending KMS cleanup across organizations, resuming after an organization id
// and stopping once budget is spent.
type InternalSessionIssuerKeySweeper interface {
	SweepEMAIssuerKeysFrom(ctx context.Context, after string, budget time.Duration) (internalsessionissuers.SweepProgress, error)
}

func internalSessionIssuerKeySweepScheduleID(queue string) string {
	return fmt.Sprintf("v1:internal-session-issuer-key-sweep:%s", queue)
}

// InternalSessionIssuerKeySweepResult is where the next run resumes; empty
// starts a new traversal.
type InternalSessionIssuerKeySweepResult struct {
	ResumeAfter string
}

func InternalSessionIssuerKeySweepWorkflow(ctx workflow.Context) (InternalSessionIssuerKeySweepResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: internalSessionIssuerKeySweepStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})
	var last InternalSessionIssuerKeySweepResult
	if workflow.HasLastCompletionResult(ctx) {
		if err := workflow.GetLastCompletionResult(ctx, &last); err != nil {
			workflow.GetLogger(ctx).Warn("discarding unreadable issuer key sweep cursor", "error", err)
			last = InternalSessionIssuerKeySweepResult{ResumeAfter: ""}
		}
	}
	after := last.ResumeAfter
	for range internalSessionIssuerKeySweepMaxPages {
		var progress internalsessionissuers.SweepProgress
		if err := workflow.ExecuteActivity(ctx, InternalSessionIssuerKeySweepActivityName, after).Get(ctx, &progress); err != nil {
			// A failed run records no result, so the next run resumes from the
			// last successful one.
			return InternalSessionIssuerKeySweepResult{ResumeAfter: ""}, fmt.Errorf("sweep internal session issuer keys: %w", err)
		}
		if progress.Done {
			return InternalSessionIssuerKeySweepResult{ResumeAfter: ""}, nil
		}
		after = progress.Next
	}
	return InternalSessionIssuerKeySweepResult{ResumeAfter: after}, nil
}

// InternalSessionIssuerKeySweepActivities runs the sweep. A worker without a
// sweeper (no KMS provisioning) sweeps nothing.
type InternalSessionIssuerKeySweepActivities struct {
	logger    *slog.Logger
	sweeper   InternalSessionIssuerKeySweeper
	pending   metric.Int64Gauge
	activated metric.Int64Counter
}

func NewInternalSessionIssuerKeySweepActivities(logger *slog.Logger, meterProvider metric.MeterProvider, sweeper InternalSessionIssuerKeySweeper) *InternalSessionIssuerKeySweepActivities {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background")
	pending, err := meter.Int64Gauge(
		"gram.internal_session_issuer.kms_cleanup.pending",
		metric.WithDescription("Revoked issuer keys whose KMS version is not yet disabled after a sweep"),
		metric.WithUnit("{key}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create issuer kms cleanup gauge", attr.SlogError(err))
	}
	activated, err := meter.Int64Counter(
		"gram.internal_session_issuer.key_activations",
		metric.WithDescription("Ready pending issuer keys the sweep activated"),
		metric.WithUnit("{key}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create issuer key activation counter", attr.SlogError(err))
	}
	return &InternalSessionIssuerKeySweepActivities{logger: logger, sweeper: sweeper, pending: pending, activated: activated}
}

// Sweep runs one budgeted page. It fails, so Temporal retries the page, only
// when it made no progress; otherwise failures are logged and paging goes on.
func (a *InternalSessionIssuerKeySweepActivities) Sweep(ctx context.Context, after string) (internalsessionissuers.SweepProgress, error) {
	if a.sweeper == nil {
		return internalsessionissuers.SweepProgress{Swept: 0, Activated: 0, Pending: 0, Next: "", Done: true}, nil
	}
	progress, err := a.sweeper.SweepEMAIssuerKeysFrom(ctx, after, internalSessionIssuerKeySweepPageBudget)
	// The count is fleet-wide and read after the sweep, so it stays accurate
	// when some organizations failed; -1 means it could not be read.
	if a.pending != nil && progress.Pending >= 0 {
		a.pending.Record(ctx, int64(progress.Pending))
	}
	if progress.Activated > 0 {
		if a.activated != nil {
			a.activated.Add(ctx, int64(progress.Activated))
		}
		a.logger.InfoContext(ctx, "activated ready internal session issuer keys", attr.SlogValueInt(progress.Activated))
	}
	if progress.Pending > 0 {
		a.logger.ErrorContext(ctx, "revoked internal session issuer keys still await kms cleanup", attr.SlogValueInt(progress.Pending))
	}
	if err != nil {
		if progress.Swept == 0 {
			return progress, fmt.Errorf("sweep issuer keys: %w", err)
		}
		a.logger.ErrorContext(ctx, "some organizations failed the issuer key sweep", attr.SlogError(err))
	}
	if progress.Swept > 0 {
		a.logger.InfoContext(ctx, "swept internal session issuer keys", attr.SlogValueInt(progress.Swept))
	}
	return progress, nil
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
