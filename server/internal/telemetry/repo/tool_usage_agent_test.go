package repo

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolUsageAgentAttributionSQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		build   func() (string, []any, error)
		grouped bool
	}{
		{"summary", func() (string, []any, error) { return toolUsageNormalizedEventsCTE(GetToolUsageSummaryParams{}) }, true},
		{"trace summary", func() (string, []any, error) { return toolUsageTraceRowsFromSummariesCTE(ListToolUsageTracesParams{}) }, true},
		{"raw search", func() (string, []any, error) { return toolUsageTraceRowsCTE(ListToolUsageTracesParams{Query: ":"}) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, _, err := tt.build()
			require.NoError(t, err)
			prefix := ""
			if tt.grouped {
				prefix = "g_"
				require.Contains(t, query, "max(agent_id) AS g_agent_id")
				require.NotContains(t, query, "telemetry_logs")
			} else {
				require.Contains(t, query, toolUsageAgentIDExpr+" AS agent_id")
				require.Contains(t, query, "telemetry_logs.event_source IN ('tool_call', 'resource_read', 'meta_discovery', 'hook') AND toString(attributes.gram.authorization.actor.type) = 'agent', toString(attributes.gram.authorization.actor.id)")
			}
			agentExpr := prefix + "agent_id"
			if !tt.grouped {
				agentExpr = "max(agent_id) OVER (PARTITION BY multiIf(trace_id != '', 'trace_id', trigger_correlation_id != '', 'correlation_id', trigger_event_id != '', 'trigger_event_id', 'log_id'), multiIf(trace_id != '', toString(trace_id), trigger_correlation_id != '', trigger_correlation_id, trigger_event_id != '', trigger_event_id, toString(log_id)))"
				require.Equal(t, 1, strings.Count(query, "FROM telemetry_logs"))
			}
			require.Contains(t, query, "multiIf("+agentExpr+" != '', 'agent_id', "+prefix+"user_email")
			require.Contains(t, query, "concat('agent:', "+agentExpr+")")
			require.Contains(t, query, " AS user_key")
			require.Contains(t, query, " AS user_label")
		})
	}
}

func TestToolUsageAgentSummaryExpressionMatchesRaw(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)
	require.Contains(t, string(schema), "max("+toolUsageAgentIDExpr+") AS agent_id")
	require.Contains(t, string(schema), "agent_id SimpleAggregateFunction(max, String)")
}

func TestToolUsageAuthorizerSummaryExpressionMatchesRaw(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)
	require.Contains(t, string(schema), "max("+toolUsageAuthorizerUserIDExpr+") AS authorizer_user_id")
	require.Contains(t, string(schema), "authorizer_user_id SimpleAggregateFunction(max, String)")
}

func TestToolUsageAgentScope(t *testing.T) {
	t.Parallel()
	agent := ToolUsageUserFilter{Kind: ToolUsageUserKindAgentID, Key: "a1"}
	email := ToolUsageUserFilter{Kind: toolUsageUserKindEmail, Key: "x@example.com"}
	tests := []struct {
		name        string
		filters     []ToolUsageUserFilter
		authorizers []string
		agentIDs    []string
		want        []string
	}{
		{"no identity filter reads every trace", nil, nil, nil, nil},
		{"an agent filter pins the read", []ToolUsageUserFilter{agent}, nil, nil, []string{"a1"}},
		{"a person in the filter reads traces with no agent", []ToolUsageUserFilter{agent, email}, nil, nil, nil},
		{"an authorizer reads traces the filter does not name", []ToolUsageUserFilter{agent}, []string{"u1"}, nil, nil},
		{"agent ids pin the read on their own", []ToolUsageUserFilter{email}, []string{"u1"}, []string{"a2"}, []string{"a2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, toolUsageAgentScope(tt.filters, tt.authorizers, tt.agentIDs))
		})
	}
}

func TestToolUsageAgentScopeNarrowsBeforeGrouping(t *testing.T) {
	t.Parallel()
	agentFilter := []ToolUsageUserFilter{{Kind: ToolUsageUserKindAgentID, Key: "a1"}}
	const narrowing = "trace_id IN (SELECT trace_id FROM trace_summaries WHERE gram_project_id = ? AND agent_id IN (?)"
	tests := []struct {
		name  string
		build func(filters []ToolUsageUserFilter) (string, []any, error)
		// Each grouped read of trace_summaries carries its own narrowing.
		reads int
	}{
		{"summary", func(filters []ToolUsageUserFilter) (string, []any, error) {
			return toolUsageNormalizedEventsCTE(GetToolUsageSummaryParams{GramProjectID: "p", UserFilters: filters})
		}, 2},
		{"trace summary", func(filters []ToolUsageUserFilter) (string, []any, error) {
			return toolUsageTraceRowsFromSummariesCTE(ListToolUsageTracesParams{GramProjectID: "p", UserFilters: filters})
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scoped, args, err := tt.build(agentFilter)
			require.NoError(t, err)
			require.Equal(t, tt.reads, strings.Count(scoped, narrowing))
			require.Len(t, args, strings.Count(scoped, "?"))
			unscoped, _, err := tt.build(nil)
			require.NoError(t, err)
			require.NotContains(t, unscoped, "agent_id IN")
		})
	}
}
