package background

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestKickStartupSeeds_AppliesEachVersionOnce(t *testing.T) {
	t.Parallel()

	env, _ := infra.NewTemporalEnv(t)
	ctx := t.Context()

	var applied atomic.Int64
	w := worker.New(env.Client(), string(env.Queue()), worker.Options{})
	w.RegisterWorkflow(StartupSeedWorkflow)
	w.RegisterActivityWithOptions(
		func(context.Context, activities.ApplyStartupSeedArgs) error {
			applied.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "ApplyStartupSeed"},
	)
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)

	seed := activities.StartupSeed{Name: "reference-data", Version: "v1", Apply: nil}
	require.NoError(t, KickStartupSeeds(ctx, env, []activities.StartupSeed{seed}))
	require.NoError(t, env.Client().GetWorkflow(ctx, startupSeedWorkflowID(env.Queue(), seed), "").Get(ctx, nil))
	require.EqualValues(t, 1, applied.Load())

	// A restart on the same build kicks the same version, which already ran.
	require.NoError(t, KickStartupSeeds(ctx, env, []activities.StartupSeed{seed}))
	require.NoError(t, env.Client().GetWorkflow(ctx, startupSeedWorkflowID(env.Queue(), seed), "").Get(ctx, nil))
	require.EqualValues(t, 1, applied.Load())

	// A build that changed the seed carries a new version and applies it.
	next := activities.StartupSeed{Name: "reference-data", Version: "v2", Apply: nil}
	require.NoError(t, KickStartupSeeds(ctx, env, []activities.StartupSeed{next}))
	require.NoError(t, env.Client().GetWorkflow(ctx, startupSeedWorkflowID(env.Queue(), next), "").Get(ctx, nil))
	require.EqualValues(t, 2, applied.Load())
}
