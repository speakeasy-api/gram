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

// The organization encryption sweep is the only mechanism that notices a
// customer revoking or disabling their key encryption key between requests,
// completes key switches past their grace period, and disables managed keys
// nothing uses any more. One activity per hour does both halves; organizations
// not reached within the budget are first in line next hour, because the
// probe orders by the last check time.
//
// Temporal actions/month: ~1,440 per namespace (one schedule start and one
// activity per hour, plus at most one retry per failed run). Fixed: it doesn't
// scale with organizations.
const (
	organizationEncryptionSweepWorkflowID    = "v1:organization-encryption-sweep"
	organizationEncryptionSweepInterval      = time.Hour
	organizationEncryptionSweepCatchup       = organizationEncryptionSweepInterval - time.Second
	organizationEncryptionSweepRunTimeout    = 55 * time.Minute
	organizationEncryptionSweepStartToClose  = 25 * time.Minute
	OrganizationEncryptionSweepActivityName  = "SweepOrganizationEncryption"
	organizationEncryptionSweepCheckInterval = time.Hour

	// organizationEncryptionSweepCheckBudget stops starting new probes so the
	// last one (bounded at a minute) and the key sweep fit StartToClose.
	organizationEncryptionSweepCheckBudget = 20 * time.Minute

	// organizationEncryptionSweepPageSize bounds the organizations probed and
	// the managed keys disabled per run.
	organizationEncryptionSweepPageSize = 500
)

// OrganizationEncryptionSweeper probes organization key encryption keys and
// disables managed ones nothing depends on.
type OrganizationEncryptionSweeper interface {
	CheckDue(ctx context.Context, interval time.Duration, pageSize int32, budget time.Duration) (int, error)
	SweepManagedKeys(ctx context.Context, pageSize int32) (int, error)
}

// OrganizationEncryptionSweepResult reports one run.
type OrganizationEncryptionSweepResult struct {
	Checked  int
	Disabled int
}

func OrganizationEncryptionSweepWorkflow(ctx workflow.Context) (OrganizationEncryptionSweepResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: organizationEncryptionSweepStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})

	var result OrganizationEncryptionSweepResult
	if err := workflow.ExecuteActivity(ctx, OrganizationEncryptionSweepActivityName).Get(ctx, &result); err != nil {
		return OrganizationEncryptionSweepResult{Checked: 0, Disabled: 0}, fmt.Errorf("sweep organization encryption: %w", err)
	}

	return result, nil
}

// OrganizationEncryptionSweepActivities runs the sweep. A worker without a
// sweeper (organization encryption not configured) sweeps nothing.
type OrganizationEncryptionSweepActivities struct {
	logger  *slog.Logger
	sweeper OrganizationEncryptionSweeper
}

func NewOrganizationEncryptionSweepActivities(logger *slog.Logger, sweeper OrganizationEncryptionSweeper) *OrganizationEncryptionSweepActivities {
	return &OrganizationEncryptionSweepActivities{logger: logger, sweeper: sweeper}
}

// Sweep probes due organizations, then disables unused managed keys. It fails,
// so Temporal retries, only when neither half made progress.
func (a *OrganizationEncryptionSweepActivities) Sweep(ctx context.Context) (OrganizationEncryptionSweepResult, error) {
	if a.sweeper == nil {
		return OrganizationEncryptionSweepResult{Checked: 0, Disabled: 0}, nil
	}

	checked, checkErr := a.sweeper.CheckDue(ctx, organizationEncryptionSweepCheckInterval, organizationEncryptionSweepPageSize, organizationEncryptionSweepCheckBudget)
	disabled, sweepErr := a.sweeper.SweepManagedKeys(ctx, organizationEncryptionSweepPageSize)
	result := OrganizationEncryptionSweepResult{Checked: checked, Disabled: disabled}

	if checked > 0 || disabled > 0 {
		a.logger.InfoContext(ctx, "swept organization encryption", attr.SlogValueInt(checked+disabled))
	}

	if err := errors.Join(checkErr, sweepErr); err != nil {
		if checked == 0 && disabled == 0 {
			return result, fmt.Errorf("sweep organization encryption: %w", err)
		}
		a.logger.ErrorContext(ctx, "organization encryption sweep partly failed", attr.SlogError(err))
	}

	return result, nil
}

func organizationEncryptionSweepScheduleID(queue string) string {
	return fmt.Sprintf("v1:organization-encryption-sweep:%s", queue)
}

func AddOrganizationEncryptionSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildOrganizationEncryptionSweepScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create organization encryption sweep schedule: %w", err)
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
			setScheduleCatchup(&schedule, organizationEncryptionSweepCatchup)
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update organization encryption sweep schedule: %w", err)
	}

	return nil
}

func buildOrganizationEncryptionSweepScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	queue := string(temporalEnv.Queue())

	return client.ScheduleOptions{
		CatchupWindow: organizationEncryptionSweepCatchup,
		ID:            organizationEncryptionSweepScheduleID(queue),
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: organizationEncryptionSweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 fmt.Sprintf("%s:%s", organizationEncryptionSweepWorkflowID, queue),
			Workflow:           OrganizationEncryptionSweepWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: organizationEncryptionSweepRunTimeout,
		},
	}
}
