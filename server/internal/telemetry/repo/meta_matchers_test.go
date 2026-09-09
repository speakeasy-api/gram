package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolUsageMetaMCPMatcherArrays_KeepsAnchoredFlagsAligned(t *testing.T) {
	t.Parallel()
	suffixes, ids, labels, anchored := toolUsageMetaMCPMatcherArrays([]MetaMCPMatcher{
		{URLSuffix: "://mcp.example.com/mcp/foo", TargetID: "gw-1", TargetLabel: "Domain", HostAnchored: true},
		{URLSuffix: "", TargetID: "gw-x", TargetLabel: "Dropped", HostAnchored: true},
		{URLSuffix: "/mcp/foo", TargetID: "gw-2", TargetLabel: "", HostAnchored: false},
	})
	require.Equal(t, []string{"://mcp.example.com/mcp/foo", "/mcp/foo"}, suffixes)
	require.Equal(t, []string{"gw-1", "gw-2"}, ids)
	require.Equal(t, []string{"Domain", "gw-2"}, labels, "a missing label falls back to the id")
	require.Equal(t, []uint8{1, 0}, anchored)
}

func TestToolUsageFilteredSelect_BindsMetaMatcherArgsInPlaceholderOrder(t *testing.T) {
	t.Parallel()

	sb, err := toolUsageFilteredSelect(GetToolUsageSummaryParams{
		GramProjectID: "project-id",
		TimeStart:     1,
		TimeEnd:       2,
		BucketSizeNs:  0,
		HostedMCPMatchers: []HostedMCPMatcher{
			{ToolsetSlug: "payments", ToolsetName: "Payments", McpSlug: "payments-mcp"},
		},
		MCPServerMatchers: []MCPServerMatcher{
			{SourceID: "source-id", TargetType: ToolUsageTargetTypeTunneledMCP, TargetID: "postgres-tunnel", TargetLabel: "Tunneled Postgres MCP"},
		},
		MetaMCPMatchers: []MetaMCPMatcher{
			{URLSuffix: "://mcp.example.com/mcp/foo", TargetID: "gw-1", TargetLabel: "Domain", HostAnchored: true},
			{URLSuffix: "/mcp/bar", TargetID: "gw-2", TargetLabel: "Platform", HostAnchored: false},
		},
		TargetTypes:        nil,
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		MetaMCPServerIDs:   []string{"gw-1"},
		UserFilters:        nil,
		HookSources:        nil,
		AccountType:        "",
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

	// In the hook classifier the host-anchored gateway branch precedes the
	// hosted URL branch and the bare gateway branch follows it.
	anchoredAt := strings.Index(sql, "meta_mcp_match_anchored = 1")
	hostedAt := strings.Index(sql, "hosted_match_index > 0")
	bareAt := strings.Index(sql, "meta_mcp_match_index > 0")
	require.Greater(t, anchoredAt, -1)
	require.Greater(t, hostedAt, anchoredAt)
	require.Greater(t, bareAt, hostedAt)
	require.Contains(t, sql, "meta_mcp_server_id IN (?)")
}

func TestListToolUsageTracesCTE_BindsMetaMatcherArgsOnBothPaths(t *testing.T) {
	t.Parallel()

	base := ListToolUsageTracesParams{
		GramProjectID: "project-id",
		TimeStart:     1,
		TimeEnd:       2,
		HostedMCPMatchers: []HostedMCPMatcher{
			{ToolsetSlug: "payments", ToolsetName: "Payments", McpSlug: "payments-mcp"},
		},
		MCPServerMatchers: nil,
		MetaMCPMatchers: []MetaMCPMatcher{
			{URLSuffix: "://mcp.example.com/mcp/foo", TargetID: "gw-1", TargetLabel: "Domain", HostAnchored: true},
		},
		TargetTypes:        nil,
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		MetaMCPServerIDs:   nil,
		UserFilters:        nil,
		HookSources:        nil,
		AccountType:        "",
		Statuses:           nil,
		Query:              "",
		Filters:            nil,
		SortOrder:          "desc",
		CursorTimeUnixNano: 0,
		CursorID:           "",
		Limit:              10,
	}
	for name, params := range map[string]ListToolUsageTracesParams{
		"summaries": base,
		"raw":       func() ListToolUsageTracesParams { p := base; p.Query = "conv"; return p }(),
	} {
		sql, args, err := toolUsageTraceRowsCTE(params)
		require.NoError(t, err, name)
		require.Len(t, args, strings.Count(sql, "?"), "%s sql: %s\nargs: %#v", name, sql, args)
		require.Contains(t, sql, "meta_mcp_match_anchored", name)
		require.Contains(t, sql, "meta_mcp_server_id", name)
	}
}
