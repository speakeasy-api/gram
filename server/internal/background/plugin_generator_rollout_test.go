package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	bgactivities "github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// When continue-as-new fires mid-sweep, the workflow must carry the pagination
// cursor (and running tallies) into the next run. Otherwise a large rollout
// restarts at the first page every time and never advances past the point where
// continue-as-new gets suggested.
func TestPluginGeneratorRolloutWorkflow_ContinueAsNewCarriesPaginationCursor(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	id1 := uuid.New()
	id2 := uuid.New()

	listCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input bgactivities.ListPluginPublishCandidatesInput) (*bgactivities.ListPluginPublishCandidatesResult, error) {
			listCallCount++
			require.Equal(t, 1, listCallCount, "should continue-as-new before a second page is listed")
			require.Nil(t, input.AfterProjectID)
			// Suggest continue-as-new so the next loop iteration bails out after
			// this full page is processed.
			env.SetContinueAsNewSuggested(true)
			return &bgactivities.ListPluginPublishCandidatesResult{
				Candidates: []bgactivities.PluginPublishCandidate{
					{ProjectID: id1, CreatedByUserID: "user_1"},
					{ProjectID: id2, CreatedByUserID: "user_2"},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "ListPluginPublishCandidates"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			return &plugins.PublishProjectResult{RepoURL: "https://example.com/repo", Skipped: false}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginGeneratorRolloutWorkflow, PluginGeneratorRolloutInput{
		BatchSize:      2,
		CommitMessage:  "Update plugin packages",
		AfterProjectID: nil,
		Carried:        PluginGeneratorRolloutResult{Scanned: 0, Published: 0, Skipped: 0, Failed: 0},
	})

	require.True(t, env.IsWorkflowCompleted())

	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "PluginGeneratorRolloutWorkflow", continueAsNewErr.WorkflowType.Name)

	var nextInput PluginGeneratorRolloutInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))

	require.NotNil(t, nextInput.AfterProjectID)
	require.Equal(t, id2, *nextInput.AfterProjectID, "cursor must advance to the last project of the processed page")
	require.Equal(t, PluginGeneratorRolloutResult{Scanned: 2, Published: 2, Skipped: 0, Failed: 0}, nextInput.Carried)
}

// A continued run is started with the carried cursor and tallies. It must
// resume listing from that cursor and fold the carried counts into its result.
func TestPluginGeneratorRolloutWorkflow_ResumesFromCarriedCursor(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	resumeFrom := uuid.New()
	next := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, input bgactivities.ListPluginPublishCandidatesInput) (*bgactivities.ListPluginPublishCandidatesResult, error) {
			require.NotNil(t, input.AfterProjectID)
			require.Equal(t, resumeFrom, *input.AfterProjectID, "must resume from the carried cursor")
			// A partial page (len < batchSize) ends the sweep.
			return &bgactivities.ListPluginPublishCandidatesResult{
				Candidates: []bgactivities.PluginPublishCandidate{
					{ProjectID: next, CreatedByUserID: "user_3"},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "ListPluginPublishCandidates"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			return &plugins.PublishProjectResult{RepoURL: "https://example.com/repo", Skipped: false}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginGeneratorRolloutWorkflow, PluginGeneratorRolloutInput{
		BatchSize:      2,
		CommitMessage:  "Update plugin packages",
		AfterProjectID: &resumeFrom,
		Carried:        PluginGeneratorRolloutResult{Scanned: 10, Published: 3, Skipped: 5, Failed: 2},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PluginGeneratorRolloutResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, PluginGeneratorRolloutResult{Scanned: 11, Published: 4, Skipped: 5, Failed: 2}, result)
}
