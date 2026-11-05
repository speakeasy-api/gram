import { clientTypeLabel } from "@/lib/auth-clients";
import { isAgentPolicyConstraint } from "@/pages/agents/agent-policy-grants";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";

/** The connection owner value that means the person the agent acts for. */
export const REPRESENTED_USER = "$represented_user";

/** Names the labels can use; any of them may be missing. */
export type AuthGrantNames = {
  /** An auth client's name and type, by auth client id. */
  client: (id: string) => { name: string; clientType: string } | undefined;
  /** The name of the server an upstream OAuth client signs in to, by its id. */
  upstreamClient: (id: string) => string | undefined;
  /** A member's display name, by user id. */
  member: (userId: string) => string | undefined;
};

/** The first characters of an id, for when its name is unknown. */
function shortId(id: string): string {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id;
}

/**
 * One sentence saying which upstream credentials an `mcp:auth` (or
 * `mcp:blocked_auth`) grant covers. It reads every dimension the grant
 * narrows by (auth client, client type, connection owner, upstream OAuth
 * client), so a grant on one person's connection never reads like one on
 * every credential. A project narrowing is shown beside it, like any grant.
 */
export function authGrantLabel(
  selector: AgentPolicySelector,
  names: AuthGrantNames,
): string {
  if (isAgentPolicyConstraint(selector.resourceId)) {
    const client = names.client(selector.resourceId);
    if (!client) return `Upstream credential ${shortId(selector.resourceId)}`;
    return `${client.name} · ${clientTypeLabel(client.clientType)}`;
  }

  const owner = isAgentPolicyConstraint(selector.connectionOwner)
    ? selector.connectionOwner
    : undefined;
  const upstream = isAgentPolicyConstraint(selector.upstreamClient)
    ? selector.upstreamClient
    : undefined;
  const server = upstream
    ? (names.upstreamClient(upstream) ?? `upstream client ${shortId(upstream)}`)
    : undefined;

  if (owner === REPRESENTED_USER) {
    return server
      ? `The ${server} connection of the person the agent acts for`
      : "Personal connections of the person the agent acts for";
  }
  if (owner) {
    const person = names.member(owner) ?? `User ${shortId(owner)}`;
    return server
      ? `${person}'s ${server} connection`
      : `${person}'s personal connections`;
  }
  if (server) return `Every personal connection to ${server}`;
  if (selector.clientType === "remote_session") {
    return "Every personal connection in the organization";
  }
  if (isAgentPolicyConstraint(selector.clientType)) {
    return `Every ${clientTypeLabel(selector.clientType).toLowerCase()} in the organization`;
  }
  return "Every upstream credential in the organization";
}
