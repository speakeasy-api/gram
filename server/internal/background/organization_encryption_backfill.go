package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The organization encryption backfill moves organizations' values from the
// platform key to their own keys in the background. Each hour one activity
// first resumes clears that never finished and sweeps finalized organizations
// for platform-key copies that appeared after clearing (SQL only, no KMS), then
// backfills and verifies the dual_write organizations that are due. The work
// lists come from SQL and the activity loops over them itself; it starts no
// workflow per organization. Backfill candidates are read a page at a time,
// least recently attempted first, and each is checked and backfilled before
// the next, with a heartbeat between them, so work starts without scanning
// every organization first. Organizations not reached within the budget are
// first in line next hour; stalled clears are taken in random order, since a
// failed clear records no attempt.
//
// An operator can also start one organization's run on demand through
// StartOrganizationEncryptionBackfill: a backfill in dual_write, or finishing
// the clear right after finalize in org_kms. Backfill runs claim the
// transition row, so an on-demand run and the hourly run never backfill the
// same organization at once; two clears may overlap, which is safe.
//
// Temporal actions/month: ~1,440 per namespace (one schedule start and one
// activity per hour, plus at most one retry per failed run), plus activity
// heartbeats, which the SDK throttles to at most about one a minute while the
// activity runs: a few per run at tens of organizations, at most ~25 for a run
// that uses its whole budget. On-demand runs, including the one each finalize
// starts, add 2 to 4 each. Fixed: it
// doesn't scale with organizations beyond those heartbeats.
const (
	organizationEncryptionBackfillWorkflowID    = "v1:organization-encryption-backfill"
	organizationEncryptionBackfillInterval      = time.Hour
	organizationEncryptionBackfillCatchup       = organizationEncryptionBackfillInterval - time.Second
	organizationEncryptionBackfillRunTimeout    = 55 * time.Minute
	organizationEncryptionBackfillStartToClose  = 25 * time.Minute
	OrganizationEncryptionBackfillActivityName  = "BackfillOrganizationEncryption"
	organizationEncryptionBackfillMaxAttempts   = 2
	organizationEncryptionBackfillRetryInterval = 30 * time.Second

	// organizationEncryptionBackfillItemTimeout bounds one item of work: a
	// Runner run (at most orgencryption.MaxRunDuration) or a resumed clear,
	// which waits a one-minute settle delay after each of at most five
	// passes.
	organizationEncryptionBackfillItemTimeout = 7 * time.Minute

	// organizationEncryptionBackfillHeartbeatTimeout is how long Temporal
	// waits between heartbeats before treating the worker as gone. The
	// activity heartbeats before each item, so the gap is at most one item.
	// It exceeds orgencryption.RunLease, so when a worker dies holding a
	// claim, the claim has expired by the time the retry reaches that
	// organization.
	organizationEncryptionBackfillHeartbeatTimeout = organizationEncryptionBackfillItemTimeout + time.Minute

	// organizationEncryptionBackfillBudget is how long the activity keeps
	// starting new items, so the last one finishes inside StartToClose.
	organizationEncryptionBackfillBudget = organizationEncryptionBackfillStartToClose - organizationEncryptionBackfillItemTimeout - time.Minute

	// organizationEncryptionBackfillMaxResumedClears bounds the stalled
	// clears one run resumes. Each takes at least one settle delay, and a
	// stalled clear is rare, so two keep the budget for backfills.
	organizationEncryptionBackfillMaxResumedClears = 2

	OrganizationEncryptionBackfillOrganizationActivityName = "BackfillOrganizationEncryptionForOrganization"

	// organizationEncryptionBackfillOrganizationStartToClose fits one item of
	// work, a Runner run or a clear finished after finalize, and its
	// bookkeeping.
	organizationEncryptionBackfillOrganizationStartToClose = organizationEncryptionBackfillItemTimeout + time.Minute

	// organizationEncryptionBackfillOrganizationMaxAttempts retries an
	// on-demand run through a short key or database outage.
	organizationEncryptionBackfillOrganizationMaxAttempts = 3

	// organizationEncryptionBackfillOrganizationRetryInterval is the wait
	// before the first retry of an on-demand run. With StartToClose it puts
	// the retry at least orgencryption.RunLease after any claim the failed
	// attempt made, which lands within orgencryption.ClaimTimeout of the run
	// starting, so a retry after a worker died finds the claim expired
	// instead of failing as already running.
	organizationEncryptionBackfillOrganizationRetryInterval = time.Minute

	// organizationEncryptionBackfillOrganizationRunTimeout covers every
	// attempt of the on-demand activity and the backoff between them.
	organizationEncryptionBackfillOrganizationRunTimeout = 30 * time.Minute

	// OrganizationEncryptionBackfillRunningErrorType is the application
	// error type an on-demand run fails with while another run holds the
	// organization's claim.
	OrganizationEncryptionBackfillRunningErrorType = "OrganizationEncryptionBackfillRunning"

	// OrganizationEncryptionBackfillRejectedErrorType is the application
	// error type an on-demand run fails with when it cannot run at all: the
	// use case is unknown or has no copy stores, the organization is in
	// platform_key or finished clearing in org_kms, or this worker has no
	// organization encryption.
	OrganizationEncryptionBackfillRejectedErrorType = "OrganizationEncryptionBackfillRejected"
)

