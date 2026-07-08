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

func TestToolUsageTraceRowsCTE_DoesNotPushStatusFilterToRows(t *testing.T) {
	t.Parallel()

	// A non-empty query forces the raw telemetry_logs path (where row-level filters live).
	base := ListToolUsageTracesParams{GramProjectID: "p", Query: ":"}
	baseSQL, baseArgs, err := toolUsageTraceRowsCTE(base)
	require.NoError(t, err)

	withStatus := base
	withStatus.Filters = []AttributeFilter{
		{Path: "http.response.status_code", Op: "not_eq", Values: []string{"200"}},
	}
	statusSQL, statusArgs, err := toolUsageTraceRowsCTE(withStatus)
	require.NoError(t, err)

	// The status filter must be handled at the aggregated trace level (in
	// ListToolUsageTraces), never pushed into the raw-row CTE — so the CTE is unchanged.
	require.Equal(t, baseSQL, statusSQL)
	require.Equal(t, baseArgs, statusArgs)
}

func TestToolUsageTraceRowsCTE_PushesNonStatusFilterToRows(t *testing.T) {
	t.Parallel()

	base := ListToolUsageTracesParams{GramProjectID: "p", Query: ":"}
	withFilter := base
	withFilter.Filters = []AttributeFilter{
		{Path: "user.email", Op: "eq", Values: []string{"alice@example.com"}},
	}
	sql, args, err := toolUsageTraceRowsCTE(withFilter)
	require.NoError(t, err)

	// A normal attribute filter is still applied at the row level.
	require.Contains(t, sql, "user_email")
	require.Contains(t, args, "alice@example.com")
}

func TestToolUsageStatusPredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filter   AttributeFilter
		wantSQL  string
		wantArgs []any
		wantNil  bool
	}{
		{
			name:     "eq compares numerically",
			filter:   AttributeFilter{Op: "eq", Values: []string{"500"}},
			wantSQL:  "http_status_code = ?",
			wantArgs: []any{int32(500)},
		},
		{
			name:     "not_eq compares numerically",
			filter:   AttributeFilter{Op: "not_eq", Values: []string{"200"}},
			wantSQL:  "http_status_code != ?",
			wantArgs: []any{int32(200)},
		},
		{
			name:     "in builds a set membership",
			filter:   AttributeFilter{Op: "in", Values: []string{"500", "502"}},
			wantSQL:  "http_status_code IN (?,?)",
			wantArgs: []any{int32(500), int32(502)},
		},
		{
			name:    "exists checks non-null",
			filter:  AttributeFilter{Op: "exists"},
			wantSQL: "http_status_code IS NOT NULL",
		},
		{
			name:    "not_exists checks null",
			filter:  AttributeFilter{Op: "not_exists"},
			wantSQL: "http_status_code IS NULL",
		},
		{
			name:     "contains matches on the stringified code",
			filter:   AttributeFilter{Op: "contains", Values: []string{"50"}},
			wantSQL:  "position(toString(http_status_code), ?) > 0",
			wantArgs: []any{"50"},
		},
		{
			name:    "non-numeric value is skipped",
			filter:  AttributeFilter{Op: "eq", Values: []string{"oops"}},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pred := toolUsageStatusPredicate(tt.filter)
			if tt.wantNil {
				require.Nil(t, pred)
				return
			}
			require.NotNil(t, pred)
			sql, args, err := pred.ToSql()
			require.NoError(t, err)
			require.Equal(t, tt.wantSQL, sql)
			require.Equal(t, tt.wantArgs, args)
		})
	}
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
