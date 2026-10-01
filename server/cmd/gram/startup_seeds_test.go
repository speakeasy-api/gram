package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestSelectStartupSeeds(t *testing.T) {
	t.Parallel()

	seeds := []activities.StartupSeed{
		{Name: "first", Version: "v1", Apply: nil},
		{Name: "second", Version: "v1", Apply: nil},
	}

	all, err := selectStartupSeeds(seeds, nil)
	require.NoError(t, err)
	require.Equal(t, seeds, all)

	one, err := selectStartupSeeds(seeds, []string{"second"})
	require.NoError(t, err)
	require.Equal(t, seeds[1:], one)

	_, err = selectStartupSeeds(seeds, []string{"missing"})
	require.ErrorContains(t, err, `unknown startup seed "missing"; known seeds: first, second`)
}

func TestStartupSeedsHaveDistinctNames(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, seed := range startupSeeds(nil, nil) {
		require.NotEmpty(t, seed.Name)
		require.NotEmpty(t, seed.Version)
		require.False(t, seen[seed.Name], "seed names key workflow IDs, so they must be distinct: %s", seed.Name)
		seen[seed.Name] = true
	}
}
