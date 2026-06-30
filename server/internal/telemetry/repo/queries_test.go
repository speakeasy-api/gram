package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAttributeColumn_AtPrefixUserAttribute(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("@user.region")
	require.Equal(t, "toString(attributes.app.user.region)", got)
}

func TestResolveAttributeColumn_FallbackJSONAccessor(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("http.route")
	require.Equal(t, "toString(attributes.http.route)", got)
}

func TestResolveAttributeColumn_MaterializedConversationID(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("gen_ai.conversation.id")
	require.Equal(t, "chat_id", got)
}

func TestResolveAttributeColumn_MaterializedToolURN(t *testing.T) {
	t.Parallel()
	got := resolveAttributeColumn("gram.tool.urn")
	require.Equal(t, "urn", got)
}

func TestToolUsageTraceRowsCTE_UsesDeterministicStatusAggregation(t *testing.T) {
	t.Parallel()

	sql, _, err := toolUsageTraceRowsCTE(ListToolUsageTracesParams{})
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(sql), "any(http_status_code)")
	require.NotContains(t, strings.ToLower(sql), "any(hook_status)")
	require.Contains(t, sql, "max(hook_status_rank)")
}

func TestToolUsageFilteredSelect_BuildsWithHostedMCPMatchers(t *testing.T) {
	t.Parallel()

	sb, err := toolUsageFilteredSelect(GetToolUsageSummaryParams{
		GramProjectID: "project-id",
		TimeStart:     1,
		TimeEnd:       2,
		BucketSizeNs:  0,
		HostedMCPMatchers: []HostedMCPMatcher{
			{ToolsetSlug: "payments", McpSlug: "payments-mcp"},
		},
		MCPServerMatchers:  nil,
		TargetTypes:        []string{ToolUsageTargetTypeHostedMCP},
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		UserFilters:        nil,
		HookSources:        nil,
		TargetLimit:        0,
		UserLimit:          0,
		UsersByTargetLimit: 0,
		TargetToolRowLimit: 0,
		TimeSeriesRowLimit: 0,
		UserSeriesRowLimit: 0,
	}, "count() AS event_count")
	require.NoError(t, err)

	sql, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Len(t, args, strings.Count(sql, "?"), "sql: %s\nargs: %#v", sql, args)
}

func TestToolUsageFilteredSelect_BuildsWithMCPServerMatchers(t *testing.T) {
	t.Parallel()

	sb, err := toolUsageFilteredSelect(GetToolUsageSummaryParams{
		GramProjectID:     "project-id",
		TimeStart:         1,
		TimeEnd:           2,
		BucketSizeNs:      0,
		HostedMCPMatchers: nil,
		MCPServerMatchers: []MCPServerMatcher{
			{
				SourceID:    "source-id",
				TargetType:  ToolUsageTargetTypeTunneledMCP,
				TargetID:    "postgres-tunnel",
				TargetLabel: "Tunneled Postgres MCP",
			},
		},
		TargetTypes:        []string{ToolUsageTargetTypeTunneledMCP},
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		UserFilters:        nil,
		HookSources:        nil,
		TargetLimit:        0,
		UserLimit:          0,
		UsersByTargetLimit: 0,
		TargetToolRowLimit: 0,
		TimeSeriesRowLimit: 0,
		UserSeriesRowLimit: 0,
	}, "count() AS event_count")
	require.NoError(t, err)

	sql, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Len(t, args, strings.Count(sql, "?"), "sql: %s\nargs: %#v", sql, args)
}

// Array dimensions group an empty array under the "" ("(unset)") bucket, so the
// filter for "" must test array emptiness — hasAny never matches an empty array.
func TestArrayDimFilter_UnsetMatchesEmptyArray(t *testing.T) {
	t.Parallel()

	sql, args, err := arrayDimFilter("groups", []string{""}).ToSql()
	require.NoError(t, err)
	require.Equal(t, "empty(groups)", sql)
	require.Empty(t, args)
}

func TestArrayDimFilter_NonEmptyUsesHasAny(t *testing.T) {
	t.Parallel()

	sql, args, err := arrayDimFilter("groups", []string{"eng", "design"}).ToSql()
	require.NoError(t, err)
	require.Equal(t, "hasAny(groups, ?)", sql)
	require.Equal(t, []any{[]string{"eng", "design"}}, args)
}

func TestArrayDimFilter_MixedCombinesWithOr(t *testing.T) {
	t.Parallel()

	sql, args, err := arrayDimFilter("groups", []string{"eng", ""}).ToSql()
	require.NoError(t, err)
	require.Equal(t, "(hasAny(groups, ?) OR empty(groups))", sql)
	require.Equal(t, []any{[]string{"eng"}}, args)
}
