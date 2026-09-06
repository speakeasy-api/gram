package background

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

func TestPluginPublishWorkflowID(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	params := PluginPublishParams{ProjectID: projectID, CreatedByUserID: "", CommitMessage: "", SkipIfUnchanged: true}

	// One workflow ID per project (and nothing else): every trigger for a
	// project must land on the same execution, or two publishes race the same
	// GitHub repo.
	require.Equal(t, "v1:plugin-publish:00000000-0000-0000-0000-0000000000aa", pluginPublishWorkflowID(params))
	require.Equal(t, "v1:plugin-publish:00000000-0000-0000-0000-0000000000aa/signal", pluginPublishDebounceSignal(params))

	other := params
	other.CommitMessage = "Initial marketplace publish"
	other.SkipIfUnchanged = false
	require.Equal(t, pluginPublishWorkflowID(params), pluginPublishWorkflowID(other))
}

// recordPublishInputs captures the activity inputs a publish workflow run sends.
type recordPublishInputs struct {
	mu     sync.Mutex
	inputs []plugins.PublishProjectInput
}

func (r *recordPublishInputs) register(env *testsuite.TestWorkflowEnvironment, skipped bool) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			r.mu.Lock()
			r.inputs = append(r.inputs, input)
			r.mu.Unlock()
			return &plugins.PublishProjectResult{RepoURL: "https://github.com/test-org/repo", Skipped: skipped}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)
}

func (r *recordPublishInputs) captured() []plugins.PublishProjectInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]plugins.PublishProjectInput(nil), r.inputs...)
}

func TestPluginPublishWorkflow_PassesParamsToActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	recorder := &recordPublishInputs{mu: sync.Mutex{}, inputs: nil}
	recorder.register(env, true)

	projectID := uuid.New()
	env.ExecuteWorkflow(PluginPublishWorkflow, PluginPublishParams{
		ProjectID:       projectID,
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result plugins.PublishProjectResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Skipped)

	require.Equal(t, []plugins.PublishProjectInput{{
		ProjectID:       projectID,
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}}, recorder.captured())
}

func TestPluginPublishWorkflowDebounced_CompletesWithoutSignals(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	recorder := &recordPublishInputs{mu: sync.Mutex{}, inputs: nil}
	recorder.register(env, true)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, PluginPublishParams{
		ProjectID:       uuid.New(),
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// A lone change trigger publishes exactly once and stops — no
	// ContinueAsNew, so an idle project costs one run per burst of changes.
	require.Len(t, recorder.captured(), 1)
}

func TestPluginPublishWorkflowDebounced_CoalescesSignalArrivingDuringRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	params := PluginPublishParams{
		ProjectID:       uuid.New(),
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}

	// Signal from inside the activity so the signals provably land mid-publish
	// (a delayed callback races the mocked activity's instant return). Two
	// changes landing mid-publish must produce exactly one follow-up run, not
	// one per change: the repo only has to converge on the final state.
	var runs int
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			runs++
			if runs == 1 {
				env.SignalWorkflow(pluginPublishDebounceSignal(params), "enqueue")
				env.SignalWorkflow(pluginPublishDebounceSignal(params), "enqueue")
			}
			return &plugins.PublishProjectResult{RepoURL: "https://github.com/test-org/repo", Skipped: true}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	require.True(t, env.IsWorkflowCompleted())

	// ContinueAsNew must target the wrapper, not the inner workflow, or the
	// follow-up run loses debounce semantics.
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "PluginPublishWorkflowDebounced", canErr.WorkflowType.Name)

	require.Equal(t, 1, runs, "the two signals coalesce into a single follow-up run")
}

func TestPluginPublishWorkflowDebounced_ForceSignalUpgradesPendingRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	params := PluginPublishParams{
		ProjectID:       uuid.New(),
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}

	// A forced publish (a project's first, which has no fingerprint to compare
	// against) folded into a pending fingerprint-gated run must win: otherwise
	// the publish that had to happen gets skipped.
	var runs int
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			runs++
			if runs == 1 {
				env.SignalWorkflow(pluginPublishDebounceSignal(params), pluginPublishForceSignal)
			}
			return &plugins.PublishProjectResult{RepoURL: "https://github.com/test-org/repo", Skipped: true}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	require.True(t, env.IsWorkflowCompleted())

	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)

	var next PluginPublishParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next))
	require.False(t, next.SkipIfUnchanged, "the forced signal must clear SkipIfUnchanged on the next run")
	require.Equal(t, params.ProjectID, next.ProjectID)
}

func TestPluginPublishWorkflowDebounced_StarterForceSignalAppliesToFirstRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	recorder := &recordPublishInputs{mu: sync.Mutex{}, inputs: nil}
	recorder.register(env, false)

	// SignalWithStartWorkflow delivers its own signal to the run it starts, so
	// a fingerprint-gated starter that is immediately forced publishes
	// unconditionally rather than deferring the force to a second run.
	params := PluginPublishParams{
		ProjectID:       uuid.New(),
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Initial marketplace publish",
		SkipIfUnchanged: true,
	}
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(pluginPublishDebounceSignal(params), pluginPublishForceSignal)
	}, 0)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	captured := recorder.captured()
	require.Len(t, captured, 1)
	require.False(t, captured[0].SkipIfUnchanged)
}
