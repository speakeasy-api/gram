package admission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestGuardResolveRequiresInjectedProvider(t *testing.T) {
	t.Parallel()

	_, err := (&Guard{}).Resolve(t.Context(), "organization", "organization", "project")
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestRequireUsableRolloutRejectsOpenMode(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, requireUsableRollout(RolloutConfig{}, nil), ErrUnavailable)
	require.ErrorIs(t, requireUsableRollout(RolloutConfig{Mode: ModeEnforce}, context.Canceled), ErrUnavailable)
	require.NoError(t, requireUsableRollout(RolloutConfig{Mode: ModeLegacy}, nil))
}

func TestRolloutKillSwitchIsIndependentFromMode(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, "organization", false)
	flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, "organization", true)

	config, err := NewGuard(flags).Resolve(t.Context(), "organization", "organization", "project")
	require.NoError(t, err)
	require.Equal(t, ModeLegacy, config.Mode)
	require.True(t, config.DirectRemoteDistributionDisabled)
}

// Compile-time assertions keep the transaction-facing guard API stable for the
// independently wired service packages.
var (
	_ = (*Guard).CheckAttachment
	_ = (*Guard).CheckPluginAudience
	_ = (*Guard).CheckMCPServerTarget
	_ = (*Guard).CheckRemoteTarget
)
