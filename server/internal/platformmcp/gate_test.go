package platformmcp

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
	if capability != productfeatures.FeaturePlatformMCP {
		return false, errors.New("unexpected capability")
	}
	return c.enabled, c.err
}

func (c testCapabilityChecker) IsFeatureEnabledUncached(ctx context.Context, organizationID string, capability productfeatures.Feature) (bool, error) {
	return c.IsFeatureEnabled(ctx, organizationID, capability)
}

type testOrganizationSlugResolver struct {
	slug string
	err  error
}

func (r testOrganizationSlugResolver) OrganizationSlug(_ context.Context, _ string) (string, error) {
	return r.slug, r.err
}

type testRolloutProvider struct {
	enabled bool
	err     error
	flag    feature.Flag
	groups  map[string]string
}

func (p *testRolloutProvider) IsFlagEnabled(_ context.Context, flag feature.Flag, _ string, groups map[string]string) (bool, error) {
	p.flag = flag
	p.groups = groups
	return p.enabled, p.err
}

func (p *testRolloutProvider) IsFlagEnabledLocal(ctx context.Context, flag feature.Flag, distinctID string, groups, _ map[string]string) (bool, error) {
	return p.IsFlagEnabled(ctx, flag, distinctID, groups)
}

func (p *testRolloutProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
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

func TestOrganizationGateRequiresPlatformMCPRolloutAndCapability(t *testing.T) {
	t.Parallel()

	const organizationID = "organization-1"
	const organizationSlug = "organization-slug"

	newGate := func(capability testCapabilityChecker, rollout *testRolloutProvider, resolver testOrganizationSlugResolver) *OrganizationGate {
		return NewOrganizationGate(capability, rollout, resolver)
	}

	t.Run("allows an organization with both rollout and capability enabled", func(t *testing.T) {
		t.Parallel()

		rollout := &testRolloutProvider{enabled: true}
		enabled, err := newGate(testCapabilityChecker{enabled: true}, rollout, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), organizationID)

		require.NoError(t, err)
		require.True(t, enabled)
		require.Equal(t, feature.FlagPlatformMCP, rollout.flag)
		require.Equal(t, feature.OrgProjectGroups(organizationSlug, ""), rollout.groups)
	})

	t.Run("denies when the rollout is disabled", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{enabled: true}, &testRolloutProvider{}, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), organizationID)

		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("denies when the organization capability is disabled", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{}, &testRolloutProvider{enabled: true}, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), organizationID)

		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("fails closed when rollout evaluation is unavailable", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{enabled: true}, &testRolloutProvider{err: errors.New("unavailable")}, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), organizationID)

		require.ErrorContains(t, err, "check platform mcp rollout")
		require.False(t, enabled)
	})

	t.Run("fails closed when the organization capability lookup is unavailable", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{err: errors.New("unavailable")}, &testRolloutProvider{enabled: true}, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), organizationID)

		require.ErrorContains(t, err, "check platform mcp capability")
		require.False(t, enabled)
	})

	t.Run("fails closed when the organization cannot be resolved", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{enabled: true}, &testRolloutProvider{enabled: true}, testOrganizationSlugResolver{err: errors.New("unavailable")}).Enabled(t.Context(), organizationID)

		require.ErrorContains(t, err, "resolve organization for platform mcp rollout")
		require.False(t, enabled)
	})

	t.Run("fails closed when the organization slug is empty", func(t *testing.T) {
		t.Parallel()

		rollout := &testRolloutProvider{enabled: true}
		enabled, err := newGate(testCapabilityChecker{enabled: true}, rollout, testOrganizationSlugResolver{}).Enabled(t.Context(), organizationID)

		require.ErrorIs(t, err, ErrUnavailable)
		require.False(t, enabled)
		require.Zero(t, rollout.flag)
	})

	t.Run("requires an organization", func(t *testing.T) {
		t.Parallel()

		enabled, err := newGate(testCapabilityChecker{enabled: true}, &testRolloutProvider{enabled: true}, testOrganizationSlugResolver{slug: organizationSlug}).Enabled(t.Context(), "")

		require.ErrorIs(t, err, ErrUnavailable)
		require.False(t, enabled)
	})
}
