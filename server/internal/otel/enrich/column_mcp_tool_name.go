package enrich

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// columnMCPToolName fills mcp_tool_name: the MCP tool involved, when the
// record says so, on the same four types and under the same pairing rule as
// mcp_server_name. The column is deprecated in favour of name, which carries
// the tool on tool_call, tool_call_result and tool_decision whether or not
// it is an MCP tool; it stays filled until a contract migration drops it.
func columnMCPToolName() columnDefinition {
	tool := question[string]{log: dialect.LogDialect.MCPToolName, span: dialect.SpanDialect.MCPToolName}
	onTool := question[string]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
			return mcpPairLog(r, d.MCPToolName, d.MCPServerName)
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, string, error) {
			return mcpPairSpan(s, d.MCPToolName, d.MCPServerName)
		},
	}
	return column[string]{
		key: MCPToolNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:     optional(tool),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
	}
}
