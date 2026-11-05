import type { AuditLog } from "@gram/client/models/components/auditlog.js";

/** Names the summary can use; any of them may be missing. */
export type AgentChangeNames = {
  /** An MCP server's name, by the id a grant stores for it. */
  server: (id: string) => string | undefined;
  /** A project's name, by id. */
  project: (id: string) => string | undefined;
};

/** Actions whose snapshots are a policy grant: { scope, selector }. */
const GRANT_ACTIONS = new Set([
  "agent:policy_grant_create",
  "agent:policy_grant_update",
  "agent:policy_grant_delete",
  "agent:connection_block_create",
  "agent:connection_block_delete",
]);

const CONNECTION_ACTIONS = new Set([
  "agent:connection_attach",
  "agent:connection_detach",
]);

type GrantSnapshot = { scope?: unknown; selector?: unknown };

function shortId(id: string): string {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id;
}

function grantResource(
  selector: Record<string, unknown>,
  names: AgentChangeNames,
): string {
  const kind = selector.resource_kind;
  const id = selector.resource_id;
  if (kind === "org") return "the organization";
  if (typeof id !== "string" || id === "*") {
    if (kind === "mcp") return "every server";
    if (kind === "mcp_auth_client") return "every upstream credential";
    return "all resources";
  }
  if (kind === "mcp") return names.server(id) ?? `server ${shortId(id)}`;
  if (kind === "project") return names.project(id) ?? `project ${shortId(id)}`;
  if (kind === "mcp_auth_client") return `upstream credential ${shortId(id)}`;
  return shortId(id);
}

/**
 * What a change to an agent touched, in words: the permission and the server
 * or project a grant names, or the upstream account a connection signs in
 * as. Undefined when the entry carries nothing more than its action.
 */
export function agentChangeSummary(
  log: Pick<
    AuditLog,
    "action" | "afterSnapshot" | "beforeSnapshot" | "metadata"
  >,
  names: AgentChangeNames,
): string | undefined {
  if (GRANT_ACTIONS.has(log.action)) {
    const snapshot = (log.afterSnapshot ?? log.beforeSnapshot) as
      | GrantSnapshot
      | undefined;
    if (!snapshot || typeof snapshot.scope !== "string") return undefined;
    const selector =
      snapshot.selector && typeof snapshot.selector === "object"
        ? (snapshot.selector as Record<string, unknown>)
        : {};
    return `${snapshot.scope} · ${grantResource(selector, names)}`;
  }
  if (CONNECTION_ACTIONS.has(log.action)) {
    const account = log.metadata?.upstream_account;
    return typeof account === "string" && account !== ""
      ? `Signs in as ${account}`
      : undefined;
  }
  return undefined;
}
