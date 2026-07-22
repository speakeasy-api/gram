package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// SignalSkillEfficacyRequested wakes a project's coordinator. The payload is
	// empty: the signal says only that the project has work, and what that work
	// is comes from the queue itself, so any number of them coalesce into one
	// pass.
	SignalSkillEfficacyRequested = "skill-efficacy-requested"

	// skillEfficacyMaxEnqueuePages bounds one pass's enqueue walk. A walk that
	// does not finish inside it keeps its cursor and carries on in the next pass
	// or the next run, so the bound costs progress nothing.
	skillEfficacyMaxEnqueuePages = 10
	// skillEfficacyMaxPasses bounds how many reserve-and-publish rounds one run
	// makes before handing the rest to a fresh run, which keeps the history a
	// long backlog writes bounded.
	skillEfficacyMaxPasses = 5
	// skillEfficacyMaxSweepPages bounds one sweep run's project pagination.
	skillEfficacyMaxSweepPages = 10

	// skillEfficacyPublishTimeout covers a whole claimed batch: the publication
	// bounds each evaluation at two minutes and a batch holds at most
	// MaxReservedClaimBatch of them, so this leaves room past the worst
	// sequential pass without outliving the claim lease that owns the rows.
	skillEfficacyPublishTimeout = 25 * time.Minute
	// skillEfficacyPublishHeartbeatTimeout is how long the publication may go
	// without reporting progress. The publication heartbeats once per evaluation
	// and bounds each at two minutes, so this clears the widest real gap while
	// still detecting a dead worker in a fraction of the start-to-close timeout —
	// and, because the server's cancellation only reaches a worker that
	// heartbeats, it is what stops a timed-out attempt from judging the same
	// reserved rows alongside its own retry.
	skillEfficacyPublishHeartbeatTimeout = 5 * time.Minute

	// SkillEfficacySignalCooldown is the per-project window a ThrottledSignaler
	// coalesces efficacy wakes into. A session is not scoreable until it has been
	// quiet for InactivityWindow, so a wake deferred to the trailing edge of this
	// window is still orders of magnitude ahead of the work it announces.
	SkillEfficacySignalCooldown = 30 * time.Second

	skillEfficacySweepScheduleID = "v1:skill-efficacy-sweep-schedule"
	skillEfficacySweepWorkflowID = skillEfficacySweepScheduleID + "/scheduled"
	// skillEfficacySweepInterval is how often the estate is swept for work no
	// signal ever arrived for and for reservations whose owner died.
	skillEfficacySweepInterval   = 15 * time.Minute
	skillEfficacySweepRunTimeout = 60 * time.Minute
)

// SkillEfficacyCoordinatorParams identifies the project this coordinator runs
// for and where its two walks — the enqueue walk over reconciled activations,
// the reservation's walk over pending evaluations — stopped.
//
// The cursors are workflow state rather than stored state: they are carried
// across ContinueAsNew so a walk longer than one run resumes where it left off,
// and each is reset once its walk reaches the end of its queue so the next one
// starts at the head and picks up anything written behind it.
type SkillEfficacyCoordinatorParams struct {
	ProjectID     uuid.UUID              `json:"project_id"`
	Cursor        efficacy.EnqueueCursor `json:"cursor"`
	PendingCursor efficacy.PendingCursor `json:"pending_cursor"`
}

// SkillEfficacySweepParams is where a sweep's project pagination resumes.
type SkillEfficacySweepParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
}

func skillEfficacyCoordinatorWorkflowID(projectID uuid.UUID) string {
	return fmt.Sprintf("v1:skill-efficacy:%s", projectID.String())
}

