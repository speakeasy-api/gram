package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	bgactivities "github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	pluginGeneratorRolloutScheduleID = "v1:plugin-generator-rollout-schedule"
	pluginGeneratorRolloutWorkflowID = pluginGeneratorRolloutScheduleID + "/scheduled"
	// The schedule cadence determines how long a generator or plugin-config
	// change takes to propagate, since nothing else triggers the rollout. The
	// fingerprint check keeps unchanged projects from doing any GitHub/key work,
	// so each tick is cheap apart from the per-project scan (resolve + in-memory
	// generate + fingerprint compare). SKIP overlap (below) gives us the
	// "trigger every interval unless one is already running" behaviour without a
	// separate triggering workflow: a run that outlasts the interval just defers
	// the next tick.
	pluginGeneratorRolloutInterval         = 10 * time.Second
	pluginGeneratorRolloutDefaultBatchSize = int32(100)
	pluginGeneratorRolloutConcurrency      = 5
)

type PluginGeneratorRolloutInput struct {
	BatchSize     int32
	CommitMessage string

	// AfterProjectID and Carried hold the pagination cursor and running tallies
	// that survive a continue-as-new. They are zero on a fresh (scheduled) run
	// and are repopulated when the workflow continues itself, so a large rollout
	// resumes from where it left off instead of restarting at the first page.
	AfterProjectID *uuid.UUID
	Carried        PluginGeneratorRolloutResult
}

type PluginGeneratorRolloutResult struct {
	Scanned   int
	Published int
	Skipped   int
	Failed    int
}

func ExecutePluginGeneratorRolloutWorkflow(ctx context.Context, env *tenv.Environment, input PluginGeneratorRolloutInput) (client.WorkflowRun, error) {
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    fmt.Sprintf("v1:plugin-generator-rollout/%d", time.Now().UTC().Unix()),
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    6 * time.Hour,
	}, PluginGeneratorRolloutWorkflow, input)
}

func PluginGeneratorRolloutWorkflow(ctx workflow.Context, input PluginGeneratorRolloutInput) (*PluginGeneratorRolloutResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})

	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = pluginGeneratorRolloutDefaultBatchSize
	}
	commitMessage := input.CommitMessage
	if commitMessage == "" {
		commitMessage = "Update plugin packages"
	}

	var a *Activities
	result := &PluginGeneratorRolloutResult{
		Scanned:   input.Carried.Scanned,
		Published: input.Carried.Published,
		Skipped:   input.Carried.Skipped,
		Failed:    input.Carried.Failed,
	}

	// At a frequent cadence most runs publish nothing (the fingerprint skips
	// unchanged projects), so only log when real work happened — a propagation
	// or a failure worth seeing — rather than emitting a line every tick.
	logSummary := func() {
		if result.Published > 0 || result.Failed > 0 {
			workflow.GetLogger(ctx).Info("plugin generator rollout complete",
				"scanned", result.Scanned,
				"published", result.Published,
				"skipped", result.Skipped,
				"failed", result.Failed,
			)
		}
	}

	after := input.AfterProjectID
	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			next := input
			next.AfterProjectID = after
			next.Carried = *result
			return nil, workflow.NewContinueAsNewError(ctx, PluginGeneratorRolloutWorkflow, next)
		}

		var candidates bgactivities.ListPluginPublishCandidatesResult
		if err := workflow.ExecuteActivity(ctx, a.ListPluginPublishCandidates, bgactivities.ListPluginPublishCandidatesInput{
			AfterProjectID: after,
			Limit:          batchSize,
		}).Get(ctx, &candidates); err != nil {
			return nil, fmt.Errorf("list plugin publish candidates: %w", err)
		}

		if len(candidates.Candidates) == 0 {
			logSummary()
			return result, nil
		}

		result.Scanned += len(candidates.Candidates)
		after = &candidates.Candidates[len(candidates.Candidates)-1].ProjectID

		for start := 0; start < len(candidates.Candidates); start += pluginGeneratorRolloutConcurrency {
			end := min(start+pluginGeneratorRolloutConcurrency, len(candidates.Candidates))

			futures := make([]workflow.Future, 0, end-start)
			for _, candidate := range candidates.Candidates[start:end] {
				futures = append(futures, workflow.ExecuteActivity(ctx, a.PublishPluginProject, plugins.PublishProjectInput{
					ProjectID:       candidate.ProjectID,
					CreatedByUserID: candidate.CreatedByUserID,
					CommitMessage:   commitMessage,
					SkipIfUnchanged: true,
				}))
			}

			for _, future := range futures {
				var publishResult plugins.PublishProjectResult
				if err := future.Get(ctx, &publishResult); err != nil {
					var appErr *temporal.ApplicationError
					if errors.As(err, &appErr) && appErr.Type() == bgactivities.ErrTypeGitHubRepoConflict {
						result.Skipped++
						workflow.GetLogger(ctx).Warn("plugin project publish skipped: github repo conflict", "error", err)
						continue
					}
					result.Failed++
					workflow.GetLogger(ctx).Error("plugin project publish failed", "error", err)
					continue
				}
				if publishResult.Skipped {
					result.Skipped++
					continue
				}
				result.Published++
			}
		}

		if len(candidates.Candidates) < int(batchSize) {
			logSummary()
			return result, nil
		}
	}
}

func AddPluginGeneratorRolloutSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: pluginGeneratorRolloutInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 pluginGeneratorRolloutWorkflowID,
		Workflow:           PluginGeneratorRolloutWorkflow,
		Args:               []any{PluginGeneratorRolloutInput{BatchSize: 0, CommitMessage: "", AfterProjectID: nil, Carried: PluginGeneratorRolloutResult{Scanned: 0, Published: 0, Skipped: 0, Failed: 0}}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 6 * time.Hour,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      pluginGeneratorRolloutScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, pluginGeneratorRolloutScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing plugin generator rollout schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create plugin generator rollout schedule: %w", err)
	}

	return nil
}
