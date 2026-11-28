package platformswitches_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformswitches"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func TestSwitch_OffUntilSet(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "platform_switches")
	require.NoError(t, err)

	state, err := platformswitches.Get(t.Context(), conn, platformswitches.KeySelfServeDisabled)
	require.NoError(t, err)
	require.False(t, state.Engaged)
	require.Empty(t, state.UpdatedBy)
	require.True(t, state.UpdatedAt.IsZero())

	set, err := platformswitches.Set(t.Context(), conn, platformswitches.KeySelfServeDisabled, true, "first@example.com")
	require.NoError(t, err)
	require.True(t, set.Engaged)

	set, err = platformswitches.Set(t.Context(), conn, platformswitches.KeySelfServeDisabled, false, "second@example.com")
	require.NoError(t, err)

	state, err = platformswitches.Get(t.Context(), conn, platformswitches.KeySelfServeDisabled)
	require.NoError(t, err)
	require.False(t, state.Engaged)
	require.Equal(t, "second@example.com", state.UpdatedBy)
	require.Equal(t, set.UpdatedAt, state.UpdatedAt)
}
