package feature_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

const testFeatureFlag feature.Flag = "test-feature"

func TestEvaluateFlagInMemory(t *testing.T) {
	t.Parallel()

	provider := &feature.InMemory{}
	provider.SetFlag(testFeatureFlag, "enabled", true)
	provider.SetFlag(testFeatureFlag, "disabled", false)

	enabled, err := feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "enabled", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, enabled)

	disabled, err := feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "disabled", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationDisabled, disabled)

	missing, err := feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "missing", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, missing)
}

func TestInMemoryAnyDistinctIDFallback(t *testing.T) {
	t.Parallel()

	provider := &feature.InMemory{}
	provider.SetFlag(testFeatureFlag, feature.AnyDistinctID, true)
	provider.SetFlag(testFeatureFlag, "opted-out", false)

	enabled, err := provider.IsFlagEnabled(t.Context(), testFeatureFlag, "anyone", nil)
	require.NoError(t, err)
	require.True(t, enabled)

	evaluation, err := feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "anyone", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, evaluation)

	evaluation, err = feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "opted-out", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationDisabled, evaluation)
}

func TestEvaluateFlagLegacyProviderIsIndeterminate(t *testing.T) {
	t.Parallel()

	result, err := feature.EvaluateFlag(t.Context(), legacyProvider{}, testFeatureFlag, "organization", nil, nil)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, result)
}

func TestEvaluateFlagForwardsGroupsAndPersonProperties(t *testing.T) {
	t.Parallel()

	provider := &recordingEvaluator{}
	groups := feature.OrgProjectGroups("org", "project")
	person := map[string]string{"email": "person@example.com"}

	result, err := feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "person@example.com", groups, person)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationEnabled, result)
	require.Equal(t, "person@example.com", provider.distinctID)
	require.Equal(t, groups, provider.groups)
	require.Equal(t, person, provider.personProperties)

	_, err = feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "organization", groups, nil)
	require.NoError(t, err)
	require.Nil(t, provider.personProperties)

	provider.err = errors.New("unavailable")
	result, err = feature.EvaluateFlag(t.Context(), provider, testFeatureFlag, "organization", groups, nil)
	require.Error(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, result)

	result, err = feature.EvaluateFlag(t.Context(), nil, testFeatureFlag, "organization", groups, person)
	require.NoError(t, err)
	require.Equal(t, feature.EvaluationIndeterminate, result)
}

type recordingEvaluator struct {
	legacyProvider
	err              error
	distinctID       string
	groups           map[string]string
	personProperties map[string]string
}

func (r *recordingEvaluator) EvaluateFlag(_ context.Context, _ feature.Flag, distinctID string, groups, personProperties map[string]string) (feature.Evaluation, error) {
	r.distinctID, r.groups, r.personProperties = distinctID, groups, personProperties
	if r.err != nil {
		return feature.EvaluationIndeterminate, r.err
	}
	return feature.EvaluationEnabled, nil
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

func TestFlagVariantLocal(t *testing.T) {
	t.Parallel()

	provider := &feature.InMemory{}
	provider.SetFlagVariant(testFeatureFlag, "organization", "treatment")

	variant, err := feature.FlagVariantLocal(t.Context(), provider, testFeatureFlag, "organization", nil, map[string]string{"harness": "codex"})
	require.NoError(t, err)
	require.Equal(t, feature.Variant("treatment"), variant)

	variant, err = feature.FlagVariantLocal(t.Context(), legacyProvider{}, testFeatureFlag, "organization", nil, nil)
	require.NoError(t, err)
	require.Empty(t, variant, "a provider without local variants serves no variant")

	variant, err = feature.FlagVariantLocal(t.Context(), nil, testFeatureFlag, "organization", nil, nil)
	require.NoError(t, err)
	require.Empty(t, variant)
}