// OrganizationEncryptionMigrator is the background side of moving use cases
// to organization keys.
type OrganizationEncryptionMigrator interface {
	NextBackfillCandidates(ctx context.Context, cursor orgencryption.BackfillCursor) (orgencryption.BackfillPage, error)
	BackfillDue(ctx context.Context, transition orgencryption.Transition) (bool, error)
	Backfill(ctx context.Context, organizationID string, useCase orgencryption.UseCase) (orgencryption.RunResult, error)
	ListFinalized(ctx context.Context) ([]orgencryption.Transition, error)
	ResumeClear(ctx context.Context, organizationID string, useCase orgencryption.UseCase) error
	RunOnDemand(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) (orgencryption.RunResult, error)
	SweepStragglers(ctx context.Context, organizationID string, useCase orgencryption.UseCase) (int64, error)
}

// OrganizationEncryptionBackfillResult reports one hourly run.
type OrganizationEncryptionBackfillResult struct {
	// Backfilled counts organizations whose run completed.
	Backfilled int

	// Skipped counts organizations whose run or sweep did not start because
	// another run held the claim or the transition state moved.
	Skipped int

	// Failed counts organizations whose due check, run or sweep returned an
	// error.
	Failed int

	// ClearsResumed counts finalized organizations whose clear this run
	// finished.
	ClearsResumed int

	// Swept counts finalized organizations swept for stragglers.
	Swept int

	// StragglersCleared counts platform-key copies the sweep cleared.
	StragglersCleared int64
}

// OrganizationEncryptionBackfillOrganizationParams names one organization's
// use case to backfill on demand.
type OrganizationEncryptionBackfillOrganizationParams struct {
	// OrganizationID owns the values.
	OrganizationID string

	// UseCase is the registered use case to backfill.
	UseCase string

	// ActorPrincipal is the URN of the staff member who asked for the run,
	// so a clear it finishes is attributed to them. Empty attributes it to
	// the system.
	ActorPrincipal string

	// ActorDisplayName renders the actor in the audit log, when known.
	ActorDisplayName *string
}

func OrganizationEncryptionBackfillWorkflow(ctx workflow.Context) (OrganizationEncryptionBackfillResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: organizationEncryptionBackfillStartToClose,
		HeartbeatTimeout:    organizationEncryptionBackfillHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    organizationEncryptionBackfillMaxAttempts,
			InitialInterval:    organizationEncryptionBackfillRetryInterval,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})

	var result OrganizationEncryptionBackfillResult
	if err := workflow.ExecuteActivity(ctx, OrganizationEncryptionBackfillActivityName).Get(ctx, &result); err != nil {
		return OrganizationEncryptionBackfillResult{Backfilled: 0, Skipped: 0, Failed: 0, ClearsResumed: 0, Swept: 0, StragglersCleared: 0}, fmt.Errorf("backfill organization encryption: %w", err)
	}

	return result, nil
}

