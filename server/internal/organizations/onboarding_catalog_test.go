package organizations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Presets and the card catalog are edited by hand; this keeps them in step.
func TestOnboardingPresetsUseCatalogTasks(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, preset := range onboardingPresets {
		require.NotEmpty(t, preset.Key)
		require.NotEmpty(t, preset.Title, preset.Key)
		require.False(t, seen[preset.Key], "duplicate preset %q", preset.Key)
		seen[preset.Key] = true
		for _, key := range preset.TaskKeys {
			require.NotNil(t, setupTaskDefinitionForKey(key), "preset %q names unknown task %q", preset.Key, key)
		}
	}
}

func TestSetupTaskCatalogKeysAreUniqueAndPrerequisitesExist(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, task := range setupTaskCatalog {
		require.False(t, seen[task.Key], "duplicate task %q", task.Key)
		seen[task.Key] = true
		for _, prerequisite := range task.Prerequisites {
			require.NotNil(t, setupTaskDefinitionForKey(prerequisite), "task %q requires unknown task %q", task.Key, prerequisite)
		}
	}
}

func TestIdentitySetupUsesOneCombinedTask(t *testing.T) {
	t.Parallel()
	identity := setupTaskDefinitionForKey("identity-provider")
	require.NotNil(t, identity)
	require.Empty(t, identity.Prerequisites, "domain verification is a nested step, not a task dependency")
	require.Equal(t, "identity-provider", setupTaskCatalog[0].Key)
	security := onboardingPresetByKey("security")
	require.NotNil(t, security)
	require.Contains(t, security.TaskKeys, "identity-provider")
	for _, retired := range []string{"domain-verification", "connect-idp", "directory-sync"} {
		require.Nil(t, setupTaskDefinitionForKey(retired))
		require.NotContains(t, security.TaskKeys, retired)
	}
}
