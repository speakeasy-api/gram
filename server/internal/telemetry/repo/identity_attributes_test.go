package repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIdentityAttributeColumns_ClassifyEveryMaterializedPath is the guard that
// keeps the identity classification from going stale. Callers that must audit a
// read of one person's history refuse the paths IsIdentityAttributePath names,
// and a materialized identity column added later would otherwise become a
// filterable, unaudited way to select an individual. Every materialized path is
// listed here with its verdict, so adding a column forces the decision.
//
// Adding a path: run `mise run clickhouse:gen-materialized-cols`, then set true
// only when a filter on it selects one named person.
func TestIdentityAttributeColumns_ClassifyEveryMaterializedPath(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"gen_ai.conversation.id":        false,
		"gen_ai.evaluation.score.label": false,
		"gram.account_type":             false,
		"gram.api_key.id":               false,
		"gram.billing_mode":             false,
		"gram.event.source":             false,
		"gram.event.urn":                false,
		"gram.external_org_id":          false,
		"gram.external_user.id":         true,
		"gram.function.id":              false,
		"gram.hook.block_reason":        false,
		"gram.hook.source":              false,
		"gram.mcp.client.name":          false,
		"gram.mcp.client.version":       false,
		"gram.mcp_server.id":            false,
		"gram.meta_mcp_server.id":       false,
		"gram.project.id":               false,
		"gram.provider":                 false,
		"gram.remote_mcp_server.id":     false,
		"gram.tool.name":                false,
		"gram.tool.urn":                 false,
		"gram.tool_call.source":         false,
		"gram.toolset.slug":             false,
		"user.email":                    true,
		"user.id":                       true,
	}

	got := make(map[string]bool, len(materializedColumns))
	for path := range materializedColumns {
		got[path] = IsIdentityAttributePath(path)
	}
	require.Equal(t, want, got,
		"a materialized attribute path is unclassified: decide whether a filter on it selects one named person")
}

func TestIsIdentityAttributePath_IgnoresUnmaterializedAndCustomPaths(t *testing.T) {
	t.Parallel()

	// A custom key is project-attached data with no materialized column, and an
	// unrecognized system path has none either, so neither is classified here.
	require.False(t, IsIdentityAttributePath("@user.email"))
	require.False(t, IsIdentityAttributePath("gram.litellm.user_email"))
	require.False(t, IsIdentityAttributePath(""))
}
