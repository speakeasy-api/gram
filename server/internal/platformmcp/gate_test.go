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

type testOrganizationSlugResolver struct {
	slug string
	err  error
}

type recordingRolloutProvider struct {
	groups map[string]string
}

func (r testOrganizationSlugResolver) OrganizationSlug(_ context.Context, _ string) (string, error) {
	return r.slug, r.err
}

func (r *recordingRolloutProvider) IsFlagEnabled(_ context.Context, _ feature.Flag, _ string, groups map[string]string) (bool, error) {
	r.groups = groups
	return true, nil
}

func (r *recordingRolloutProvider) IsFlagEnabledLocal(ctx context.Context, flag feature.Flag, distinctID string, groups, _ map[string]string) (bool, error) {
	return r.IsFlagEnabled(ctx, flag, distinctID, groups)
}

func (r *recordingRolloutProvider) FlagPayload(_ context.Context, _ feature.Flag, _ string, _ map[string]string) ([]byte, error) {
	return nil, nil
}

func (r *recordingRolloutProvider) EvaluateFlag(_ context.Context, _ feature.Flag, _ string, groups map[string]string) (feature.Evaluation, error) {
	r.groups = groups
	return feature.EvaluationEnabled, nil
}

func (c testCapabilityChecker) IsFeatureEnabled(_ context.Context, _ string, capability productfeatures.Feature) (bool, error) {
	if capability != productfeatures.FeaturePlatformMCP {
		return false, errors.New("unexpected capability")
	}
	return c.enabled, c.err
}

func TestOrganizationGateRequiresBothGates(t *testing.T) {
	t.Parallel()

	rollout := &feature.InMemory{}
	rollout.SetFlag(feature.FlagPlatformMCPRollout, "organization-1", true)
	resolver := testOrganizationSlugResolver{slug: "organization-slug"}

	t.Run("allows both enabled", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout, resolver).Enabled(t.Context(), "organization-1")
		require.NoError(t, err)
		require.True(t, enabled)
	})

	t.Run("denies missing capability", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{}, rollout, resolver).Enabled(t.Context(), "organization-1")
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("denies rollout disabled", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout, resolver).Enabled(t.Context(), "organization-2")
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("returns capability errors", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{err: errors.New("unavailable")}, rollout, resolver).Enabled(t.Context(), "organization-1")
		require.Error(t, err)
		require.False(t, enabled)
	})

	t.Run("fails closed when resolving the organization slug fails", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout, testOrganizationSlugResolver{err: errors.New("unavailable")}).Enabled(t.Context(), "organization-1")
		require.Error(t, err)
		require.False(t, enabled)
	})

	t.Run("fails closed when the organization slug is empty", func(t *testing.T) {
		t.Parallel()
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout, testOrganizationSlugResolver{}).Enabled(t.Context(), "organization-1")
		require.ErrorIs(t, err, ErrUnavailable)
		require.False(t, enabled)
	})

	t.Run("passes organization slug as rollout group", func(t *testing.T) {
		t.Parallel()
		rollout := &recordingRolloutProvider{}
		enabled, err := NewOrganizationGate(testCapabilityChecker{enabled: true}, rollout, resolver).Enabled(t.Context(), "organization-1")
		require.NoError(t, err)
		require.True(t, enabled)
		require.Equal(t, map[string]string{"organization": "organization-slug"}, rollout.groups)
	})
}
