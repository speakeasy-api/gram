package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnMCPServerName fills mcp_server_name, the MCP family's own column:
// the MCP server involved, when the record says so. On a tool_call, its
// result and the decision about it the two MCP names come as a pair, so the
// server is Conditionally Required on the tool being stated: a built-in
// tool states neither and nothing is counted, and a record that states the
// tool but not the server is counted on the server, since the producer
// stated half of the pair. On an api_request the server is Recommended: a
// request names the server whose tool result it was made on behalf of,
// which most requests were not.
func columnMCPServerName() columnDefinition {
	server := getter[string]{log: dialect.LogDialect.MCPServerName, span: dialect.SpanDialect.MCPServerName}
	tool := getter[string]{log: dialect.LogDialect.MCPToolName, span: dialect.SpanDialect.MCPToolName}
	onTool := conditionallyRequired(server, statedBy(tool))
	return column[string]{
		key: MCPServerNameColumnKey,
		byType: perEventType[string]{
			dialect.EventTypeAPIRequest:     recommended(server),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
	}
}
