package enrich

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// columnMCPServerName fills mcp_server_name, the MCP family's own column:
// the MCP server involved, when the record says so. A tool call, its result
// and the decision about it name the server the tool belongs to; an
// api_request names the server whose tool result the request was made on
// behalf of, which most requests were not, so there it is optional.
//
// On a tool event the two MCP names come as a pair. A built-in tool has
// neither, and that is not a gap; a record that names the tool but not the
// server is, since the producer stated half of the pair.
func columnMCPServerName(in *Instruments) LogEnricher {
	server := question[string]{log: dialect.LogDialect.MCPServerName, span: dialect.SpanDialect.MCPServerName}
	onTool := question[string]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
			return mcpPairLog(r, d.MCPServerName, d.MCPToolName)
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, string, error) {
			return mcpPairSpan(s, d.MCPServerName, d.MCPToolName)
		},
	}
	return &logColumnEnricher[string]{
		column: MCPServerNameColumnKey,
		byType: columnTable[string]{
			dialect.EventTypeAPIRequest:     optional(server),
			dialect.EventTypeToolCall:       onTool,
			dialect.EventTypeToolCallResult: onTool,
			dialect.EventTypeToolDecision:   onTool,
		},
		instruments: in,
	}
}

// mcpPairLog answers one half of the MCP server and tool pair on a tool
// event: the half asked for when stated, not applicable when the record
// states neither half (a built-in tool), and missing when it states only the
// other half.
func mcpPairLog(
	r *otelv1.InboundLogRecord,
	asked func(*otelv1.InboundLogRecord) (string, string, error),
	other func(*otelv1.InboundLogRecord) (string, string, error),
) (string, string, error) {
	key, value, err := asked(r)
	if err != nil || key != "" {
		return key, value, err
	}
	if otherKey, _, otherErr := other(r); otherErr == nil && otherKey == "" {
		return "", "", errNotApplicable
	}
	return "", "", nil
}

func mcpPairSpan(
	s *otelv1.InboundSpan,
	asked func(*otelv1.InboundSpan) (string, string, error),
	other func(*otelv1.InboundSpan) (string, string, error),
) (string, string, error) {
	key, value, err := asked(s)
	if err != nil || key != "" {
		return key, value, err
	}
	if otherKey, _, otherErr := other(s); otherErr == nil && otherKey == "" {
		return "", "", errNotApplicable
	}
	return "", "", nil
}
