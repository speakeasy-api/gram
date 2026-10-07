package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnMCPToolName fills mcp_tool_name: the MCP tool involved, when the
// record says so, on the same four types and at the same levels as
// mcp_server_name: Conditionally Required on the server being stated on the
// three tool types, Recommended on an api_request. The column is deprecated
// in favour of name, which carries the tool on tool_call, tool_call_result
// and tool_decision whether or not it is an MCP tool; it stays filled until
// a contract migration drops it.
func columnMCPToolName() columnDefinition {
	tool := getter[string]{log: dialect.LogDialect.MCPToolName, span: dialect.SpanDialect.MCPToolName}
	server := getter[string]{log: dialect.LogDialect.MCPServerName, span: dialect.SpanDialect.MCPServerName}
	onTool := conditionallyRequired(tool, statedBy(server))
	return column[string]{
		key: MCPToolNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:     recommended(tool),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
	}
}