func OrganizationEncryptionBackfillOrganizationWorkflow(ctx workflow.Context, params OrganizationEncryptionBackfillOrganizationParams) (orgencryption.RunResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: organizationEncryptionBackfillOrganizationStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    organizationEncryptionBackfillOrganizationMaxAttempts,
			InitialInterval:    organizationEncryptionBackfillOrganizationRetryInterval,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
			NonRetryableErrorTypes: []string{
				OrganizationEncryptionBackfillRunningErrorType,
				OrganizationEncryptionBackfillRejectedErrorType,
			},
		},
	})

	// The activity error is returned as is: a wrapped error reaches the
	// caller as an application error of its own, hiding the activity's
	// error type, which callers match on.
	var result orgencryption.RunResult
	if err := workflow.ExecuteActivity(ctx, OrganizationEncryptionBackfillOrganizationActivityName, params).Get(ctx, &result); err != nil {
		return result, err
	}

	return result, nil
}

// OrganizationEncryptionBackfillActivities runs the backfill. A worker without
// a migrator (organization encryption not configured) does nothing on the
// hourly run and rejects on-demand runs.
type OrganizationEncryptionBackfillActivities struct {
	logger    *slog.Logger
	migrator  OrganizationEncryptionMigrator
	now       func() time.Time
	heartbeat func(ctx context.Context, details ...any)
}

func NewOrganizationEncryptionBackfillActivities(logger *slog.Logger, migrator OrganizationEncryptionMigrator) *OrganizationEncryptionBackfillActivities {
	return &OrganizationEncryptionBackfillActivities{
		logger:    logger.With(attr.SlogComponent("organization-encryption-backfill")),
		migrator:  migrator,
		now:       time.Now,
		heartbeat: activity.RecordHeartbeat,
	}
}

