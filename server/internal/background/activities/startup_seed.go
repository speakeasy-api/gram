package activities

import (
	"context"
	"fmt"
)

// StartupSeed is reference data that workers keep applied without an operator
// running a command. Each version is applied once per task queue.
type StartupSeed struct {
	// Name identifies the seed in workflow IDs. It never changes.
	Name string

	// Version changes whenever the data, or how it is applied, changes. A
	// content hash works well: a deploy that leaves the seed alone then
	// applies nothing.
	Version string

	// Apply writes the data. It must be idempotent, because a retry or a
	// rerun after a failed attempt repeats it.
	Apply func(ctx context.Context) error
}

// ApplyStartupSeedArgs names the seed version a workflow wants applied.
type ApplyStartupSeedArgs struct {
	// Name is the seed's Name.
	Name string

	// Version is the Version carried by the worker build that started the
	// workflow.
	Version string
}

// ApplyStartupSeed applies one startup seed from the set compiled into this
// worker build.
type ApplyStartupSeed struct {
	seeds map[string]StartupSeed
}

func NewApplyStartupSeed(seeds []StartupSeed) *ApplyStartupSeed {
	byName := make(map[string]StartupSeed, len(seeds))
	for _, seed := range seeds {
		byName[seed.Name] = seed
	}
	return &ApplyStartupSeed{seeds: byName}
}

// Do refuses a version this build does not carry. During a rollout a worker
// from the previous build can pick the task up, and applying its older data
// would mark the new version done without writing it; the error sends the
// task back for a current worker to take.
func (a *ApplyStartupSeed) Do(ctx context.Context, args ApplyStartupSeedArgs) error {
	seed, ok := a.seeds[args.Name]
	if !ok {
		return fmt.Errorf("startup seed %q: unknown to this worker build", args.Name)
	}
	if seed.Version != args.Version {
		return fmt.Errorf("startup seed %q: this worker build carries version %s, not %s", args.Name, seed.Version, args.Version)
	}
	if err := seed.Apply(ctx); err != nil {
		return fmt.Errorf("apply startup seed %q: %w", args.Name, err)
	}
	return nil
}
