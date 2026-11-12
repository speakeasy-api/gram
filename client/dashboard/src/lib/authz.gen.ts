// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

/** The resource kind each scope's checks and grant selectors name. */
export const scopeResourceKinds: Readonly<Record<string, string>> = {
  "agent:authorize": "agent",
  "agent:read": "agent",
  "agent:transfer": "agent",
  "agent:write": "agent",
  "assistant:blocked_read": "assistant",
  "assistant:blocked_write": "assistant",
  "assistant:read": "assistant",
  "assistant:write": "assistant",
  "chat:read": "chat",
  "chat:write": "chat",
  "environment:blocked_read": "environment",
  "environment:blocked_write": "environment",
  "environment:read": "environment",
  "environment:write": "environment",
  "inference:execute": "inference",
  "log:read": "project",
  "mcp:auth": "mcp_auth_client",
  "mcp:blocked_auth": "mcp_auth_client",
  "mcp:blocked_connect": "mcp",
  "mcp:blocked_read": "mcp",
  "mcp:blocked_write": "mcp",
  "mcp:connect": "mcp",
  "mcp:read": "mcp",
  "mcp:write": "mcp",
  "org:admin": "org",
  "org:blocked_admin": "org",
  "org:blocked_read": "org",
  "org:device_agent_sync": "org",
  "org:hooks_ingest": "org",
  "org:read": "org",
  "plugin:blocked_write": "project",
  "plugin:write": "project",
  "project:blocked_read": "project",
  "project:blocked_write": "project",
  "project:read": "project",
  "project:write": "project",
  "risk_policy:block": "risk_policy",
  "risk_policy:bypass": "risk_policy",
  "risk_policy:evaluate": "risk_policy",
  "root": "*",
  "skill:blocked_read": "skill",
  "skill:blocked_write": "skill",
  "skill:read": "skill",
  "skill:write": "skill",
  "workload:blocked_read": "workload",
  "workload:blocked_write": "workload",
  "workload:read": "workload",
  "workload:write": "workload",
};

/**
 * The exclusion scopes a check of each scope must clear: its own exclusion,
 * then the broader blocks that also cover it. A block on read also takes
 * write away.
 */
export const scopeExclusions: Readonly<
  Record<string, readonly string[]>
> = {
  "assistant:read": [
    "assistant:blocked_read",
  ],
  "assistant:write": [
    "assistant:blocked_write",
    "assistant:blocked_read",
  ],
  "environment:read": [
    "environment:blocked_read",
  ],
  "environment:write": [
    "environment:blocked_write",
    "environment:blocked_read",
  ],
  "mcp:auth": [
    "mcp:blocked_auth",
  ],
  "mcp:connect": [
    "mcp:blocked_connect",
  ],
  "mcp:read": [
    "mcp:blocked_read",
  ],
  "mcp:write": [
    "mcp:blocked_write",
    "mcp:blocked_read",
  ],
  "org:admin": [
    "org:blocked_admin",
    "org:blocked_read",
  ],
  "org:read": [
    "org:blocked_read",
  ],
  "plugin:write": [
    "plugin:blocked_write",
  ],
  "project:read": [
    "project:blocked_read",
  ],
  "project:write": [
    "project:blocked_write",
    "project:blocked_read",
  ],
  "risk_policy:evaluate": [
    "risk_policy:bypass",
  ],
  "skill:read": [
    "skill:blocked_read",
  ],
  "skill:write": [
    "skill:blocked_write",
    "skill:blocked_read",
  ],
  "workload:read": [
    "workload:blocked_read",
  ],
  "workload:write": [
    "workload:blocked_write",
    "workload:blocked_read",
  ],
};
