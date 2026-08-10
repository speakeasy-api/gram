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
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// SignalChatAnalysisRequested wakes a project's coordinator. The payload is
	// empty: the signal says only that the project has work, and what that work
	// is comes from the queue itself, so any number of them coalesce into one
	// pass.
	SignalChatAnalysisRequested = "chat-analysis-requested"

	// chatAnalysisMaxEnqueuePages bounds one pass's enqueue walk. A walk that
	// does not finish inside it keeps its cursor and carries on in the next pass
	// or the next run.
	chatAnalysisMaxEnqueuePages = 10
	// chatAnalysisMaxPasses bounds how many reserve-and-publish rounds one run
	// makes before handing the rest to a fresh run.
	chatAnalysisMaxPasses = 5
	// chatAnalysisMaxSweepPages bounds one sweep run's project pagination.
	chatAnalysisMaxSweepPages = 10

	// chatAnalysisPublishTimeout covers a whole claimed batch: the publication
	// bounds each evaluation at two minutes and a batch holds at most
	// MaxReservedClaimBatch of them, so this leaves room past the worst
	// sequential pass without outliving the claim lease that owns the rows.
	chatAnalysisPublishTimeout = 25 * time.Minute
	// chatAnalysisPublishHeartbeatTimeout is how long the publication may go
	// without reporting progress. The publication heartbeats once per evaluation
	// and bounds each at two minutes.
	chatAnalysisPublishHeartbeatTimeout = 5 * time.Minute

	// ChatAnalysisSignalCooldown is the per-project window a ThrottledSignaler
	// coalesces analysis wakes into. A session is not analyzable until it has
	// been quiet for InactivityWindow, so a wake deferred to the trailing edge
	// of this window is still orders of magnitude ahead of the work it
	// announces.
	ChatAnalysisSignalCooldown = 30 * time.Second

	chatAnalysisSweepScheduleID = "v1:chat-analysis-sweep-schedule"
	chatAnalysisSweepWorkflowID = chatAnalysisSweepScheduleID + "/scheduled"
	// chatAnalysisSweepInterval is how often the estate is swept for work no
	// signal ever arrived for and for reservations whose owner died.
	chatAnalysisSweepInterval   = 15 * time.Minute
	chatAnalysisSweepRunTimeout = 60 * time.Minute
)

// ChatAnalysisCoordinatorParams identifies the project this coordinator runs
// for and where its two walks — the enqueue walk over the project's chats, the
// reservation's walk over pending evaluations — stopped. The cursors are
// workflow state: carried across ContinueAsNew so a walk longer than one run
// resumes where it left off, and each reset once its walk reaches the end of
// its queue.
type ChatAnalysisCoordinatorParams struct {
	ProjectID     uuid.UUID              `json:"project_id"`
	Cursor        analysis.EnqueueCursor `json:"cursor"`
	PendingCursor analysis.PendingCursor `json:"pending_cursor"`
}

// ChatAnalysisSweepParams is where a sweep's project pagination resumes.
type ChatAnalysisSweepParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
}

func chatAnalysisCoordinatorWorkflowID(projectID uuid.UUID) string {
	return fmt.Sprintf("v1:chat-analysis:%s", projectID.String())
}

