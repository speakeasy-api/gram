package activities_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestApplyStartupSeed_AppliesTheVersionThisBuildCarries(t *testing.T) {
	t.Parallel()

	applied := 0
	a := activities.NewApplyStartupSeed([]activities.StartupSeed{{
		Name:    "reference-data",
		Version: "v2",
		Apply:   func(context.Context) error { applied++; return nil },
	}})

	require.NoError(t, a.Do(t.Context(), activities.ApplyStartupSeedArgs{Name: "reference-data", Version: "v2"}))
	require.Equal(t, 1, applied)
}

func TestApplyStartupSeed_RefusesAVersionFromAnotherBuild(t *testing.T) {
	t.Parallel()

	applied := 0
	a := activities.NewApplyStartupSeed([]activities.StartupSeed{{
		Name:    "reference-data",
		Version: "v1",
		Apply:   func(context.Context) error { applied++; return nil },
	}})

	err := a.Do(t.Context(), activities.ApplyStartupSeedArgs{Name: "reference-data", Version: "v2"})
	require.ErrorContains(t, err, "carries version v1, not v2")
	require.Zero(t, applied, "older data must not be written under the newer version's run")
}

func TestApplyStartupSeed_RefusesAnUnknownSeed(t *testing.T) {
	t.Parallel()

	a := activities.NewApplyStartupSeed(nil)

	err := a.Do(t.Context(), activities.ApplyStartupSeedArgs{Name: "reference-data", Version: "v1"})
	require.ErrorContains(t, err, "unknown to this worker build")
}

func TestApplyStartupSeed_ReturnsTheApplyError(t *testing.T) {
	t.Parallel()

	failure := errors.New("database unavailable")
	a := activities.NewApplyStartupSeed([]activities.StartupSeed{{
		Name:    "reference-data",
		Version: "v1",
		Apply:   func(context.Context) error { return failure },
	}})

	err := a.Do(t.Context(), activities.ApplyStartupSeedArgs{Name: "reference-data", Version: "v1"})
	require.ErrorIs(t, err, failure)
}

func TestApplyStartupSeed_RefusesARepeatedName(t *testing.T) {
	t.Parallel()

	applied := 0
	apply := func(context.Context) error { applied++; return nil }
	seeds := []activities.StartupSeed{
		{Name: "reference-data", Version: "v1", Apply: apply},
		{Name: "reference-data", Version: "v2", Apply: apply},
	}
	require.ErrorContains(t, activities.ValidateStartupSeeds(seeds), `startup seed "reference-data": declared more than once`)

	a := activities.NewApplyStartupSeed(seeds)
	for _, version := range []string{"v1", "v2"} {
		err := a.Do(t.Context(), activities.ApplyStartupSeedArgs{Name: "reference-data", Version: version})
		require.ErrorContains(t, err, "declared more than once")
	}
	require.Zero(t, applied, "neither seed may shadow the other")
}
