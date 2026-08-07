package background

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
)

func TestSkillSuggestionWorkflowIdentityIsPerSkill(t *testing.T) {
	t.Parallel()

	skillID := uuid.New()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: skillID, Force: false}
	require.Equal(t, "v1:skill-suggestion:"+skillID.String(), skillSuggestionWorkflowID(skillID))
	require.Equal(t, "v1:skill-suggestion:"+skillID.String()+"/signal", skillSuggestionSignal(params))
	require.NotEqual(t, skillSuggestionWorkflowID(uuid.New()), skillSuggestionWorkflowID(skillID))
	require.Equal(t, 5*time.Minute, defaultSkillSuggestionStartDelay)
	require.Equal(t, 40*time.Minute, skillSuggestionWorkflowTimeout)
}

func TestSkillSuggestionWorkflowCompletesSkippedPass(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, input activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		calls++
		require.Equal(t, params, input.SkillSuggestionIdentity)
		return &suggest.Result{Kind: suggest.ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
	var result suggest.Result
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, suggest.ResultSkipped, result.Kind)
}

func TestSkillSuggestionWorkflowReenqueuesBaseRace(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		return &suggest.Result{Kind: suggest.ResultBaseMoved, Reenqueue: true, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
	require.Equal(t, "SkillSuggestionWorkflow", continueErr.WorkflowType.Name)
}

func TestSkillSuggestionWorkflowCoalescesSignalDuringAnalysis(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		env.SignalWorkflow(skillSuggestionSignal(params), "enqueue")
		env.SignalWorkflow(skillSuggestionSignal(params), "enqueue")
		return &suggest.Result{Kind: suggest.ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
}

func TestSkillSuggestionWorkflowDrainsStartDelayBurstBeforeAnalysis(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		calls++
		return &suggest.Result{Kind: suggest.ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(skillSuggestionSignal(params), "enqueue")
		env.SignalWorkflow(skillSuggestionSignal(params), "enqueue")
		env.SignalWorkflow(skillSuggestionSignal(params), "enqueue")
	}, 0)

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
}

func TestSkillSuggestionWorkflowCoalescesManualSignalIntoForcedPass(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	env.RegisterActivityWithOptions(func(_ context.Context, input activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		require.True(t, input.Force)
		return &suggest.Result{Kind: suggest.ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(skillSuggestionSignal(params), skillSuggestionForceMessage)
	}, 0)

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	require.NoError(t, env.GetWorkflowError())
}

func TestSkillSuggestionWorkflowPreservesForcedPassOnReenqueue(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := SkillSuggestionParams{ProjectID: uuid.New(), SkillID: uuid.New(), Force: false}
	env.RegisterActivityWithOptions(func(_ context.Context, input activities.AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
		require.True(t, input.Force)
		return &suggest.Result{Kind: suggest.ResultBaseMoved, Reenqueue: true, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{}}, nil
	}, activity.RegisterOptions{Name: "AnalyzeSkillSuggestion"})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(skillSuggestionSignal(params), skillSuggestionForceMessage)
	}, 0)

	env.ExecuteWorkflow(SkillSuggestionWorkflow, params)

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
	var nextParams SkillSuggestionParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &nextParams))
	require.True(t, nextParams.Force)
}

func TestSkillSuggestionSweepPagesActiveSkillsAndSignalsExactIdentity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	project := activities.SkillSuggestionProject{ProjectID: uuid.New()}
	env.RegisterActivityWithOptions(func(_ context.Context, params activities.ListSkillSuggestionProjectsParams) ([]activities.SkillSuggestionProject, error) {
		require.Equal(t, uuid.Nil, params.AfterProjectID)
		require.Equal(t, int32(skillSuggestionProjectPageSize), params.PageLimit)
		return []activities.SkillSuggestionProject{project}, nil
	}, activity.RegisterOptions{Name: "ListSkillSuggestionProjects"})

	firstPage := make([]activities.ActiveSuggestionSkill, skillSuggestionSkillPageSize)
	seenAt := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	for i := range firstPage {
		firstPage[i] = activities.ActiveSuggestionSkill{ID: uuid.New(), LastSeenAt: seenAt.Add(-time.Duration(i) * time.Minute)}
	}
	last := activities.ActiveSuggestionSkill{ID: uuid.New(), LastSeenAt: seenAt.Add(-2 * time.Hour)}
	listCalls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, params activities.ListRecentlyActiveSuggestionSkillsParams) ([]activities.ActiveSuggestionSkill, error) {
		listCalls++
		require.Equal(t, project.ProjectID, params.ProjectID)
		if listCalls == 1 {
			require.True(t, params.CursorLastSeenAt.IsZero())
			require.Equal(t, uuid.Nil, params.CursorID)
			return firstPage, nil
		}
		require.Equal(t, firstPage[len(firstPage)-1].LastSeenAt, params.CursorLastSeenAt)
		require.Equal(t, firstPage[len(firstPage)-1].ID, params.CursorID)
		return []activities.ActiveSuggestionSkill{last}, nil
	}, activity.RegisterOptions{Name: "ListRecentlyActiveSuggestionSkills"})

	var signaled [][]activities.SkillSuggestionIdentity
	env.RegisterActivityWithOptions(func(_ context.Context, params []activities.SkillSuggestionIdentity) error {
		signaled = append(signaled, params)
		return nil
	}, activity.RegisterOptions{Name: "SignalSkillSuggestions"})

	env.ExecuteWorkflow(SkillSuggestionSweepWorkflow, SkillSuggestionSweepParams{})

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, listCalls)
	require.Len(t, signaled, 2)
	require.Len(t, signaled[0], skillSuggestionSkillPageSize)
	require.Len(t, signaled[1], 1)
	flattened := append([]activities.SkillSuggestionIdentity{}, signaled[0]...)
	flattened = append(flattened, signaled[1]...)
	for i, signal := range flattened {
		require.Equal(t, project.ProjectID, signal.ProjectID)
		if i < len(firstPage) {
			require.Equal(t, firstPage[i].ID, signal.SkillID)
		} else {
			require.Equal(t, last.ID, signal.SkillID)
		}
	}
}
