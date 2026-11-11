package authz

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every task scope is one a person can be granted: catalogued, visible
// and not implied by another scope in the same task.
func TestTasks_StayInsideTheCatalogue(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, task := range Tasks() {
		require.NotEmpty(t, task.Slug)
		require.False(t, seen[task.Slug], "task %q listed twice", task.Slug)
		seen[task.Slug] = true
		require.NotEmpty(t, task.Label, task.Slug)
		require.False(t, strings.HasSuffix(task.Label, "."), "%s: a label is not a sentence", task.Slug)
		require.GreaterOrEqual(t, len(task.Scopes), 2, "%s: a task with one scope is just that scope", task.Slug)

		for _, scope := range task.Scopes {
			entry, ok := ScopeEntryFor(scope)
			require.True(t, ok, "%s: %s is not catalogued", task.Slug, scope)
			visibility, ok := scope.Visibility()
			require.True(t, ok)
			require.Equal(t, ScopeVisibilityUserVisible, visibility, "%s: %s is not grantable by a person", task.Slug, entry.Scope)

			for _, other := range task.Scopes {
				if other == scope {
					continue
				}
				require.NotContains(t, other.ImpliedScopes(), scope,
					"%s: %s already includes %s", task.Slug, other, scope)
			}
		}
	}
}

func TestTasks_IsACopy(t *testing.T) {
	t.Parallel()

	first := Tasks()
	first[0].Scopes[0] = ScopeRoot
	first[0].Label = "changed"
	second := Tasks()
	require.NotEqual(t, ScopeRoot, second[0].Scopes[0])
	require.NotEqual(t, "changed", second[0].Label)
}

func TestTasksNeeding(t *testing.T) {
	t.Parallel()

	slugs := func(tasks []ScopeTask) []string {
		out := make([]string, 0, len(tasks))
		for _, task := range tasks {
			out = append(out, task.Slug)
		}
		slices.Sort(out)
		return out
	}

	require.Equal(t, []string{"agent_setup", "mcp_maintenance"}, slugs(TasksNeeding(ScopeEnvironmentRead)))
	require.Equal(t, []string{"session_review"}, slugs(TasksNeeding(ScopeLogRead)))
	require.Empty(t, TasksNeeding(ScopeOrgAdmin))
}
