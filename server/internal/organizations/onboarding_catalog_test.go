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
