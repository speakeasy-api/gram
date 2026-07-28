import type { AuditLog } from "@gram/client/models/components/auditlog.js";

export function getActorLabel(log: AuditLog): string {
  return log.actorDisplayName || log.actorSlug || "Someone";
}

export function formatAuditAction(action: string): string {
  const [resource, verb] = action.split(":");
  if (!resource || !verb) {
    return action;
  }
  const resourceLabel = resource === "toolset" ? "mcp" : resource;
  return `${resourceLabel}:${verb}`;
}

function getResourceLabel(resource: string): string {
  switch (resource) {
    case "api_key":
      return "API key";
    case "asset":
      return "asset";
    case "custom_domains":
      return "custom domain";
    case "deployments":
      return "deployment";
    case "environment":
      return "environment";
    case "mcp_metadata":
      return "MCP metadata";
    case "otel_forwarding":
    case "otel_forwarding_config":
      return "OpenTelemetry forwarding";
    case "organization_invitation":
      return "organization invitation";
    case "plugin":
      return "plugin";
    case "project":
      return "project";
    case "template":
      return "template";
    case "toolset":
      return "MCP server";
    case "variation":
      return "global variation";
    default:
      return resource.replace(/_/g, " ");
  }
}

function endpointHost(raw: unknown): string {
  if (typeof raw !== "string" || raw === "") return "";
  try {
    return new URL(raw).host || raw;
  } catch {
    return raw;
  }
}

function describeOtelForwardingUpsert(log: AuditLog): string {
  const before = log.beforeSnapshot as Record<string, unknown> | undefined;
  const after = log.afterSnapshot as Record<string, unknown> | undefined;
  if (!after) return "updated OpenTelemetry forwarding configuration";

  const afterHost = endpointHost(after["endpoint_url"]);
  const afterEnabled = Boolean(after["enabled"]);
  const afterHeaders = Array.isArray(after["header_names"])
    ? (after["header_names"] as string[])
    : [];

  if (!before) {
    return afterEnabled
      ? `enabled OpenTelemetry forwarding${afterHost ? ` to ${afterHost}` : ""}`
      : `configured OpenTelemetry forwarding${afterHost ? ` to ${afterHost}` : ""} (disabled)`;
  }

  const beforeHost = endpointHost(before["endpoint_url"]);
  const beforeEnabled = Boolean(before["enabled"]);
  const beforeHeaders = Array.isArray(before["header_names"])
    ? (before["header_names"] as string[])
    : [];

  const enabledChanged = beforeEnabled !== afterEnabled;
  const endpointChanged = beforeHost !== afterHost;
  const headersChanged =
    JSON.stringify([...beforeHeaders].sort()) !==
    JSON.stringify([...afterHeaders].sort());

  const changedCount = [enabledChanged, endpointChanged, headersChanged].filter(
    Boolean,
  ).length;

  if (changedCount === 1) {
    if (enabledChanged) {
      return afterEnabled
        ? "enabled OpenTelemetry forwarding"
        : "disabled OpenTelemetry forwarding";
    }
    if (endpointChanged) {
      return `changed OpenTelemetry forwarding endpoint to ${afterHost || "(unset)"}`;
    }
    if (headersChanged) {
      return "updated OpenTelemetry forwarding headers";
    }
  }

  return "updated OpenTelemetry forwarding configuration";
}

function describeToolsetUpdate(log: AuditLog): string {
  const before = log.beforeSnapshot as Record<string, unknown> | undefined;
  const after = log.afterSnapshot as Record<string, unknown> | undefined;
  if (!before || !after) return "updated MCP server";

  const changed = new Set<string>();
  for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (JSON.stringify(before[key]) !== JSON.stringify(after[key])) {
      changed.add(key);
    }
  }

  if (changed.has("McpIsPublic") && changed.size <= 2) {
    const isPublic = after["McpIsPublic"];
    return `changed MCP server visibility to ${isPublic ? "public" : "private"}`;
  }
  if (changed.has("McpEnabled") && changed.size <= 2) {
    const enabled = after["McpEnabled"];
    return `${enabled ? "enabled" : "disabled"} MCP for server`;
  }
  if (changed.has("Name") && changed.size <= 2) {
    return `renamed MCP server to ${String(after["Name"])}`;
  }
  if (changed.has("ToolSelectionMode") && changed.size <= 2) {
    return `changed tool selection mode to ${String(after["ToolSelectionMode"])}`;
  }
  if (changed.has("Description") && changed.size <= 2) {
    return "updated MCP server description";
  }

  return "updated MCP server";
}

export function renderVerb(log: AuditLog): string {
  switch (log.action) {
    case "project:create":
      return "created project";
    case "project:update":
      return "updated project";
    case "project:delete":
      return "deleted project";
    case "environment:create":
      return "created environment";
    case "environment:update":
      return "updated environment";
    case "environment:delete":
      return "deleted environment";
    case "template:create":
      return "created template";
    case "template:update":
      return "updated template";
    case "template:delete":
      return "deleted template";
    case "toolset:create":
      return "created MCP server";
    case "toolset:update":
      return describeToolsetUpdate(log);
    case "toolset:delete":
      return "deleted MCP server";
    case "toolset:attach_external_oauth":
      return "attached an external OAuth server to MCP server";
    case "toolset:detach_external_oauth":
      return "detached an external OAuth server from MCP server";
    case "toolset:attach_oauth_proxy":
      return "attached an OAuth proxy to MCP server";
    case "toolset:detach_oauth_proxy":
      return "detached an OAuth proxy from MCP server";
    case "api_key:create":
      return "created API key";
    case "api_key:revoke":
      return "revoked API key";
    case "variation:update_global":
      return "updated a global variation for";
    case "variation:delete_global":
      return "deleted a global variation for";
    case "deployments:create":
      return "created deployment";
    case "deployments:evolve":
      return "created deployment";
    case "deployments:redeploy":
      return "redeployed deployment";
    case "custom_domains:create":
      return "added custom domain";
    case "custom_domains:delete":
      return "deleted custom domain";
    case "mcp_metadata:update":
      return "updated MCP metadata for";
    case "otel_forwarding:upsert":
      return describeOtelForwardingUpsert(log);
    case "otel_forwarding:delete":
      return "removed OpenTelemetry forwarding configuration";
    case "asset:create":
      return "uploaded asset";
    case "plugin:create":
      return "created plugin";
    case "plugin:update":
      return "updated plugin";
    case "plugin:delete":
      return "deleted plugin";
    case "plugin:server_add":
      return "added server to plugin";
    case "plugin:server_update":
      return "updated server on plugin";
    case "plugin:server_remove":
      return "removed server from plugin";
    case "plugin:assignments_set":
      return "updated plugin access assignments";
    case "plugin:publish":
      return "published plugins";
    case "organization:webhooks_enabled":
      return "enabled webhooks delivery";
    case "organization:webhooks_disabled":
      return "disabled webhooks delivery";
    case "organization_invitation:create":
      return "invited";
    case "organization_invitation:revoke":
      return "revoked invite for";
    case "organization_invitation:update_role":
      return "changed invite role for";
    default: {
      const [resource = "activity", verb = "updated"] = log.action.split(":");
      return `${verb.replace(/_/g, " ")} ${getResourceLabel(resource)}`;
    }
  }
}
