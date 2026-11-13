package repo

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexQueriesAreCompleteScopedSets(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../queries.sql")
	require.NoError(t, err)
	q := strings.ToLower(string(source))
	require.Equal(t, 1, strings.Count(q, "limit 1"), "only the designated-host lookup may be bounded")
	require.NotContains(t, q, "lower_bound")
	require.NotContains(t, q, "lower_ceiling")
	for _, table := range []string{"assistants", "environments", "meta_mcp_servers", "mcp_servers", "plugins", "risk_custom_detection_rules", "skills"} {
		require.Contains(t, q, table)
	}
	require.GreaterOrEqual(t, strings.Count(q, "project_id = @project_id"), 7)
	require.GreaterOrEqual(t, strings.Count(q, "deleted is false"), 9, "all soft-deletable resources and MCP hosts stay live-only")
	require.Contains(t, q, "archived_at is null", "skills stay lifecycle-filtered")
	require.GreaterOrEqual(t, strings.Count(q, "organization_id = @organization_id"), 3, "org-owned resources retain tenant scope")
}

func TestMCPIndexRequiresDesignatedListableHost(t *testing.T) {
	t.Parallel()
	q := strings.ToLower(listProjectResourceIndexMCPServers)
	require.Contains(t, q, "join lateral")
	require.NotContains(t, q, "left join lateral")
	require.Contains(t, q, "ms.id = t.id or exists")
	require.Contains(t, q, "mcp_endpoints")
	require.Contains(t, q, "order by (ms.id = t.id) desc, ms.created_at, ms.id")
	require.Contains(t, q, "ms.builtin_backend is null")
	require.Contains(t, q, "ms.toolset_id is null")
}

func TestSearchOnlyExpressionIndexesWereRemoved(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../database/schema.sql")
	require.NoError(t, err)
	q := strings.ToLower(string(schema))
	for _, index := range []string{"assistants_project_id_lower_name_idx", "mcp_servers_project_id_lower_name_listable_idx", "toolsets_project_id_lower_name_idx", "skills_project_id_lower_display_name_idx", "plugins_organization_project_lower_name_idx", "environments_project_id_lower_name_idx", "meta_mcp_servers_organization_project_lower_name_idx", "risk_custom_rules_org_project_lower_title_idx"} {
		require.NotContains(t, q, index)
	}
}
