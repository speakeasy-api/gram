package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type testCapabilityChecker struct {
	enabled bool
	err     error
}

func (c testCapabilityChecker) IsFeatureEnabled(_ context.Context, _ string, capability productfeatures.Feature) (bool, error) {
	if capability != productfeatures.FeaturePlatformMCP {
		return false, errors.New("unexpected capability")
	}
	return c.enabled, c.err
}

func (c testCapabilityChecker) IsFeatureEnabledUncached(ctx context.Context, organizationID string, capability productfeatures.Feature) (bool, error) {
	return c.IsFeatureEnabled(ctx, organizationID, capability)
}

func TestCatalogRegistrationGateRequiresPlatformMCPCapability(t *testing.T) {
	t.Parallel()

	const organizationID = "organization-1"
	const projectSlug = "project-slug"

	newMainGate := func(enabled bool, err error) *testGate {
		return &testGate{enabled: enabled, err: err}
	}

	t.Run("allows enabled organization", func(t *testing.T) {
		t.Parallel()

		enabled, err := NewCatalogRegistrationGate(newMainGate(true, nil)).Enabled(t.Context(), organizationID, projectSlug)

		require.NoError(t, err)
		require.True(t, enabled)
	})

	t.Run("does not allow a disabled organization", func(t *testing.T) {
		t.Parallel()

		enabled, err := NewCatalogRegistrationGate(newMainGate(false, nil)).Enabled(t.Context(), organizationID, projectSlug)

		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("propagates capability errors", func(t *testing.T) {
		t.Parallel()

		enabled, err := NewCatalogRegistrationGate(newMainGate(false, errors.New("unavailable"))).Enabled(t.Context(), organizationID, projectSlug)

		require.ErrorContains(t, err, "check platform mcp gate")
		require.False(t, enabled)
	})

	t.Run("requires explicit project slug", func(t *testing.T) {
		t.Parallel()

		enabled, err := NewCatalogRegistrationGate(newMainGate(true, nil)).Enabled(t.Context(), organizationID, "")

		require.ErrorIs(t, err, ErrUnavailable)
		require.False(t, enabled)
	})
}

func TestOrganizationGateAllowsEnabledPlatformMCPCapability(t *testing.T) {
	t.Parallel()

	enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}).Enabled(t.Context(), "organization-1")

	require.NoError(t, err)
	require.True(t, enabled)
}

func TestOrganizationGateDeniesDisabledPlatformMCPCapability(t *testing.T) {
	t.Parallel()

	enabled, err := NewOrganizationGate(testCapabilityChecker{}).Enabled(t.Context(), "organization-1")

	require.NoError(t, err)
	require.False(t, enabled)
}

func TestOrganizationGateFailsClosedWhenCapabilityLookupIsUnavailable(t *testing.T) {
	t.Parallel()

	enabled, err := NewOrganizationGate(testCapabilityChecker{err: errors.New("unavailable")}).Enabled(t.Context(), "organization-1")

	require.ErrorContains(t, err, "check platform mcp capability")
	require.False(t, enabled)
}

func TestOrganizationGateRequiresOrganization(t *testing.T) {
	t.Parallel()

	enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}).Enabled(t.Context(), "")

	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, enabled)
}
