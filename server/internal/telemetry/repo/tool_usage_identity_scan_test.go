package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// An identity filter must reach the trace_summaries scan itself, not just the
// outer select over the grouped rows: the scan groups and classifies every
// trace in the project's window, and only then would the outer filter drop all
// but one agent's. Pushed into the scan, the GROUP BY and classification run
// over that agent's traces alone, and a single-kind filter is a single-column
// WHERE the bloom_filter skip index on agent_id can prune at granule level.
func TestToolUsageNormalizedEventsCTEPushesIdentityFilterIntoScan(t *testing.T) {
	t.Parallel()

	unfiltered, unfilteredArgs, err := toolUsageNormalizedEventsCTE(GetToolUsageSummaryParams{})
	require.NoError(t, err)
	require.NotContains(t, unfiltered, "agent_id = ?")
	require.NotContains(t, unfiltered, "user_email = ?")
	for _, arg := range unfilteredArgs {
		require.NotEqual(t, "agent-123", arg)
	}

	query, args, err := toolUsageNormalizedEventsCTE(GetToolUsageSummaryParams{
		UserFilters: []ToolUsageUserFilter{{Kind: ToolUsageUserKindAgentID, Key: "agent-123"}},
	})
	require.NoError(t, err)
	// One predicate per scan branch: the direct (non-hook) branch and the hook branch.
	require.Equal(t, 2, strings.Count(query, "agent_id = ?"))
	count := 0
	for _, arg := range args {
		if arg == "agent-123" {
			count++
		}
	}
	// Twice from the scan pushdown, plus once per branch of the agent-trace
	// subquery (withToolUsageAgentTraces) for an agent_id filter.
	require.GreaterOrEqual(t, count, 2)
	// In the WHERE of the grouped scans, ahead of the GROUP BY that would
	// otherwise aggregate every trace in the window.
	firstScan := strings.Index(query, "FROM trace_summaries")
	require.Less(t, strings.Index(query, "agent_id = ?"), strings.Index(query, "GROUP BY trace_id"))
	require.Greater(t, firstScan, -1)
}

func TestToolUsageScanIdentityFilter(t *testing.T) {
	t.Parallel()

	t.Run("no identity filter is nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, toolUsageScanIdentityFilter(GetToolUsageSummaryParams{}))
	})

	t.Run("single agent filter is a single-column equality", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			UserFilters: []ToolUsageUserFilter{{Kind: ToolUsageUserKindAgentID, Key: "agent-123"}},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		require.Equal(t, "(agent_id = ?)", sql)
		require.Equal(t, []any{"agent-123"}, args)
	})

	t.Run("id kinds map to their trace_summaries columns", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			UserFilters: []ToolUsageUserFilter{
				{Kind: ToolUsageUserKindAgentID, Key: "agent-123"},
				{Kind: toolUsageUserKindExternalUserID, Key: "ext-1"},
				{Kind: toolUsageUserKindUserID, Key: "user-1"},
			},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		require.Contains(t, sql, "agent_id = ?")
		require.Contains(t, sql, "external_user_id = ?")
		require.Contains(t, sql, "user_id = ?")
		require.Equal(t, []any{"agent-123", "ext-1", "user-1"}, args)
	})

	t.Run("email filters fold through the identity_map", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			CanonicalIdentityOrg: "org-1",
			UserFilters:          []ToolUsageUserFilter{{Kind: toolUsageUserKindEmail, Key: "A@Example.com"}},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		require.Contains(t, sql, "joinGet('identity_map', 'canonical_email', 'org-1'")
		require.Contains(t, sql, "user_email")
		require.NotContains(t, sql, "user_email = ?")
		// The requested value is bound normalized, on both sides of the fold.
		require.Equal(t, []any{"a@example.com", "a@example.com"}, args)
	})

	t.Run("email filter without an org disables the pushdown", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			UserFilters: []ToolUsageUserFilter{{Kind: toolUsageUserKindEmail, Key: "a@example.com"}},
		}))
	})

	t.Run("authorizer ids join the user filter as an OR branch", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			UserFilters:       []ToolUsageUserFilter{{Kind: toolUsageUserKindUserID, Key: "user-1"}},
			AuthorizerUserIDs: []string{"user-1", "user-2"},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		require.Contains(t, sql, "user_id = ?")
		require.Contains(t, sql, "authorizer_user_id IN (?,?)")
		require.Equal(t, []any{"user-1", "user-1", "user-2"}, args)
	})

	t.Run("agent ids are ANDed with the user filter", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			UserFilters: []ToolUsageUserFilter{{Kind: toolUsageUserKindUserID, Key: "user-1"}},
			AgentIDs:    []string{"agent-1", "agent-2"},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		require.Contains(t, sql, "user_id = ?")
		require.Contains(t, sql, "agent_id IN (?,?)")
		require.Equal(t, []any{"user-1", "agent-1", "agent-2"}, args)
	})

	t.Run("agent ids alone still narrow the scan", func(t *testing.T) {
		t.Parallel()
		pred := toolUsageScanIdentityFilter(GetToolUsageSummaryParams{
			AgentIDs: []string{"agent-1"},
		})
		sql, args, err := pred.ToSql()
		require.NoError(t, err)
		// A single-element id slice renders as IN, still a single column.
		require.Equal(t, "agent_id IN (?)", sql)
		require.Equal(t, []any{"agent-1"}, args)
	})
}
