package organizations

import (
	"slices"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
	"github.com/stretchr/testify/require"
)

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
	for _, retired := range []string{"domain-verification", "connect-idp", "directory-sync"} {
		require.Nil(t, setupTaskDefinitionForKey(retired))
		require.Nil(t, setupTaskGroupForKey(retired))
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

// Prerequisites are declared in code, so a cycle would be a catalog bug that
// no card could ever escape; the mirror in onboarding_step_dependencies only
// forbids a step requiring itself.
func TestSetupTaskCatalogDependenciesHaveNoCycles(t *testing.T) {
	t.Parallel()

	requires := make(map[string][]string, len(setupTaskCatalog))
	for _, card := range setupTaskCatalog {
		requires[card.Key] = card.Prerequisites
	}
	var visit func(key string, trail []string)
	visit = func(key string, trail []string) {
		require.NotContains(t, trail, key, "dependency cycle %v", append(slices.Clone(trail), key))
		for _, prerequisite := range requires[key] {
			visit(prerequisite, append(slices.Clone(trail), key))
		}
	}
	for _, card := range setupTaskCatalog {
		visit(card.Key, nil)
	}
}

func TestSetupTaskCatalogMethodsExistInTheSupportMatrix(t *testing.T) {
	t.Parallel()

	matrix, err := supportmatrix.Current()
	require.NoError(t, err)
	known := make(map[string]bool, len(matrix.Methods))
	for _, method := range matrix.Methods {
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
	// A group is visible by default while any of its cards is; every card
	// but LiteLLM is, so both groups are.
	require.False(t, records[position["agent-observability"]].HiddenByDefault)
	require.False(t, records[position["mcp-distribution"]].HiddenByDefault)
}

// The catalog is the wizard's fallback order, so a prerequisite must come
// before every card that needs it.
func TestSetupTaskCatalogListsPrerequisitesFirst(t *testing.T) {
	t.Parallel()

	position := make(map[string]int, len(setupTaskCatalog))
	for index, card := range setupTaskCatalog {
		position[card.Key] = index
	}
	for _, card := range setupTaskCatalog {
		for _, prerequisite := range card.Prerequisites {
			require.Contains(t, position, prerequisite, "card %q needs %q, which the catalog does not define", card.Key, prerequisite)
			require.Less(t, position[prerequisite], position[card.Key], "card %q needs %q before it", card.Key, prerequisite)
		}
	}
}
