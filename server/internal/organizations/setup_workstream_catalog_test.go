package organizations

import (
	"slices"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/stretchr/testify/require"
)

func TestSetupWorkstreamMembershipPartitionsCatalog(t *testing.T) {
	t.Parallel()
	views := setupWorkstreamViews()
	require.Len(t, views, len(setupWorkstreams))
	seenIDs := map[string]bool{}
	seen := map[string]bool{}
	for i, workstream := range setupWorkstreams {
		require.NotEmpty(t, workstream.ID)
		require.NotEmpty(t, workstream.Title)
		require.NotEmpty(t, workstream.TaskKeys)
		require.False(t, seenIDs[workstream.ID], "workstream IDs must be unique")
		seenIDs[workstream.ID] = true
		require.Same(t, &setupWorkstreams[i], setupWorkstreamForID(workstream.ID), "assignment uses the canonical catalog")
		require.Equal(t, workstream.ID, views[i].ID)
		require.Equal(t, workstream.Title, views[i].Title)
		require.Equal(t, workstream.TaskKeys, views[i].TaskKeys, "API preserves task order, including hidden tasks")
		for _, key := range workstream.TaskKeys {
			require.NotNil(t, setupTaskDefinitionForKey(key))
			require.False(t, seen[key], "task must belong to only one workstream: %s", key)
			seen[key] = true
		}
	}
	require.Len(t, seen, len(setupTaskCatalog), "every task, including hidden tasks, needs a workstream")
	require.Nil(t, setupWorkstreamForID("unknown"))
}

func TestSetupWorkstreamOrderMatchesTaskCatalog(t *testing.T) {
	t.Parallel()
	var workstreamKeys []string
	for _, workstream := range setupWorkstreams {
		workstreamKeys = append(workstreamKeys, workstream.TaskKeys...)
	}
	var catalogKeys []string
	for _, task := range setupTaskCatalog {
		catalogKeys = append(catalogKeys, task.Key)
	}
	require.Equal(t, catalogKeys, workstreamKeys, "wizard and workstreams must use the same task order")
}

func TestSetupWorkstreamViewsFilterMembership(t *testing.T) {
	t.Parallel()
	views := setupWorkstreamViewsForTasks([]*gen.SetupTask{{Key: "directory-sync"}, {Key: "domain-verification"}})
	require.Len(t, views, len(setupWorkstreams))
	require.Equal(t, []string{"domain-verification", "directory-sync"}, views[0].TaskKeys, "catalog order is preserved")
	for _, view := range views[1:] {
		require.Empty(t, view.TaskKeys, "empty workstreams must not disclose hidden members")
		require.NotNil(t, view.TaskKeys, "serialize empty membership as an array")
	}
	require.Equal(t, []string{"domain-verification", "connect-idp", "directory-sync", "identity-provider"}, setupWorkstreams[0].TaskKeys, "filtering must not mutate assignment membership")
}

func TestSetupWorkstreamViewsDoNotAliasCatalog(t *testing.T) {
	t.Parallel()
	title := setupWorkstreams[0].Title
	keys := slices.Clone(setupWorkstreams[0].TaskKeys)
	views := setupWorkstreamViews()
	views[0].Title = "Changed"
	views[0].TaskKeys[0] = "unknown"
	fresh := setupWorkstreamViews()
	require.Equal(t, title, setupWorkstreams[0].Title)
	require.Equal(t, keys, setupWorkstreams[0].TaskKeys)
	require.Equal(t, title, fresh[0].Title)
	require.Equal(t, keys, fresh[0].TaskKeys)
}

// The catalog is the only source of task keys, so every reference into it —
// preset selections and prerequisite edges — must resolve, and the prerequisite
// graph must stay acyclic or projectSetupTasks would block tasks forever.
func TestSetupTaskCatalogReferencesResolve(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, definition := range setupTaskCatalog {
		require.NotEmpty(t, definition.Key)
		require.NotEmpty(t, definition.Title)
		require.NotEmpty(t, definition.Description)
		require.False(t, seen[definition.Key], "task keys must be unique: %s", definition.Key)
		seen[definition.Key] = true
		for _, prerequisite := range definition.Prerequisites {
			require.NotEqual(t, definition.Key, prerequisite, "a task cannot require itself: %s", definition.Key)
			require.NotNil(t, setupTaskDefinitionForKey(prerequisite), "unknown prerequisite %q on %q", prerequisite, definition.Key)
		}
	}
	for _, preset := range adminOnboardingPresets() {
		require.NotEmpty(t, preset.Key)
		require.NotEmpty(t, preset.VisibleTaskKeys)
		presetKeys := map[string]bool{}
		for _, key := range preset.VisibleTaskKeys {
			require.NotNil(t, setupTaskDefinitionForKey(key), "unknown task %q in preset %q", key, preset.Key)
			require.False(t, presetKeys[key], "preset %q repeats task %q", preset.Key, key)
			presetKeys[key] = true
		}
	}
}

func TestSetupTaskPrerequisitesAreAcyclic(t *testing.T) {
	t.Parallel()
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var walk func(key string, path []string)
	walk = func(key string, path []string) {
		switch state[key] {
		case done:
			return
		case onStack:
			require.Failf(t, "prerequisite cycle", "cycle through %v back to %s", path, key)
			return
		}
		state[key] = onStack
		definition := setupTaskDefinitionForKey(key)
		require.NotNil(t, definition)
		for _, prerequisite := range definition.Prerequisites {
			walk(prerequisite, append(path, key))
		}
		state[key] = done
	}
	for _, definition := range setupTaskCatalog {
		walk(definition.Key, nil)
	}
}

func TestSetupTaskProgressMetadataIsExplicit(t *testing.T) {
	t.Parallel()
	optional := []string{}
	for _, definition := range setupTaskCatalog {
		if definition.Optional {
			optional = append(optional, definition.Key)
		}
	}
	require.Equal(t, []string{"platform-mcp"}, optional, "changing which tasks are optional changes every progress count")
}
