package authz

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every workflow scope is one a person can be granted: catalogued, visible
// and not implied by another scope in the same workflow.
func TestWorkflows_StayInsideTheCatalogue(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, workflow := range Workflows() {
		require.NotEmpty(t, workflow.Slug)
		require.False(t, seen[workflow.Slug], "workflow %q listed twice", workflow.Slug)
		seen[workflow.Slug] = true
		require.NotEmpty(t, workflow.Label, workflow.Slug)
		require.False(t, strings.HasSuffix(workflow.Label, "."), "%s: a label is not a sentence", workflow.Slug)
		require.GreaterOrEqual(t, len(workflow.Scopes), 2, "%s: a workflow with one scope is just that scope", workflow.Slug)

		for _, scope := range workflow.Scopes {
			entry, ok := ScopeEntryFor(scope)
			require.True(t, ok, "%s: %s is not catalogued", workflow.Slug, scope)
			visibility, ok := ScopeVisibilityFor(scope)
			require.True(t, ok)
			require.Equal(t, ScopeVisibilityUserVisible, visibility, "%s: %s is not grantable by a person", workflow.Slug, entry.Scope)

			for _, other := range workflow.Scopes {
				if other == scope {
					continue
				}
				require.NotContains(t, CalculateSubScopes(other), string(scope),
					"%s: %s already includes %s", workflow.Slug, other, scope)
			}
		}
	}
}

func TestWorkflows_IsACopy(t *testing.T) {
	t.Parallel()

	first := Workflows()
	first[0].Scopes[0] = ScopeRoot
	first[0].Label = "changed"
	second := Workflows()
	require.NotEqual(t, ScopeRoot, second[0].Scopes[0])
	require.NotEqual(t, "changed", second[0].Label)
}

func TestWorkflowsNeeding(t *testing.T) {
	t.Parallel()

	slugs := func(workflows []Workflow) []string {
		out := make([]string, 0, len(workflows))
		for _, workflow := range workflows {
			out = append(out, workflow.Slug)
		}
		slices.Sort(out)
		return out
	}

	require.Equal(t, []string{"agent_setup", "mcp_maintenance"}, slugs(WorkflowsNeeding(ScopeEnvironmentRead)))
	require.Equal(t, []string{"session_review"}, slugs(WorkflowsNeeding(ScopeLogRead)))
	require.Empty(t, WorkflowsNeeding(ScopeOrgAdmin))
}
