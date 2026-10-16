package access

// accessScopeSlugs keeps role input, effective grants, and the scope catalogue
// on the same management API vocabulary.
var accessScopeSlugs = []any{
	"org:read", "org:blocked_read", "org:admin", "org:blocked_admin",
	"project:read", "project:blocked_read", "project:write", "project:blocked_write",
	"mcp:read", "mcp:blocked_read", "mcp:write", "mcp:blocked_write", "mcp:connect", "mcp:blocked_connect",
	"environment:read", "environment:blocked_read", "environment:write", "environment:blocked_write",
	"skill:read", "skill:blocked_read", "skill:write", "skill:blocked_write",
	"assistant:read", "assistant:blocked_read", "assistant:write", "assistant:blocked_write",
	"plugin:write", "plugin:blocked_write", "risk_policy:evaluate", "risk_policy:bypass", "risk_policy:block",
	"chat:read", "chat:write", "log:read", "agent:read", "agent:write", "agent:authorize", "agent:transfer",
	"workload:read", "workload:blocked_read", "workload:write", "workload:blocked_write",
	"org:device_agent_sync", "org:hooks_ingest",
}
