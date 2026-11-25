package background

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

// The sweep's activity must be registered on the worker that runs the
// workflow. Dependencies are nil, so the activity itself fails; what matters
// is that the worker finds it rather than reporting an unknown activity type.
func TestSignInRevocationSweepWorkflow_ActivityIsRegistered(t *testing.T) {
	t.Parallel()

	env, _ := infra.NewTemporalEnv(t)
	workers := newSchedulingWorkers(t, env)
	require.NoError(t, workers.Start(), "start workers")
	t.Cleanup(workers.Stop)

	ctx := t.Context()
	run, err := env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                 "sign-in-revocation-sweep-registration",
		TaskQueue:          string(env.Queue()),
		WorkflowRunTimeout: time.Minute,
	}, SignInRevocationSweepWorkflow)
	require.NoError(t, err)

	err = run.Get(ctx, nil)
	if err != nil {
		require.NotContains(t, err.Error(), "unable to find activityType", "the sweep activity is registered")
	}
}
