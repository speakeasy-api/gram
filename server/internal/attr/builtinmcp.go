package attr

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

// BuiltinMcpServerIDKey identifies a synthetic built-in resource, not an MCP server row.
const BuiltinMcpServerIDKey = attribute.Key("gram.builtin_mcp_server.id")

func SlogBuiltinMcpServerID(v string) slog.Attr {
	return slog.String(string(BuiltinMcpServerIDKey), v)
}
