package feature_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestEvaluateFlagInMemory(t *testing.T) {
	t.Parallel()

	provider := &feature.InMemory{}
	provider.SetFlag(feature.FlagPlatformMCPRollout, "enabled", true)
	provider.SetFlag(feature.FlagPlatformMCPRollout, "disabled", false)

	enabled, err := feature.EvaluateFlag(t.Context(), provider, feature.FlagPlatformMCPRollout, "enabled", nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, enabled)

	disabled, err := feature.EvaluateFlag(t.Context(), provider, feature.FlagPlatformMCPRollout, "disabled", nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationDisabled, disabled)

	missing, err := feature.EvaluateFlag(t.Context(), provider, feature.FlagPlatformMCPRollout, "missing", nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, missing)
}

func TestEvaluateFlagLegacyProviderIsIndeterminate(t *testing.T) {
	t.Parallel()

	result, err := feature.EvaluateFlag(t.Context(), legacyProvider{}, feature.FlagPlatformMCPRollout, "organization", nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, result)
}

type legacyProvider struct{}

func (legacyProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (legacyProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (legacyProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func TestOrgProjectGroups_OrgAndProject(t *testing.T) {
	t.Parallel()

	got := feature.OrgProjectGroups("speakeasy-team", "default")
	require.Equal(t, map[string]string{
		"organization": "speakeasy-team",
		"slug":         "speakeasy-team/default",
	}, got)
}

func TestOrgProjectGroups_OrgOnly(t *testing.T) {
	t.Parallel()

	got := feature.OrgProjectGroups("speakeasy-team", "")
	require.Equal(t, map[string]string{"organization": "speakeasy-team"}, got)
}

func TestOrgProjectGroups_NoOrgReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, feature.OrgProjectGroups("", "default"))
	require.Nil(t, feature.OrgProjectGroups("", ""))
}