// ChatAnalysisCoordinatorWorkflow is the per-project driver of the chat
// analysis pipeline, a structural twin of SkillEfficacyCoordinatorWorkflow.
// One run is a sequence of passes, and one pass is: walk the enqueue cursor
// until the project's candidate chats are exhausted, reserve a batch against
// the organization's per-judge budgets, and publish it.
//
// Its id is derived from the project, so SignalWithStart makes it exactly-one
// per project: a second signal arriving while a run is live is delivered to
// that run rather than starting a competing one. Signals are drained, never
// counted — the queue is the work list — so a burst of them costs one pass.
func ChatAnalysisCoordinatorWorkflow(ctx workflow.Context, params ChatAnalysisCoordinatorParams) error {
	signalCh := workflow.GetSignalChannel(ctx, SignalChatAnalysisRequested)

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
	// sized for that work. The queue is shared with skill efficacy: both
	// pipelines' publications are judge conversations, and one per-pod cap
	// should bound them together.
	publishCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           SkillEfficacyTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName)),
		StartToCloseTimeout: chatAnalysisPublishTimeout,
		HeartbeatTimeout:    chatAnalysisPublishHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *activities.ChatAnalysisScorer
	// A run that spends all its passes on work ran out of history budget, not
	// out of work. The flag tells the two apart at the end.
	exhausted := false
	for range chatAnalysisMaxPasses {
		for range chatAnalysisMaxEnqueuePages {
			var page activities.EnqueueChatAnalysisPageResult
			if err := workflow.ExecuteActivity(ctx, a.EnqueueChatAnalysisPage, activities.EnqueueChatAnalysisPageParams{
				ProjectID: params.ProjectID,
				Cursor:    params.Cursor,
				PageSize:  analysis.MaxEnqueuePageSize,
			}).Get(ctx, &page); err != nil {
				return fmt.Errorf("enqueue chat analysis page: %w", err)
			}

			params.Cursor = page.NextCursor
			if page.Exhausted {
				// The walk reached the end of the candidate set. Starting the next
				// one at the head is what reaches a chat that went quiet behind the
				// cursor.
				params.Cursor = analysis.EnqueueCursor{CreatedAt: time.Time{}, ID: uuid.Nil}
				break
			}
			if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
				return workflow.NewContinueAsNewError(ctx, ChatAnalysisCoordinatorWorkflow, params)
			}
		}

		pendingCursor := params.PendingCursor
		var reserved activities.ReserveChatAnalysisEvaluationsResult
		if err := workflow.ExecuteActivity(ctx, a.ReserveChatAnalysisEvaluations, activities.ReserveChatAnalysisEvaluationsParams{
			ProjectID: params.ProjectID,
			Cursor:    params.PendingCursor,
			BatchSize: analysis.MaxReservedClaimBatch,
		}).Get(ctx, &reserved); err != nil {
			return fmt.Errorf("reserve chat analysis evaluations: %w", err)
		}

		// The reservation walks a bounded slice of the queue, so where it stopped
		// is what the next pass resumes from.
		params.PendingCursor = reserved.NextCursor

		ids := reserved.IDs
		if len(ids) == 0 {
			// Claim any reservation an earlier owner never published before
			// deciding whether this empty result exhausted the queue or merely
			// advanced past capped candidates.
			var claimed activities.ChatAnalysisBatch
			if err := workflow.ExecuteActivity(ctx, a.LoadReservedChatAnalysisEvaluations, activities.LoadReservedChatAnalysisEvaluationsParams{
				ProjectID: params.ProjectID,
				BatchSize: analysis.MaxReservedClaimBatch,
			}).Get(ctx, &claimed); err != nil {
				return fmt.Errorf("load reserved chat analysis evaluations: %w", err)
			}
			ids = claimed.IDs
		}

		if len(ids) == 0 {
			if reserved.NextCursor != (analysis.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil}) && reserved.NextCursor != pendingCursor {
				continue
			}
			exhausted = true
			break
		}

		var published activities.PublishChatAnalysisBatchResult
		if err := workflow.ExecuteActivity(publishCtx, a.PublishChatAnalysisBatch, activities.PublishChatAnalysisBatchParams{
			ProjectID: params.ProjectID,
			IDs:       ids,
		}).Get(ctx, &published); err != nil {
			return fmt.Errorf("publish chat analysis batch: %w", err)
		}

		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, ChatAnalysisCoordinatorWorkflow, params)
		}
	}

	// Every pass published a batch or advanced over capped candidates, so the
	// run stopped on its history budget rather than queue exhaustion.
	if !exhausted {
		return workflow.NewContinueAsNewError(ctx, ChatAnalysisCoordinatorWorkflow, params)
	}

	// A signal that arrived while the passes ran describes work this run may
	// never have looked at, so it is answered with another run rather than lost
	// to completion.
	if drainSignals(signalCh) {
		return workflow.NewContinueAsNewError(ctx, ChatAnalysisCoordinatorWorkflow, params)
	}

	return nil
}

