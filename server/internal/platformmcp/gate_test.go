package adminmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type testCapabilityChecker struct {
	enabled bool
	err     error
}

func (c testCapabilityChecker) IsFeatureEnabled(_ context.Context, _ string, capability productfeatures.Feature) (bool, error) {
	if capability != productfeatures.FeatureAdminMCP {
		return false, errors.New("unexpected capability")
	}
	return c.enabled, c.err
}

func TestOrganizationGateRequiresBothGates(t *testing.T) {
	t.Parallel()

	rollout := &feature.InMemory{}
	rollout.SetFlag(feature.FlagAdminMCPRollout, "organization-1", true)

	t.Run("allows both enabled", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout).Enabled(t.Context(), "organization-1")
		require.NoError(t, err)
		require.True(t, enabled)
	})

	t.Run("denies missing capability", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{}, rollout).Enabled(t.Context(), "organization-1")
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("denies rollout disabled", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout).Enabled(t.Context(), "organization-2")
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("returns capability errors", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{err: errors.New("unavailable")}, rollout).Enabled(t.Context(), "organization-1")
		require.Error(t, err)
		require.False(t, enabled)
	})
}
