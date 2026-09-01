import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { isAuditAction, staticActionPhrase } from "@/lib/audit-actions";

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
    // The new name is the subject display name rendered right after this
    // phrase, so naming it here would print it twice.
    return "renamed MCP server";
  }
  if (changed.has("ToolSelectionMode") && changed.size <= 2) {
    return `changed tool selection mode to ${String(after["ToolSelectionMode"])}`;
  }
  if (changed.has("Description") && changed.size <= 2) {
    return "updated MCP server description";
  }

  return "updated MCP server";
}

function recordString(value: unknown, key: string): string | undefined {
  if (value == null || typeof value !== "object") return undefined;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" && field !== "" ? field : undefined;
}

function formatRoleSlug(roleSlug: string): string {
  return roleSlug.replace(/[-_]/g, " ");
}

function describeInvitation(log: AuditLog): string | undefined {
  if (log.action === "organization_invitation:create") {
    const role = recordString(log.metadata, "role_slug");
    return role ? `sent ${formatRoleSlug(role)} invite to` : undefined;
  }

  if (log.action === "organization_invitation:update_role") {
    const before =
      recordString(log.beforeSnapshot, "RoleSlug") ??
      recordString(log.beforeSnapshot, "role_slug");
    const after =
      recordString(log.afterSnapshot, "RoleSlug") ??
      recordString(log.afterSnapshot, "role_slug");
    if (before && after && before !== after) {
      return `changed invite role from ${formatRoleSlug(before)} to ${formatRoleSlug(after)} for`;
    }
    if (after) {
      return `changed invite role to ${formatRoleSlug(after)} for`;
    }
  }

  return undefined;
}

const IRREGULAR_PAST_TENSE: Record<string, string> = {
  set: "set",
  send: "sent",
  run: "ran",
  upsert: "updated",
};

function pastTense(verb: string): string {
  const irregular = IRREGULAR_PAST_TENSE[verb];
  if (irregular) return irregular;
  if (verb.endsWith("ed")) return verb;
  if (verb.endsWith("e")) return `${verb}d`;
  // Consonant + y inflects to -ied ("retry" -> "retried"); a vowel before it
  // does not ("relay" -> "relayed").
  if (/[^aeiou]y$/.test(verb)) return `${verb.slice(0, -1)}ied`;
  return `${verb}ed`;
}

/**
 * Last-resort phrasing for an action the client doesn't know about yet — a
 * server deploy that adds an action ships before the dashboard does. Still past
 * tense ("risk_policy:delete" -> "deleted risk policy") so a stale client reads
 * like the rest of the trail instead of leaking the raw action key.
 */
function describeUnknownAction(action: string): string {
  const [resource = "activity", verb = "update"] = action.split(":");
  const words = verb.replace(/[-_]/g, " ").split(" ");
  const [head = "update", ...rest] = words;
  return [pastTense(head), ...rest, getResourceLabel(resource)]
    .filter(Boolean)
    .join(" ");
}

const ASSET_KIND_LABELS: Record<string, string> = {
  functions: "Functions bundle",
  openapi: "OpenAPI document",
  image: "Image",
};

const SUBJECT_TYPE_LABELS: Record<string, string> = {
  mcp_server: "MCP server",
  otel_forwarding_config: "OpenTelemetry forwarding",
  api_key: "API key",
  chat_session: "Chat session",
};

function subjectTypeLabel(subjectType: string): string {
  const known = SUBJECT_TYPE_LABELS[subjectType];
  if (known) return known;
  const words = subjectType.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

const HASHED_FILENAME = /^([a-z0-9]+)-([0-9a-f]{16,})(\.[a-z0-9]+)?$/i;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Display text for a subject whose stored name is content-addressed — upload
 * filenames like `functions-<64 hex>.zip` and bare UUIDs carry no meaning at a
 * glance. Names the kind and keeps a short prefix for identity; callers keep
 * the raw value for the tooltip.
 */
export function formatSubjectLabel(raw: string, subjectType: string): string {
  const hashed = HASHED_FILENAME.exec(raw);
  if (hashed) {
    const [, prefix = "", hash = ""] = hashed;
    const kind =
      ASSET_KIND_LABELS[prefix.toLowerCase()] ??
      `${prefix.charAt(0).toUpperCase()}${prefix.slice(1)} file`;
    return `${kind} \u00b7 ${hash.slice(0, 8)}`;
  }

  if (UUID.test(raw)) {
    return `${subjectTypeLabel(subjectType)} ${raw.slice(0, 8)}`;
  }

  return raw;
}

/**
 * Human label for an action on its own — filter chips, facet dropdowns — where
 * there is no actor or subject around it. Same phrases as the feed rows, minus
 * the dangling preposition a subject name would have filled in
 * ("revoked invite for" -> "Revoked invite").
 */
export function formatAuditActionLabel(action: string): string {
  const phrase = isAuditAction(action)
    ? staticActionPhrase(action)
    : describeUnknownAction(action);
  const trimmed = phrase.replace(/\s+(for|to|from|on)$/, "");
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

export function renderVerb(log: AuditLog): string {
  // Actions whose wording depends on what actually changed come first; the rest
  // resolve to a fixed phrase from the exhaustive table.
  switch (log.action) {
    case "toolset:update":
      return describeToolsetUpdate(log);
    case "otel_forwarding:upsert":
      return describeOtelForwardingUpsert(log);
    case "organization_invitation:create":
    case "organization_invitation:update_role":
      return describeInvitation(log) ?? staticActionPhrase(log.action);
  }

  return isAuditAction(log.action)
    ? staticActionPhrase(log.action)
    : describeUnknownAction(log.action);
}
