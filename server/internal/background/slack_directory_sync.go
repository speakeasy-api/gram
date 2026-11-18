package background

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/constants"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Temporal actions/month ≈ 2N + R + 2,880 + 2,880·W, per namespace, where N is
// connects and manual syncs, R is activity retries and W is connected workspaces.
// The 30-minute sweep costs one start and one activity per tick (2,880) and
// starts one sync workflow plus one activity per due workspace. Scales with
// connected workspaces; at 50 workspaces that is ~150k actions/month.
// Directory pages and profiles stay in the activity, never in workflow history.
func SlackDirectorySyncWorkflow(ctx workflow.Context, input slackdirectoryconnections.SyncInput) error {
	input.StartedAt = workflow.Now(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute, ScheduleToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout: time.Minute,
		RetryPolicy:      &temporal.RetryPolicy{InitialInterval: 30 * time.Second, BackoffCoefficient: 2, MaximumInterval: 5 * time.Minute, MaximumAttempts: 5},
	})
	var a *slackDirectoryActivities
	if err := workflow.ExecuteActivity(ctx, a.SyncSlackDirectory, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("sync Slack directory: %w", err)
	}
	return nil
}

type slackDirectoryActivities struct {
	sync *slackdirectoryconnections.DirectorySync
	db   *pgxpool.Pool
}

func newSlackDirectoryActivities(db *pgxpool.Pool, enc *encryption.Client, httpClient *guardian.HTTPClient, refresher slackdirectoryconnections.TokenRefresher) *slackDirectoryActivities {
	return &slackDirectoryActivities{db: db, sync: slackdirectoryconnections.NewDirectorySync(db, enc, slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient("", httpClient)), audit.NewLogger(), refresher)}
}

const (
	slackDirectorySweepInterval = 30 * time.Minute
	// Skip workspaces synced recently, manually or by the previous tick.
	slackDirectorySweepDueAfter = 25 * time.Minute
	// Bounds child starts per tick; later workspaces are picked up by the next tick.
	slackDirectorySweepMaxWorkspaces = 100
	// A workspace whose latest sync failed is retried by the sweep at most this often.
	slackDirectorySweepFailureBackoff = 6 * time.Hour
)

func slackDirectorySweepScheduleID(queue tenv.TaskQueueName) string {
	return "v1:slack-directory-sweep:" + string(queue)
}

// SlackDirectorySweepWorkflow starts a sync for every connected workspace that is due.
func SlackDirectorySweepWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumAttempts: 3},
	})
	var a *slackDirectoryActivities
	var due []slackdirectoryconnections.SyncInput
	if err := workflow.ExecuteActivity(activityCtx, a.ListDueSlackDirectories).Get(activityCtx, &due); err != nil {
		return fmt.Errorf("list due Slack directories: %w", err)
	}
	queue := tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName)
	for _, input := range due {
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			// Same ID as a manual sync, so a sync already running for this generation is left alone.
			WorkflowID:               slackDirectoryWorkflowID(queue, input.ConnectionID, input.Generation),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowExecutionTimeout: 2*time.Hour + time.Minute,
			ParentClosePolicy:        enums.PARENT_CLOSE_POLICY_ABANDON,
		})
		child := workflow.ExecuteChildWorkflow(childCtx, SlackDirectorySyncWorkflow, input)
		if err := child.GetChildWorkflowExecution().Get(ctx, nil); err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
			workflow.GetLogger(ctx).Warn("start scheduled Slack directory sync failed", "connection_id", input.ConnectionID.String(), "error", err.Error())
		}
	}
	return nil
}

func (a *slackDirectoryActivities) ListDueSlackDirectories(ctx context.Context) ([]slackdirectoryconnections.SyncInput, error) {
	rows, err := slackrepo.New(a.db).ListDueSlackDirectorySyncs(ctx, slackrepo.ListDueSlackDirectorySyncsParams{
		ExcludedOrganizationID: constants.DemoOrganizationID,
		StartedBefore:          pgtype.Timestamptz{Time: time.Now().Add(-slackDirectorySweepDueAfter), Valid: true, InfinityModifier: pgtype.Finite},
		FailedAfter:            pgtype.Timestamptz{Time: time.Now().Add(-slackDirectorySweepFailureBackoff), Valid: true, InfinityModifier: pgtype.Finite},
		MaxRows:                slackDirectorySweepMaxWorkspaces,
	})
	if err != nil {
		return nil, fmt.Errorf("list due Slack directories: %w", err)
	}
	due := make([]slackdirectoryconnections.SyncInput, 0, len(rows))
	for _, row := range rows {
		due = append(due, slackdirectoryconnections.SyncInput{OrganizationID: row.OrganizationID, ConnectionID: row.ID, Generation: row.Generation, ActorID: "", StartedAt: time.Time{}})
	}
	return due, nil
}