// SkillEfficacyCoordinatorWorkflow is the per-project driver of the efficacy
// pipeline. One run is a sequence of passes, and one pass is: walk the enqueue
// cursor until the pending activations are exhausted, reserve a batch against
// the organization's budget, and publish it. An empty reservation with an
// advanced cursor continues past capped candidates; one that reaches the queue
// end or makes no progress stops until the next signal or sweep. A run that
// spends its whole pass budget publishing or advancing continues as new because
// unfinished work still remains.
//
// Its id is derived from the project, so SignalWithStart makes it exactly-one
// per project: a second signal arriving while a run is live is delivered to that
// run rather than starting a competing one. Signals are drained, never counted —
// the queue is the work list — so a burst of them costs one pass. A signal that
// lands while a pass is running is drained at the end and answered with
// ContinueAsNew, which is what stops a run from completing on top of work it
// never looked at.
//
// No step here owns work it can lose. The enqueue is idempotent per scoring
// unit, a reservation that is never published is picked back up by the claim or,
// much later, by the sweeper's stale reset, and the publication is guarded on the
// scores already written.
func SkillEfficacyCoordinatorWorkflow(ctx workflow.Context, params SkillEfficacyCoordinatorParams) error {
	signalCh := workflow.GetSignalChannel(ctx, SignalSkillEfficacyRequested)

	// The signal that started this run is its own reason to work, so draining it
	// up front is what keeps the end-of-run check from continuing as new for it.
	drainSignals(signalCh)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	// Publication is the only step that calls a model, so it runs on the queue
	// sized for that work rather than competing with the estate's short
	// database activities for the main worker's slots.
	publishCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           SkillEfficacyTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName)),
		StartToCloseTimeout: skillEfficacyPublishTimeout,
		HeartbeatTimeout:    skillEfficacyPublishHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *activities.SkillEfficacyScorer
	// A run that spends all its passes on work is a run that ran out of history
	// budget, not out of work. The flag is what tells the two apart at the end:
	// the loop only clears it on the pass that found nothing left to publish.
	exhausted := false
	for range skillEfficacyMaxPasses {
		for range skillEfficacyMaxEnqueuePages {
			var page activities.EnqueueSkillEfficacyPageResult
			if err := workflow.ExecuteActivity(ctx, a.EnqueueSkillEfficacyPage, activities.EnqueueSkillEfficacyPageParams{
				ProjectID: params.ProjectID,
				Cursor:    params.Cursor,
				PageSize:  efficacy.MaxEnqueuePageSize,
			}).Get(ctx, &page); err != nil {
				return fmt.Errorf("enqueue skill efficacy page: %w", err)
			}

			params.Cursor = page.NextCursor
			if page.Exhausted {
				// The walk reached the end of the pending set. Starting the next
				// one at the head is what reaches an activation whose seen_at
				// sits behind the cursor, which a late-arriving capture writes.
				params.Cursor = efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil}
				break
			}
			if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
				return workflow.NewContinueAsNewError(ctx, SkillEfficacyCoordinatorWorkflow, params)
			}
		}

		pendingCursor := params.PendingCursor
		var reserved activities.ReserveSkillEfficacyEvaluationsResult
		if err := workflow.ExecuteActivity(ctx, a.ReserveSkillEfficacyEvaluations, activities.ReserveSkillEfficacyEvaluationsParams{
			ProjectID: params.ProjectID,
			Cursor:    params.PendingCursor,
			BatchSize: efficacy.MaxReservedClaimBatch,
		}).Get(ctx, &reserved); err != nil {
			return fmt.Errorf("reserve skill efficacy evaluations: %w", err)
		}

		// The reservation walks a bounded slice of the queue, so where it stopped
		// is what the next pass resumes from. It reports the head once it has
		// walked to the end, which is what starts the walk over.
		params.PendingCursor = reserved.NextCursor

		ids := reserved.IDs
		if len(ids) == 0 {
			// Claim any reservation an earlier owner never published before deciding
			// whether this empty result exhausted the queue or merely advanced past
			// capped candidates.
			var claimed activities.SkillEfficacyBatch
			if err := workflow.ExecuteActivity(ctx, a.LoadReservedSkillEfficacyEvaluations, activities.LoadReservedSkillEfficacyEvaluationsParams{
				ProjectID: params.ProjectID,
				BatchSize: efficacy.MaxReservedClaimBatch,
			}).Get(ctx, &claimed); err != nil {
				return fmt.Errorf("load reserved skill efficacy evaluations: %w", err)
			}
			ids = claimed.IDs
		}

		if len(ids) == 0 {
			if reserved.NextCursor != (efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}) && reserved.NextCursor != pendingCursor {
				continue
			}
			exhausted = true
			break
		}

		var published activities.PublishSkillEfficacyBatchResult
		if err := workflow.ExecuteActivity(publishCtx, a.PublishSkillEfficacyBatch, activities.PublishSkillEfficacyBatchParams{
			ProjectID: params.ProjectID,
			IDs:       ids,
		}).Get(ctx, &published); err != nil {
			return fmt.Errorf("publish skill efficacy batch: %w", err)
		}

		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, SkillEfficacyCoordinatorWorkflow, params)
		}
	}

	// Every pass published a batch or advanced over capped candidates, so the run
	// stopped on its history budget rather than queue exhaustion.
	if !exhausted {
		return workflow.NewContinueAsNewError(ctx, SkillEfficacyCoordinatorWorkflow, params)
	}

	// A signal that arrived while the passes ran describes work this run may
	// never have looked at, so it is answered with another run rather than lost
	// to completion.
	if drainSignals(signalCh) {
		return workflow.NewContinueAsNewError(ctx, SkillEfficacyCoordinatorWorkflow, params)
	}

	return nil
}

