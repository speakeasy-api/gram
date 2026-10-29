package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// A manual sync folded into a pending ordinary run, before or after the
// ordinary trigger, must make that run a manual sync: dropping it would leave
// the pending changes of plugins whose auto sync is off unpublished.
func TestPluginPublishWorkflowDebounced_SyncSurvivesCoalescingWithOrdinarySignals(t *testing.T) {
	t.Parallel()

	for name, messages := range map[string][]string{
		"sync last":  {"enqueue", pluginPublishSyncSignal},
		"sync first": {pluginPublishSyncSignal, "enqueue", pluginPublishForceSignal},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			recorder := &recordPublishInputs{mu: sync.Mutex{}, inputs: nil}
			recorder.register(env, false)

			params := PluginPublishParams{ProjectID: uuid.New(), CreatedByUserID: "user_01HZ", CommitMessage: "Update plugin packages", SkipIfUnchanged: true}
			env.RegisterDelayedCallback(func() {
				for _, message := range messages {
					env.SignalWorkflow(pluginPublishDebounceSignal(params), message)
				}
			}, 0)

			env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())
			captured := recorder.captured()
			require.Len(t, captured, 1, "the signals coalesce into one run")
			require.True(t, captured[0].IncludePausedPlugins, "the coalesced run must publish paused plugins' pending changes")
		})
	}
}

// A manual sync arriving while an ordinary publish runs upgrades the follow-up
// run, the way a forced trigger does.
func TestPluginPublishWorkflowDebounced_SyncSignalUpgradesPendingRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := PluginPublishParams{ProjectID: uuid.New(), CreatedByUserID: "user_01HZ", CommitMessage: "Update plugin packages", SkipIfUnchanged: true}

	var runs int
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			runs++
			if runs == 1 {
				env.SignalWorkflow(pluginPublishDebounceSignal(params), pluginPublishSyncSignal)
				env.SignalWorkflow(pluginPublishDebounceSignal(params), "enqueue")
			}
			return &plugins.PublishProjectResult{RepoURL: "https://github.com/test-org/repo", Skipped: false}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	var next PluginPublishParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next))
	require.True(t, next.IncludePausedPlugins, "the follow-up run is the manual sync that was asked for")
	require.True(t, next.SkipIfUnchanged, "a manual sync stays fingerprint-gated")
}

// A manual sync holds for its own run only. Changes made to a plugin whose
// auto sync is off while that run is publishing must keep waiting, so the
// follow-up run their signals start is automatic again.
func TestPluginPublishWorkflowDebounced_SyncDoesNotCarryIntoTheNextRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := PluginPublishParams{ProjectID: uuid.New(), CreatedByUserID: "user_01HZ", CommitMessage: "Update plugin packages", SkipIfUnchanged: true, IncludePausedPlugins: true}

	var inputs []plugins.PublishProjectInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			inputs = append(inputs, input)
			if len(inputs) == 1 {
				env.SignalWorkflow(pluginPublishDebounceSignal(params), "enqueue")
			}
			return &plugins.PublishProjectResult{RepoURL: "https://github.com/test-org/repo", Skipped: false}, nil
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	require.Len(t, inputs, 1)
	require.True(t, inputs[0].IncludePausedPlugins, "the sync run publishes paused plugins' pending changes")
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	var next PluginPublishParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next))
	require.False(t, next.IncludePausedPlugins, "a completed sync must not make the next run a sync")
}

// A sync whose run failed has not happened, so a follow-up run started by
// signals that arrived meanwhile must still carry it out.
func TestPluginPublishWorkflowDebounced_FailedSyncCarriesIntoTheNextRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := PluginPublishParams{ProjectID: uuid.New(), CreatedByUserID: "user_01HZ", CommitMessage: "Update plugin packages", SkipIfUnchanged: true, IncludePausedPlugins: true}

	var attempts int
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
			attempts++
			if attempts == 1 {
				env.SignalWorkflow(pluginPublishDebounceSignal(params), "enqueue")
			}
			return nil, errors.New("push failed")
		},
		activity.RegisterOptions{Name: "PublishPluginProject"},
	)

	env.ExecuteWorkflow(PluginPublishWorkflowDebounced, params)

	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	var next PluginPublishParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next))
	require.True(t, next.IncludePausedPlugins, "the failed sync is retried by the next run")
}

func TestPluginPublishSignalMessage(t *testing.T) {
	t.Parallel()

	ordinary := PluginPublishParams{ProjectID: uuid.New(), CreatedByUserID: "", CommitMessage: "", SkipIfUnchanged: true}
	require.Equal(t, "enqueue", pluginPublishSignalMessage(ordinary))

	forced := ordinary
	forced.SkipIfUnchanged = false
	require.Equal(t, pluginPublishForceSignal, pluginPublishSignalMessage(forced))

	synced := ordinary
	synced.IncludePausedPlugins = true
	require.Equal(t, pluginPublishSyncSignal, pluginPublishSignalMessage(synced))
}

// rejectingPublishClient stands in for the plugins service and refuses every
// publish the way an organization with no member to attribute keys to does.
type rejectingPublishClient struct {
	mu    sync.Mutex
	calls int
}

func (c *rejectingPublishClient) PublishProject(context.Context, plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return nil, fmt.Errorf("%w: organization %q", plugins.ErrPluginAPIKeyCreatorNotMember, "org_none")
}

func TestPluginPublishWorkflow_NoActorIsRejectedOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Run the registered activity shim, not a stand-in: the shim is the last
	// hop before the Temporal SDK decides retryability from the error's
	// concrete type, so a wrapper there silently turns the permanent
	// rejection into three attempts and a failed workflow.
	client := &rejectingPublishClient{mu: sync.Mutex{}, calls: 0}
	var a Activities
	a.pluginPublisher = activities.NewPluginPublisher(testenv.NewLogger(t), nil, client)
	env.RegisterActivity(a.PublishPluginProject)

	env.ExecuteWorkflow(PluginPublishWorkflow, PluginPublishParams{
		ProjectID:       uuid.New(),
		CreatedByUserID: "user_01HZ",
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result plugins.PublishProjectResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Skipped)
	require.Equal(t, 1, client.calls, "a permanent rejection must not be retried")
}
