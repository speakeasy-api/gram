package repo

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchQueriesUseScopedBoundedPrefixRanges(t *testing.T) {
	t.Parallel()

	queries := []string{listCommandIndexCustomRules, listCommandIndexEnvironments, listCommandIndexGateways, listCommandIndexPlugins}
	for _, query := range queries {
		q := strings.ToLower(query)
		require.Contains(t, q, "project_id")
		require.Contains(t, q, "limit")
		require.Contains(t, q, " >= ")
		require.Contains(t, q, " < ")
		require.Contains(t, q, "lower(")
		require.NotContains(t, q, "count(")
		require.NotContains(t, q, " like ")
		require.NotContains(t, q, "ilike")
		require.NotContains(t, q, "%")
	}
}

func TestSearchQuerySourceUsesOneNormalizedRange(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("../queries.sql")
	require.NoError(t, err)
	q := string(source)
	require.Contains(t, q, "@lower_bound")
	require.Contains(t, q, "@lower_ceiling")
	for _, stale := range []string{"@titlecase_bound", "@uppercase_bound", "ListCommandIndexAssistants", "ListCommandIndexMCPServers", "ListCommandIndexSkills"} {
		require.NotContains(t, q, stale)
	}
}
