package organizations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func TestDefaultOnboardingSelectionOnlyHidesLiteLLM(t *testing.T) {
	t.Parallel()

	for _, task := range setupTaskCatalog {
		require.Equal(t, task.Key == "litellm", task.HiddenByDefault, task.Key)
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
func TestSetupTaskCatalogGroupsAreWellFormed(t *testing.T) {
	t.Parallel()

	groups := map[string]bool{}
	for _, group := range setupTaskGroups {
		require.NotEmpty(t, group.Key)
		require.NotEmpty(t, group.Title, group.Key)
		require.False(t, groups[group.Key], "duplicate group %q", group.Key)
		require.Nil(t, setupTaskDefinitionForKey(group.Key), "group %q collides with a card", group.Key)
		groups[group.Key] = true
	}

	// Every parent names a group, and a group's cards sit together so the
	// group can take the place of its first card in the wizard.
	closed := map[string]bool{}
	previous := ""
	for _, card := range setupTaskCatalog {
		if card.Parent != "" {
			require.True(t, groups[card.Parent], "card %q names unknown group %q", card.Key, card.Parent)
			require.False(t, closed[card.Parent], "card %q is separated from the other cards of %q", card.Key, card.Parent)
		}
		if previous != "" && previous != card.Parent {
			closed[previous] = true
		}
		previous = card.Parent
		switch card.Completion {
		case setupTaskCompletionManual, setupTaskCompletionFact:
		default:
			require.Failf(t, "invalid completion", "card %q completes by %q; only cards complete by manual or fact", card.Key, card.Completion)
		}
	}
	for key := range groups {
		require.True(t, slices.ContainsFunc(setupTaskCatalog, func(card setupTaskDefinition) bool { return card.Parent == key }), "group %q has no cards", key)
	}
}

func TestSetupTaskCatalogDependenciesStayOffTheirOwnLine(t *testing.T) {
	t.Parallel()

	for _, card := range setupTaskCatalog {
		for _, prerequisite := range card.Prerequisites {
			require.Nil(t, setupTaskGroupForKey(prerequisite), "card %q requires group %q; require its cards instead", card.Key, prerequisite)
			require.NotEqual(t, card.Key, prerequisite, "card %q requires itself", card.Key)
		}
	}
}

func TestSetupTaskCatalogMethodsExistInTheSupportMatrix(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "admin", "supportmatrix", "catalog.json"))
	require.NoError(t, err)
	var catalog struct {
		Methods []struct {
			ID string `json:"id"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &catalog))
	known := make(map[string]bool, len(catalog.Methods))
	for _, method := range catalog.Methods {
		known[method.ID] = true
	}
	for _, card := range setupTaskCatalog {
		for _, method := range card.Methods {
			require.True(t, known[method], "card %q names integration method %q that the support matrix does not define", card.Key, method)
		}
	}
}

func TestOnboardingStepRecordsPlaceGroupsBeforeTheirCards(t *testing.T) {
	t.Parallel()

	records := onboardingStepRecords()
	require.Len(t, records, len(setupTaskCatalog)+len(setupTaskGroups))
	position := make(map[string]int, len(records))
	for index, record := range records {
		position[record.Slug] = index
	}
	for _, record := range records {
		if record.Parent == "" {
			continue
		}
		require.Less(t, position[record.Parent], position[record.Slug], "group %q must precede %q", record.Parent, record.Slug)
		require.Equal(t, setupTaskCompletionChildren, records[position[record.Parent]].Completion)
	}
	// The agent observability group is visible by default because a card of
	// its is; the distribution group hides with all of its cards.
	require.False(t, records[position["agent-observability"]].HiddenByDefault)
	require.True(t, records[position["mcp-distribution"]].HiddenByDefault)
}