// AddSlackDirectorySweepSchedule creates the queue-scoped schedule; an existing one keeps its spec and any pause.
func AddSlackDirectorySweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	queue := temporalEnv.Queue()
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		ID:            slackDirectorySweepScheduleID(queue),
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: slackDirectorySweepInterval - time.Second,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: slackDirectorySweepInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 "v1:slack-directory-sweep-run:" + string(queue),
			Workflow:           SlackDirectorySweepWorkflow,
			TaskQueue:          string(queue),
			WorkflowRunTimeout: 10 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("create Slack directory sweep schedule: %w", err)
	}
	return nil
}

func (a *slackDirectoryActivities) SyncSlackDirectory(ctx context.Context, input slackdirectoryconnections.SyncInput) error {
	var mu sync.Mutex
	progress := slackdirectoryconnections.SyncProgress{Phase: "fetching", Pages: 0, Members: 0, ExcludedExternal: 0, Bots: 0}
	report := func(p slackdirectoryconnections.SyncProgress) {
		mu.Lock()
		progress = p
		mu.Unlock()
		activity.RecordHeartbeat(ctx, p)
	}
	// Keep cancellation live while Slack asks us to wait and while publishing.
	stop := make(chan struct{})
	done := make(chan struct{})
	defer func() { close(stop); <-done }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				p := progress
				mu.Unlock()
				activity.RecordHeartbeat(ctx, p)
			}
		}
	}()
	err := a.sync.Run(ctx, input, report)
	return slackDirectoryActivityError(err, input.StartedAt, time.Now())
}

func slackDirectoryActivityError(err error, started, now time.Time) error {
	if syncErr, ok := errors.AsType[*slackdirectoryconnections.SyncError](err); ok && !syncErr.Retryable {
		return temporal.NewNonRetryableApplicationError(syncErr.Code, "SlackDirectorySync", nil)
	}
	if syncErr, ok := errors.AsType[*slackdirectoryconnections.SyncError](err); ok && syncErr.RetryAfter > 0 {
		// Leave room for a full attempt within the workflow's two-hour activity budget.
		if now.Add(syncErr.RetryAfter + 30*time.Minute).After(started.Add(2 * time.Hour)) {
			return temporal.NewNonRetryableApplicationError(syncErr.Code, "SlackDirectorySync", nil)
		}
		return temporal.NewApplicationErrorWithOptions(syncErr.Code, "SlackDirectorySync", temporal.ApplicationErrorOptions{NextRetryDelay: syncErr.RetryAfter})
	}
	if err != nil {
		return fmt.Errorf("run Slack directory sync: %w", err)
	}
	return nil
}

type slackDirectoryScheduler struct{ env *tenv.Environment }

func NewSlackDirectorySyncScheduler(env *tenv.Environment) slackdirectoryconnections.SyncScheduler {
	return &slackDirectoryScheduler{env: env}
}

func slackDirectoryWorkflowID(queue tenv.TaskQueueName, id, generation uuid.UUID) string {
	return "v1:slack-directory-sync:" + string(queue) + ":" + id.String() + ":" + generation.String()
}

func (s *slackDirectoryScheduler) Start(ctx context.Context, input slackdirectoryconnections.SyncInput) error {
	if s.env == nil {
		return errors.New("slack directory sync is not configured")
	}
	_, err := s.env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: slackDirectoryWorkflowID(s.env.Queue(), input.ConnectionID, input.Generation), TaskQueue: string(s.env.Queue()),
		WorkflowExecutionTimeout: 2*time.Hour + time.Minute,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}, SlackDirectorySyncWorkflow, input)
	if err != nil {
		return fmt.Errorf("start Slack directory sync: %w", err)
	}
	return nil
}

func (s *slackDirectoryScheduler) State(ctx context.Context, id, generation uuid.UUID) (slackdirectoryconnections.SyncState, error) {
	state := slackdirectoryconnections.SyncState{Status: "idle", Progress: slackdirectoryconnections.SyncProgress{Phase: "", Pages: 0, Members: 0, ExcludedExternal: 0, Bots: 0}}
	if s.env == nil {
		state.Status = "unknown"
		return state, errors.New("slack directory sync is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	execution, err := s.env.Client().DescribeWorkflowExecution(ctx, slackDirectoryWorkflowID(s.env.Queue(), id, generation), "")
	if _, missing := errors.AsType[*serviceerror.NotFound](err); missing {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("describe Slack sync: %w", err)
	}
	switch execution.WorkflowExecutionInfo.GetStatus() {
	case enums.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED, enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW, enums.WORKFLOW_EXECUTION_STATUS_PAUSED:
		state.Status = "unknown"
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		state.Status = "idle"
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		state.Status = "queued"
		for _, pending := range execution.PendingActivities {
			state.Status = "running"
			if pending.GetAttempt() > 1 {
				state.Status = "retrying"
			}
			if pending.HeartbeatDetails != nil {
				// Only our bounded progress structure is exposed. Failure messages stay private.
				_ = converter.GetDefaultDataConverter().FromPayloads(pending.HeartbeatDetails, &state.Progress)
			}
		}
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED, enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, enums.WORKFLOW_EXECUTION_STATUS_CANCELED, enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		state.Status = "failed"
	}
	return state, nil
}