// SkillEfficacySweepWorkflow is the estate-wide safety net behind the
// coordinators. Every tick it pages over the projects holding unfinished
// efficacy work, returns their abandoned reservations to the queue, and signals
// their coordinator.
//
// It is what makes a lost signal survivable: the pipeline is driven by signals,
// and a producer that failed to send one, or a coordinator that died between
// reserving and publishing, leaves work no one is coming back for. The discovery
// read is what finds it, and it names stale reservations explicitly because a
// project whose only remaining work is one would otherwise be invisible.
//
// A project that fails to recover does not fail the sweep: the tick after this
// one visits it again, and failing the run would cost every project behind it in
// the page.
func SkillEfficacySweepWorkflow(ctx workflow.Context, params SkillEfficacySweepParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	logger := workflow.GetLogger(ctx)
	var a *activities.SkillEfficacyScorer
	afterProjectID := params.AfterProjectID
	for range skillEfficacyMaxSweepPages {
		var projects []efficacy.PendingWorkProject
		if err := workflow.ExecuteActivity(ctx, a.ListSkillEfficacyProjects, activities.ListSkillEfficacyProjectsParams{
			AfterProjectID: afterProjectID,
			PageLimit:      efficacy.MaxSweepProjectPage,
		}).Get(ctx, &projects); err != nil {
			return fmt.Errorf("list projects with pending skill efficacy work: %w", err)
		}

		for _, project := range projects {
			// Only a project the discovery read found a stale reservation on is
			// reset; for every other one the UPDATE would match no row. The reset
			// runs before the signal so the coordinator this wakes already sees the
			// recovered rows as pending.
			if project.HasStale {
				var reset activities.ResetStaleSkillEfficacyReservationsResult
				if err := workflow.ExecuteActivity(ctx, a.ResetStaleSkillEfficacyReservations, activities.ResetStaleSkillEfficacyReservationsParams{
					ProjectID: project.ProjectID,
				}).Get(ctx, &reset); err != nil {
					logger.Error("reset stale skill efficacy reservations failed", "project_id", project.ProjectID.String(), "error", err.Error())
					continue
				}
			}

			// The wake is what every discovered project is here for: a stale-only
			// project needs its recovered rows picked up, and a pending-only one
			// needs the pass no signal ever asked for.
			if err := workflow.ExecuteActivity(ctx, a.SignalSkillEfficacyCoordinator, activities.SignalSkillEfficacyCoordinatorParams{
				ProjectID: project.ProjectID,
			}).Get(ctx, nil); err != nil {
				logger.Error("signal skill efficacy coordinator failed", "project_id", project.ProjectID.String(), "error", err.Error())
			}
		}

		if len(projects) < int(efficacy.MaxSweepProjectPage) {
			return nil
		}

		afterProjectID = projects[len(projects)-1].ProjectID
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, SkillEfficacySweepWorkflow, SkillEfficacySweepParams{
				AfterProjectID: afterProjectID,
			})
		}
	}

	return workflow.NewContinueAsNewError(ctx, SkillEfficacySweepWorkflow, SkillEfficacySweepParams{
		AfterProjectID: afterProjectID,
	})
}

func AddSkillEfficacySweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: skillEfficacySweepInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:       skillEfficacySweepWorkflowID,
		Workflow: SkillEfficacySweepWorkflow,
		Args: []any{SkillEfficacySweepParams{
			AfterProjectID: uuid.Nil,
		}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: skillEfficacySweepRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID: skillEfficacySweepScheduleID,
		// A tick that overlaps the previous one would re-signal projects the
		// running sweep is still working through, so a slow sweep skips rather
		// than doubles.
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, skillEfficacySweepScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update skill efficacy sweep schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create skill efficacy sweep schedule: %w", err)
	}

	return nil
}

// TemporalSkillEfficacySignaler starts or wakes a project's coordinator through
// Temporal.
type TemporalSkillEfficacySignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

var _ efficacy.Signaler = (*TemporalSkillEfficacySignaler)(nil)

// Signal delivers the request to the project's coordinator, starting it when no
// run is live. The workflow id is the project's, so a signal raised while a run
// is in flight joins that run instead of starting a second one.
func (s *TemporalSkillEfficacySignaler) Signal(ctx context.Context, projectID uuid.UUID) error {
	workflowID := skillEfficacyCoordinatorWorkflowID(projectID)

	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		workflowID,
		SignalSkillEfficacyRequested,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    workflowID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		SkillEfficacyCoordinatorWorkflow,
		SkillEfficacyCoordinatorParams{
			ProjectID:     projectID,
			Cursor:        efficacy.EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil},
			PendingCursor: efficacy.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil},
		},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start skill efficacy coordinator: %w", err)
	}

	s.Logger.DebugContext(ctx, "skill efficacy coordinator signal sent",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(workflowID),
	)

	return nil
}
