package gram

import (
	"flag"
	"fmt"
	"github.com/urfave/cli/v2"
	"slices"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/risk/policylifecycle"

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

	reversed, err := selectStartupSeeds(seeds, []string{"second", "first"})
	require.NoError(t, err)
	require.Equal(t, []activities.StartupSeed{seeds[1], seeds[0]}, reversed)

	_, err = selectStartupSeeds(seeds, []string{"missing"})
	require.ErrorContains(t, err, `unknown startup seed "missing"; known seeds: first, second`)
}

func TestStartupSeedsSourceManagedCatalog(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "dev", "prod", "preview", ""} {
		for _, managed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/managed=%t", environment, managed), func(t *testing.T) {
				seeds := startupSeeds(nil, nil, nil, environment, managed)
				names := make([]string, 0, len(seeds))
				for _, seed := range seeds {
					names = append(names, seed.Name)
				}
				require.Contains(t, names, policylifecycle.OrphanRepairSeedName)
				if managed && (environment == "local" || environment == "dev") {
					require.NotContains(t, names, "registry-okta", "startup/reseed must not recreate upstream removals or enrich source-managed testing edits")
					require.Len(t, names, 1)
				} else {
					require.Contains(t, names, "registry-okta")
					require.Len(t, names, 2)
				}
			})
		}
	}
}
func TestCatalogSourceManagedModeReachesEverySeedEntrypoint(t *testing.T) {
	t.Setenv("GRAM_CATALOG_SOURCE_MANAGED", "true")
	t.Setenv("GRAM_ENVIRONMENT", "local")
	for name, flags := range map[string][]cli.Flag{
		"admin":           newAdminCommand().Flags,
		"worker":          workerRuntimeFlags(),
		"embedded worker": serverFlags(),
		"reseed":          newAppSeedCommand().Flags,
	} {
		t.Run(name, func(t *testing.T) {
			set := flag.NewFlagSet(name, flag.ContinueOnError)
			for _, f := range flags {
				if slices.Contains(f.Names(), "catalog-source-managed") || slices.Contains(f.Names(), "environment") {
					require.NoError(t, f.Apply(set))
				}
			}
			c := cli.NewContext(cli.NewApp(), set, nil)
			require.True(t, c.Bool("catalog-source-managed"))
			require.Equal(t, "local", c.String("environment"))
			for _, seed := range startupSeeds(nil, nil, nil, c.String("environment"), c.Bool("catalog-source-managed")) {
				require.NotEqual(t, "registry-okta", seed.Name)
			}
		})
	}
}
func TestStartupSeedsHaveDistinctNames(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, seed := range startupSeeds(nil, nil, nil, "local", false) {
		require.NotEmpty(t, seed.Name)
		require.NotEmpty(t, seed.Version)
		require.False(t, seen[seed.Name], "seed names key workflow IDs, so they must be distinct: %s", seed.Name)
		seen[seed.Name] = true
	}
}
