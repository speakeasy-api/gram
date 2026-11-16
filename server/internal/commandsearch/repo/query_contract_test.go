package repo

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchQueriesUseScopedBoundedPrefixRanges(t *testing.T) {
	t.Parallel()

	queries := []string{listCommandIndexAssistants, listCommandIndexCustomRules, listCommandIndexEnvironments, listCommandIndexGateways, listCommandIndexMCPServers, listCommandIndexPlugins, listCommandIndexSkills}
	for _, query := range queries {
		q := strings.ToLower(query)
		require.Contains(t, q, "project_id")
		require.Contains(t, q, "limit")
		require.Contains(t, q, " >= ")
		require.Contains(t, q, " < ")
		require.Contains(t, q, "lower(")
		require.Contains(t, q, "order by case")
		require.NotContains(t, q, "count(")
		require.NotContains(t, q, " like ")
		require.NotContains(t, q, "ilike")
		require.NotContains(t, q, "%")
	}
}

func TestMCPSearchRequiresDesignatedListableHost(t *testing.T) {
	t.Parallel()

	q := strings.ToLower(listCommandIndexMCPServers)
	require.Contains(t, q, "join lateral")
	require.NotContains(t, q, "left join lateral")
	require.NotContains(t, q, "coalesce(host.id")
	require.Contains(t, q, "ms.id = t.id or exists")
	require.Contains(t, q, "mcp_endpoints")
	require.Contains(t, q, "order by (ms.id = t.id) desc, ms.created_at, ms.id")
	require.Contains(t, q, "ms.builtin_backend is null")
	require.Contains(t, q, "ms.toolset_id is null")
	require.GreaterOrEqual(t, strings.Count(q, "lower(ms.name) >="), 1)
	require.GreaterOrEqual(t, strings.Count(q, "lower(t.name) >="), 1)
}

func TestSearchExpressionIndexesMatchLifecyclePredicates(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../database/schema.sql")
	require.NoError(t, err)
	q := strings.ToLower(string(schema))
	for _, index := range []string{
		"assistants_project_id_lower_name_idx",
		"mcp_servers_project_id_lower_name_listable_idx",
		"toolsets_project_id_lower_name_idx",
		"skills_project_id_lower_display_name_idx",
	} {
		require.Contains(t, q, index)
	}
	require.Contains(t, q, "on assistants (project_id, lower(name))\nwhere deleted is false")
	require.Contains(t, q, "on toolsets (project_id, lower(name))\nwhere deleted is false")
	require.Contains(t, q, "on skills (project_id, lower(display_name)) where archived_at is null")
}

func TestSearchQuerySourceUsesOneNormalizedRange(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("../queries.sql")
	require.NoError(t, err)
	q := string(source)
	require.Contains(t, q, "@lower_bound")
	require.Contains(t, q, "@lower_ceiling")
	require.Contains(t, q, "slug = @lower_bound")
	require.Contains(t, q, "rule_id = @lower_bound")
	for _, stale := range []string{"@titlecase_bound", "@uppercase_bound"} {
		require.NotContains(t, q, stale)
	}
	for _, required := range []string{"ListCommandIndexAssistants", "ListCommandIndexMCPServers", "ListCommandIndexSkills"} {
		require.Contains(t, q, required)
	}
}