// ChatAnalysisSweepWorkflow is the estate-wide safety net behind the
// coordinators. Every tick it pages over the projects holding unfinished
// analysis work, returns their abandoned reservations to the queue, and
// signals their coordinator. It is what makes a lost signal survivable.
//
// A project that fails to recover does not fail the sweep: the tick after this
// one visits it again.
func ChatAnalysisSweepWorkflow(ctx workflow.Context, params ChatAnalysisSweepParams) error {
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
	var a *activities.ChatAnalysisScorer
	afterProjectID := params.AfterProjectID
	for range chatAnalysisMaxSweepPages {
		var projects []analysis.PendingWorkProject
		if err := workflow.ExecuteActivity(ctx, a.ListChatAnalysisProjects, activities.ListChatAnalysisProjectsParams{
			AfterProjectID: afterProjectID,
			PageLimit:      analysis.MaxSweepProjectPage,
		}).Get(ctx, &projects); err != nil {
			return fmt.Errorf("list projects with pending chat analysis work: %w", err)
		}

		for _, project := range projects {
			// Only a project the discovery read found a stale reservation on is
			// reset. The reset runs before the signal so the coordinator this
			// wakes already sees the recovered rows as pending.
			if project.HasStale {
				var reset activities.ResetStaleChatAnalysisReservationsResult
				if err := workflow.ExecuteActivity(ctx, a.ResetStaleChatAnalysisReservations, activities.ResetStaleChatAnalysisReservationsParams{
					ProjectID: project.ProjectID,
				}).Get(ctx, &reset); err != nil {
					logger.Error("reset stale chat analysis reservations failed", "project_id", project.ProjectID.String(), "error", err.Error())
					continue
				}
			}

			if err := workflow.ExecuteActivity(ctx, a.SignalChatAnalysisCoordinator, activities.SignalChatAnalysisCoordinatorParams{
				ProjectID: project.ProjectID,
			}).Get(ctx, nil); err != nil {
				logger.Error("signal chat analysis coordinator failed", "project_id", project.ProjectID.String(), "error", err.Error())
			}
		}

		if len(projects) < int(analysis.MaxSweepProjectPage) {
			return nil
		}

		afterProjectID = projects[len(projects)-1].ProjectID
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, ChatAnalysisSweepWorkflow, ChatAnalysisSweepParams{
				AfterProjectID: afterProjectID,
			})
		}
	}

	return workflow.NewContinueAsNewError(ctx, ChatAnalysisSweepWorkflow, ChatAnalysisSweepParams{
		AfterProjectID: afterProjectID,
	})
}

func AddChatAnalysisSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: chatAnalysisSweepInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:       chatAnalysisSweepWorkflowID,
		Workflow: ChatAnalysisSweepWorkflow,
		Args: []any{ChatAnalysisSweepParams{
			AfterProjectID: uuid.Nil,
		}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: chatAnalysisSweepRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID: chatAnalysisSweepScheduleID,
		// A tick that overlaps the previous one would re-signal projects the
		// running sweep is still working through, so a slow sweep skips rather
		// than doubles.
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, chatAnalysisSweepScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update chat analysis sweep schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create chat analysis sweep schedule: %w", err)
	}

	return nil
}

// TemporalChatAnalysisSignaler starts or wakes a project's coordinator through
// Temporal.
type TemporalChatAnalysisSignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

var _ analysis.Signaler = (*TemporalChatAnalysisSignaler)(nil)

// Signal delivers the request to the project's coordinator, starting it when no
// run is live. The workflow id is the project's, so a signal raised while a run
// is in flight joins that run instead of starting a second one.
func (s *TemporalChatAnalysisSignaler) Signal(ctx context.Context, projectID uuid.UUID) error {
	workflowID := chatAnalysisCoordinatorWorkflowID(projectID)

	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		workflowID,
		SignalChatAnalysisRequested,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    workflowID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		ChatAnalysisCoordinatorWorkflow,
		ChatAnalysisCoordinatorParams{
			ProjectID:     projectID,
			Cursor:        analysis.EnqueueCursor{CreatedAt: time.Time{}, ID: uuid.Nil},
			PendingCursor: analysis.PendingCursor{ObservedAt: time.Time{}, ID: uuid.Nil},
		},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start chat analysis coordinator: %w", err)
	}

	s.Logger.DebugContext(ctx, "chat analysis coordinator signal sent",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(workflowID),
	)

	return nil
}