// Backfill resumes stalled clears and sweeps stragglers, then backfills the
// due dual_write organizations. A problem with one organization doesn't stop
// the others. It fails, so Temporal retries, only when something failed and
// nothing succeeded.
func (a *OrganizationEncryptionBackfillActivities) Backfill(ctx context.Context) (OrganizationEncryptionBackfillResult, error) {
	result := OrganizationEncryptionBackfillResult{Backfilled: 0, Skipped: 0, Failed: 0, ClearsResumed: 0, Swept: 0, StragglersCleared: 0}
	if a.migrator == nil {
		return result, nil
	}

	deadline := a.now().Add(organizationEncryptionBackfillBudget)
	var errs []error
	succeeded := false

	finalized, err := a.migrator.ListFinalized(ctx)
	if err != nil {
		errs = append(errs, err)
	}
	a.heartbeat(ctx)

	resumed := 0
	for _, transition := range finalized {
		if ctx.Err() != nil || !a.now().Before(deadline) {
			break
		}
		resumeClear := transition.PlatformCopiesClearedAt == nil
		if resumeClear && resumed >= organizationEncryptionBackfillMaxResumedClears {
			continue
		}
		a.heartbeat(ctx)

		logger := a.logger.With(attr.SlogOrganizationID(transition.OrganizationID), attr.SlogOrgEncryptionUseCase(transition.UseCase))
		itemCtx, cancel := context.WithTimeout(ctx, organizationEncryptionBackfillItemTimeout)

		if resumeClear {
			resumed++
			err := a.migrator.ResumeClear(itemCtx, transition.OrganizationID, transition.UseCase)
			cancel()
			if err != nil {
				result.Failed++
				errs = append(errs, err)
				logger.ErrorContext(ctx, "resume clearing platform-key copies", attr.SlogError(err))
				continue
			}
			result.ClearsResumed++
			succeeded = true
			continue
		}

		cleared, err := a.migrator.SweepStragglers(itemCtx, transition.OrganizationID, transition.UseCase)
		cancel()
		result.StragglersCleared += cleared
		switch {
		case errors.Is(err, orgencryption.ErrTransitionNotAllowed):
			result.Skipped++
			logger.InfoContext(ctx, "skipped sweeping platform-key copies", attr.SlogError(err))
		case err != nil:
			result.Failed++
			errs = append(errs, err)
			logger.ErrorContext(ctx, "sweep platform-key copies", attr.SlogError(err))
		default:
			result.Swept++
			succeeded = true
		}
	}

	var cursor orgencryption.BackfillCursor
backfills:
	for ctx.Err() == nil && a.now().Before(deadline) {
		page, err := a.migrator.NextBackfillCandidates(ctx, cursor)
		a.heartbeat(ctx)
		if err != nil {
			errs = append(errs, err)
			break
		}

		for _, transition := range page.Candidates {
			if ctx.Err() != nil || !a.now().Before(deadline) {
				break backfills
			}
			a.heartbeat(ctx)

			logger := a.logger.With(attr.SlogOrganizationID(transition.OrganizationID), attr.SlogOrgEncryptionUseCase(transition.UseCase))

			due, err := a.migrator.BackfillDue(ctx, transition)
			if err != nil {
				result.Failed++
				errs = append(errs, err)
				logger.ErrorContext(ctx, "check whether an organization encryption backfill is due", attr.SlogError(err))
				continue
			}
			if !due {
				continue
			}

			// The run claims within ClaimTimeout of this heartbeat, so a
			// dead worker's claim expires before Temporal retries.
			a.heartbeat(ctx)
			_, err = a.migrator.Backfill(ctx, transition.OrganizationID, transition.UseCase)
			switch {
			case errors.Is(err, orgencryption.ErrBackfillRunning), errors.Is(err, orgencryption.ErrFenced), errors.Is(err, orgencryption.ErrTransitionNotAllowed):
				result.Skipped++
				logger.InfoContext(ctx, "skipped organization encryption backfill", attr.SlogError(err))
			case err != nil:
				result.Failed++
				errs = append(errs, err)
				logger.ErrorContext(ctx, "organization encryption backfill failed", attr.SlogError(err))
			default:
				result.Backfilled++
				succeeded = true
			}
		}

		if !page.More {
			break
		}
		cursor = page.Next
	}

	if err := errors.Join(errs...); err != nil {
		if !succeeded {
			return result, fmt.Errorf("backfill organization encryption: %w", err)
		}
		a.logger.ErrorContext(ctx, "organization encryption backfill partly failed", attr.SlogError(err))
	}

	return result, nil
}

// BackfillOrganization runs one organization's use case on demand: a backfill
// and verification in dual_write, or finishing the clear in org_kms.
func (a *OrganizationEncryptionBackfillActivities) BackfillOrganization(ctx context.Context, params OrganizationEncryptionBackfillOrganizationParams) (orgencryption.RunResult, error) {
	var empty orgencryption.RunResult
	if a.migrator == nil {
		return empty, temporal.NewNonRetryableApplicationError("organization encryption is not configured on this worker", OrganizationEncryptionBackfillRejectedErrorType, nil)
	}

	useCase, err := orgencryption.ParseUseCase(params.UseCase)
	if err != nil {
		return empty, temporal.NewNonRetryableApplicationError("unknown organization encryption use case", OrganizationEncryptionBackfillRejectedErrorType, err)
	}

	itemCtx, cancel := context.WithTimeout(ctx, organizationEncryptionBackfillItemTimeout)
	defer cancel()

	actor := orgencryption.SystemActor()
	if params.ActorPrincipal != "" {
		principal, err := urn.ParsePrincipal(params.ActorPrincipal)
		if err != nil {
			return empty, temporal.NewNonRetryableApplicationError("invalid organization encryption run actor", OrganizationEncryptionBackfillRejectedErrorType, err)
		}
		actor = orgencryption.Actor{Principal: principal, DisplayName: params.ActorDisplayName}
	}

	result, err := a.migrator.RunOnDemand(itemCtx, params.OrganizationID, useCase, actor)
	switch {
	case errors.Is(err, orgencryption.ErrBackfillRunning):
		return result, temporal.NewNonRetryableApplicationError("a backfill run is already in progress for this organization and use case", OrganizationEncryptionBackfillRunningErrorType, err)
	case errors.Is(err, orgencryption.ErrTransitionNotAllowed), errors.Is(err, orgencryption.ErrUnknownUseCase), errors.Is(err, orgencryption.ErrNoCopyStores):
		return result, temporal.NewNonRetryableApplicationError("organization encryption backfill cannot run for this organization and use case", OrganizationEncryptionBackfillRejectedErrorType, err)
	case err != nil:
		return result, fmt.Errorf("backfill organization encryption: %w", err)
	}

	return result, nil
}

