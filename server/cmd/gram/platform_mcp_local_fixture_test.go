package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformMCPLocalFixtureConfigIsLocalOnly(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI(
		"local",
		true,
		"https://localhost:8080",
	)
	require.NoError(t, err)
	require.NotNil(t, fixture)
	require.NotNil(t, fixture.Fixture)

	fixture, err = platformMCPLocalFixtureConfigFromCLI(
		"production",
		true,
		"https://localhost:8080",
	)
	require.ErrorContains(t, err, "only supported when environment is local")
	require.Nil(t, fixture)
}

func TestPlatformMCPLocalFixtureConfigIsDisabledByDefault(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("local", false, "https://localhost:8080")
	require.NoError(t, err)
	require.Nil(t, fixture)
}
