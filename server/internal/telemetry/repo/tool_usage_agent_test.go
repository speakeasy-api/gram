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
				require.Contains(t, query, "telemetry_logs.event_source IN ('tool_call', 'resource_read', 'meta_discovery') AND toString(attributes.gram.authorization.actor.type) = 'agent'")
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