// OrganizationEncryptionBackfillOrganizationWorkflowID is the workflow id of
// one organization's on-demand run on queue, so a second start joins the run
// already in progress. It is scoped by queue because PR previews share the
// dev namespace with their own databases.
func OrganizationEncryptionBackfillOrganizationWorkflowID(queue string, organizationID string, useCase orgencryption.UseCase) string {
	return fmt.Sprintf("%s:%s:%s:%s", organizationEncryptionBackfillWorkflowID, queue, useCase, organizationID)
}

// StartOrganizationEncryptionBackfill starts an on-demand run for one
// organization's use case, a backfill and verification in dual_write or the
// clear after finalize in org_kms, or returns the on-demand run already in
// progress for it. The run fails with
// OrganizationEncryptionBackfillRunningErrorType while the hourly run is
// working on the same organization.
func StartOrganizationEncryptionBackfill(ctx context.Context, temporalEnv *tenv.Environment, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) (client.WorkflowRun, error) {
	if _, err := orgencryption.ParseUseCase(string(useCase)); err != nil {
		return nil, fmt.Errorf("start organization encryption backfill: %w", err)
	}

	run, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       OrganizationEncryptionBackfillOrganizationWorkflowID(string(temporalEnv.Queue()), organizationID, useCase),
		TaskQueue:                string(temporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       organizationEncryptionBackfillOrganizationRunTimeout,
	}, OrganizationEncryptionBackfillOrganizationWorkflow, OrganizationEncryptionBackfillOrganizationParams{
		OrganizationID:   organizationID,
		UseCase:          string(useCase),
		ActorPrincipal:   actor.Principal.String(),
		ActorDisplayName: actor.DisplayName,
	})
	if err != nil {
		return nil, fmt.Errorf("start organization encryption backfill workflow: %w", err)
	}

	return run, nil
}

// TemporalOrganizationEncryptionRunStarter starts on-demand organization
// encryption runs for the admin API.
type TemporalOrganizationEncryptionRunStarter struct {
	// TemporalEnv is the Temporal connection runs start on.
	TemporalEnv *tenv.Environment
}

// StartOrganizationEncryptionRun starts an on-demand run, or joins the one in
// progress, without waiting for it.
func (s *TemporalOrganizationEncryptionRunStarter) StartOrganizationEncryptionRun(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error {
	if _, err := StartOrganizationEncryptionBackfill(ctx, s.TemporalEnv, organizationID, useCase, actor); err != nil {
		return err
	}

	return nil
}

func organizationEncryptionBackfillScheduleID(queue string) string {
	return fmt.Sprintf("%s:%s", organizationEncryptionBackfillWorkflowID, queue)
}

func AddOrganizationEncryptionBackfillSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildOrganizationEncryptionBackfillScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create organization encryption backfill schedule: %w", err)
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
			setScheduleCatchup(&schedule, organizationEncryptionBackfillCatchup)
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update organization encryption backfill schedule: %w", err)
	}

	return nil
}

func buildOrganizationEncryptionBackfillScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	queue := string(temporalEnv.Queue())

	return client.ScheduleOptions{
		CatchupWindow: organizationEncryptionBackfillCatchup,
		ID:            organizationEncryptionBackfillScheduleID(queue),
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: organizationEncryptionBackfillInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 fmt.Sprintf("%s:%s", organizationEncryptionBackfillWorkflowID, queue),
			Workflow:           OrganizationEncryptionBackfillWorkflow,
			TaskQueue:          queue,
			WorkflowRunTimeout: organizationEncryptionBackfillRunTimeout,
		},
	}
}
