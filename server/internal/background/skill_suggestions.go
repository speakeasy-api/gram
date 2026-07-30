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
	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	defaultSkillSuggestionStartDelay = 5 * time.Minute
	skillSuggestionActivityTimeout   = 10 * time.Minute
	skillSuggestionWorkflowTimeout   = 40 * time.Minute
	skillSuggestionSweepInterval     = 24 * time.Hour
	skillSuggestionSweepTimeout      = 2 * time.Hour
	skillSuggestionProjectPageSize   = 100
	skillSuggestionSkillPageSize     = 100
	skillSuggestionMaxProjectPages   = 10
	skillSuggestionMaxSkillPages     = 10

	skillSuggestionSweepScheduleID = "v1:skill-suggestion-sweep-schedule"
	skillSuggestionSweepWorkflowID = skillSuggestionSweepScheduleID + "/scheduled"
	skillSuggestionForceMessage    = "force"
)

type SkillSuggestionParams = activities.SkillSuggestionIdentity

type SkillSuggestionSweepParams struct {
	AfterProjectID   uuid.UUID                         `json:"after_project_id"`
	ActiveSince      time.Time                         `json:"active_since"`
	CurrentProject   activities.SkillSuggestionProject `json:"current_project"`
	CursorLastSeenAt time.Time                         `json:"cursor_last_seen_at"`
	CursorSkillID    uuid.UUID                         `json:"cursor_skill_id"`
}

func skillSuggestionWorkflowID(skillID uuid.UUID) string {
	return fmt.Sprintf("v1:skill-suggestion:%s", skillID)
}

func skillSuggestionSignal(params SkillSuggestionParams) string {
	return skillSuggestionWorkflowID(params.SkillID) + "/signal"
}

func SkillSuggestionWorkflow(ctx workflow.Context, params SkillSuggestionParams) (*suggest.Result, error) {
	return DebounceWithSignalCoalescing(
		SkillSuggestionAnalysisWorkflow,
		SkillSuggestionWorkflow,
		skillSuggestionSignal,
		func(_ SkillSuggestionParams, result *suggest.Result) bool { return result.Reenqueue },
		func(params SkillSuggestionParams, message string) SkillSuggestionParams {
			params.Force = params.Force || message == skillSuggestionForceMessage
			return params
		},
	)(ctx, params)
}

func SkillSuggestionAnalysisWorkflow(ctx workflow.Context, params SkillSuggestionParams) (*suggest.Result, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           SkillEfficacyTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName)),
		StartToCloseTimeout: skillSuggestionActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})
	var analyzer *activities.SkillSuggestionAnalyzer
	var result suggest.Result
	if err := workflow.ExecuteActivity(ctx, analyzer.AnalyzeSkillSuggestion, activities.AnalyzeSkillSuggestionParams{
		SkillSuggestionIdentity: params,
		Now:                     workflow.Now(ctx),
	}).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("analyze skill suggestion: %w", err)
	}
	return &result, nil
}

func SkillSuggestionSweepWorkflow(ctx workflow.Context, params SkillSuggestionSweepParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3, InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Second},
	})
	if params.ActiveSince.IsZero() {
		params.ActiveSince = workflow.Now(ctx).Add(-suggest.DefaultConfig().ActivityWindow)
	}

	if params.CurrentProject.ProjectID != uuid.Nil {
		done, err := sweepSkillSuggestionProject(ctx, params.CurrentProject, &params)
		if err != nil {
			return err
		}
		if !done {
			return workflow.NewContinueAsNewError(ctx, SkillSuggestionSweepWorkflow, params)
		}
		params.AfterProjectID = params.CurrentProject.ProjectID
		params.CurrentProject = activities.SkillSuggestionProject{ProjectID: uuid.Nil}
		params.CursorLastSeenAt = time.Time{}
		params.CursorSkillID = uuid.Nil
	}

	var analyzer *activities.SkillSuggestionAnalyzer
	for range skillSuggestionMaxProjectPages {
		var projects []activities.SkillSuggestionProject
		if err := workflow.ExecuteActivity(ctx, analyzer.ListSkillSuggestionProjects, activities.ListSkillSuggestionProjectsParams{
			AfterProjectID: params.AfterProjectID,
			ActiveSince:    params.ActiveSince,
			PageLimit:      skillSuggestionProjectPageSize,
		}).Get(ctx, &projects); err != nil {
			return fmt.Errorf("list skill suggestion projects: %w", err)
		}
		for _, project := range projects {
			params.CurrentProject = project
			done, err := sweepSkillSuggestionProject(ctx, project, &params)
			if err != nil {
				return err
			}
			if !done {
				return workflow.NewContinueAsNewError(ctx, SkillSuggestionSweepWorkflow, params)
			}
			params.AfterProjectID = project.ProjectID
			params.CurrentProject = activities.SkillSuggestionProject{ProjectID: uuid.Nil}
			params.CursorLastSeenAt = time.Time{}
			params.CursorSkillID = uuid.Nil
		}
		if len(projects) < skillSuggestionProjectPageSize {
			return nil
		}
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, SkillSuggestionSweepWorkflow, params)
		}
	}
	return workflow.NewContinueAsNewError(ctx, SkillSuggestionSweepWorkflow, params)
}

