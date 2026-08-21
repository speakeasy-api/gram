package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformMCPLocalFixtureConfigIsEnabledByDefaultLocally(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("local", "https://localhost:8080")
	require.NoError(t, err)
	require.NotNil(t, fixture)
	require.NotNil(t, fixture.Fixture)
	require.Equal(t, "https://localhost:8080", fixture.Origin.String())
}

func TestPlatformMCPLocalFixtureConfigAllowsLocalHTTPWithoutSyntheticSource(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("local", "http://localhost:8080")
	require.NoError(t, err)
	require.Nil(t, fixture)
}

func TestPlatformMCPLocalFixtureConfigDoesNotLeakOutsideLocal(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("production", "https://localhost:8080")
	require.NoError(t, err)
	require.Nil(t, fixture)
}
