package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillEfficacyIsInternalButNotCustomerConfigurable(t *testing.T) {
	t.Parallel()

	require.NotContains(t, ModelUsageSources(), ModelUsageSourceSkillEfficacy)
	require.Contains(t, GramHostedHookSourceStrings(), string(ModelUsageSourceSkillEfficacy))
	require.NotContains(t, ModelUsageSources(), ModelUsageSourceSkillSuggestions)
	require.Contains(t, GramHostedHookSourceStrings(), string(ModelUsageSourceSkillSuggestions))
}