func sweepSkillSuggestionProject(ctx workflow.Context, project activities.SkillSuggestionProject, params *SkillSuggestionSweepParams) (bool, error) {
	var analyzer *activities.SkillSuggestionAnalyzer
	logger := workflow.GetLogger(ctx)
	for range skillSuggestionMaxSkillPages {
		var skills []activities.ActiveSuggestionSkill
		if err := workflow.ExecuteActivity(ctx, analyzer.ListRecentlyActiveSuggestionSkills, activities.ListRecentlyActiveSuggestionSkillsParams{
			ProjectID:        project.ProjectID,
			ActiveSince:      params.ActiveSince,
			CursorLastSeenAt: params.CursorLastSeenAt,
			CursorID:         params.CursorSkillID,
			PageLimit:        skillSuggestionSkillPageSize,
		}).Get(ctx, &skills); err != nil {
			return false, fmt.Errorf("list active skills for suggestion sweep: %w", err)
		}
		identities := make([]activities.SkillSuggestionIdentity, len(skills))
		for i, skill := range skills {
			identities[i] = activities.SkillSuggestionIdentity{ProjectID: project.ProjectID, SkillID: skill.ID, Force: false}
		}
		if len(identities) > 0 {
			if err := workflow.ExecuteActivity(ctx, analyzer.SignalSkillSuggestions, identities).Get(ctx, nil); err != nil {
				logger.Error("signal skill suggestion page failed", "project_id", project.ProjectID.String(), "count", len(identities), "error", err.Error())
			}
		}
		if len(skills) < skillSuggestionSkillPageSize {
			return true, nil
		}
		last := skills[len(skills)-1]
		params.CursorLastSeenAt = last.LastSeenAt
		params.CursorSkillID = last.ID
	}
	return false, nil
}

func AddSkillSuggestionSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: skillSuggestionSweepInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:       skillSuggestionSweepWorkflowID,
		Workflow: SkillSuggestionSweepWorkflow,
		Args: []any{SkillSuggestionSweepParams{
			AfterProjectID: uuid.Nil, ActiveSince: time.Time{},
			CurrentProject:   activities.SkillSuggestionProject{ProjectID: uuid.Nil},
			CursorLastSeenAt: time.Time{}, CursorSkillID: uuid.Nil,
		}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: skillSuggestionSweepTimeout,
	}
	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{ID: skillSuggestionSweepScheduleID, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP, Spec: spec, Action: action})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, skillSuggestionSweepScheduleID).Update(ctx, client.ScheduleUpdateOptions{DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			input.Description.Schedule.Spec = &spec
			input.Description.Schedule.Action = action
			return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
		}}); err != nil {
			return fmt.Errorf("update skill suggestion sweep schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create skill suggestion sweep schedule: %w", err)
	}
	return nil
}

type TemporalSkillSuggestionSignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
	StartDelay  time.Duration
}

var _ suggest.Signaler = (*TemporalSkillSuggestionSignaler)(nil)
var _ domainskills.ManualSuggestionSignaler = (*TemporalSkillSuggestionSignaler)(nil)

func (s *TemporalSkillSuggestionSignaler) Signal(ctx context.Context, projectID, skillID uuid.UUID) error {
	return s.signal(ctx, projectID, skillID, "enqueue", s.StartDelay)
}

// SignalManual requests a pass that bypasses automatic thresholds.
func (s *TemporalSkillSuggestionSignaler) SignalManual(ctx context.Context, projectID, skillID uuid.UUID) error {
	return s.signal(ctx, projectID, skillID, skillSuggestionForceMessage, 0)
}

func (s *TemporalSkillSuggestionSignaler) signal(ctx context.Context, projectID, skillID uuid.UUID, message string, startDelay time.Duration) error {
	params := SkillSuggestionParams{ProjectID: projectID, SkillID: skillID, Force: false}
	workflowID := skillSuggestionWorkflowID(skillID)
	if message != skillSuggestionForceMessage && startDelay <= 0 {
		startDelay = defaultSkillSuggestionStartDelay
	}
	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(ctx, workflowID, skillSuggestionSignal(params), message, client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       skillSuggestionWorkflowTimeout,
		StartDelay:               startDelay,
	}, SkillSuggestionWorkflow, params)
	if err != nil {
		return fmt.Errorf("signal-with-start skill suggestion: %w", err)
	}
	s.Logger.DebugContext(ctx, "skill suggestion signal sent", attr.SlogProjectID(projectID.String()), attr.SlogResourceID(skillID.String()), attr.SlogTemporalWorkflowID(workflowID))
	return nil
}
