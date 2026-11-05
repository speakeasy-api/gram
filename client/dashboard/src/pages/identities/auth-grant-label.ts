import { clientTypeLabel } from "@/lib/auth-clients";
import { isAgentPolicyConstraint } from "@/pages/agents/agent-policy-grants";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";

/** The connection owner value that means the person the agent acts for. */
export const REPRESENTED_USER = "$represented_user";

/** A server a credential signs in to, linked to its authentication settings. */
export type AuthGrantServer = {
  id: string;
  slug?: string | undefined;
  name: string;
  projectId: string;
};

/** Names the labels can use; any of them may be missing. */
export type AuthGrantNames = {
  /** An auth client's name, type and the servers it serves, by id. */
  client: (
    id: string,
  ) =>
    | { name: string; clientType: string; servers: AuthGrantServer[] }
    | undefined;
  /**
   * The name of an upstream OAuth client and the servers whose auth clients
   * sign in through it, by its id.
   */
  upstreamClient: (
    id: string,
  ) => { name: string; servers: AuthGrantServer[] } | undefined;
  /** A member's display name, by user id. */
  member: (userId: string) => string | undefined;
};

/** One piece of a label: words, a person to link, or servers to link. */
export type AuthGrantPart =
  | { kind: "text"; text: string }
  | { kind: "person"; userId: string; name: string }
  | { kind: "servers"; servers: AuthGrantServer[] };

/** The first characters of an id, for when its name is unknown. */
function shortId(id: string): string {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id;
}

function text(value: string): AuthGrantPart {
  return { kind: "text", text: value };
}

/** " · on <server>, <server>" after a credential, when it serves any. */
function onServers(servers: AuthGrantServer[]): AuthGrantPart[] {
  return servers.length > 0
    ? [text(" · on "), { kind: "servers", servers }]
    : [];
}

/**
 * Which upstream credentials an `mcp:auth` (or `mcp:blocked_auth`) grant
 * covers, as words with the people and servers it names kept apart so they
 * can be linked. It reads every dimension the grant narrows by (auth client,
 * client type, connection owner, upstream OAuth client), so a grant on one
 * person's connection never reads like one on every credential. A project
 * narrowing is shown beside it, like any grant.
 */
export function authGrantParts(
  selector: AgentPolicySelector,
  names: AuthGrantNames,
): AuthGrantPart[] {
  if (isAgentPolicyConstraint(selector.resourceId)) {
    const client = names.client(selector.resourceId);
    if (!client) {
      return [
        text(
          `Upstream credential ${shortId(selector.resourceId)}, no longer found`,
        ),
      ];
    }
    return [
      text(`${client.name} · ${clientTypeLabel(client.clientType)}`),
      ...onServers(client.servers),
    ];
  }

  const owner = isAgentPolicyConstraint(selector.connectionOwner)
    ? selector.connectionOwner
    : undefined;
  const upstreamId = isAgentPolicyConstraint(selector.upstreamClient)
    ? selector.upstreamClient
    : undefined;
  const upstream = upstreamId ? names.upstreamClient(upstreamId) : undefined;
  const servers = upstream ? onServers(upstream.servers) : [];
  // An upstream client nobody's auth client names any more keeps its id.
  const through =
    upstreamId && !upstream
      ? ` through upstream client ${shortId(upstreamId)}`
      : "";

  if (owner === REPRESENTED_USER) {
    if (upstream) {
      return [
        text(
          `The ${upstream.name} connection of the person the agent acts for`,
        ),
        ...servers,
      ];
    }
    return [
      text(
        upstreamId
          ? `The connection of the person the agent acts for${through}`
          : "Personal connections of the person the agent acts for",
      ),
    ];
  }
  if (owner) {
    const name = names.member(owner);
    const person: AuthGrantPart = name
      ? { kind: "person", userId: owner, name }
      : text(`User ${shortId(owner)}`);
    if (upstream) {
      return [person, text(`'s ${upstream.name} connection`), ...servers];
    }
    return [
      person,
      text(upstreamId ? `'s connection${through}` : "'s personal connections"),
    ];
  }
  if (upstream) {
    return [text(`Every personal connection to ${upstream.name}`), ...servers];
  }
  if (upstreamId) return [text(`Every personal connection${through}`)];
  if (selector.clientType === "remote_session") {
    return [text("Every personal connection in the organization")];
  }
  if (isAgentPolicyConstraint(selector.clientType)) {
    return [
      text(
        `Every ${clientTypeLabel(selector.clientType).toLowerCase()} in the organization`,
      ),
    ];
  }
  return [text("Every upstream credential in the organization")];
}

/** The label as plain text, with each linked name in its place. */
export function authGrantText(parts: AuthGrantPart[]): string {
  return parts
    .map((part) => {
      if (part.kind === "text") return part.text;
      if (part.kind === "person") return part.name;
      return part.servers.map((server) => server.name).join(", ");
    })
    .join("");
}

/**
 * Where a grant sorts among an agent's `mcp:auth` grants: the broadest first,
 * then narrower kinds, down to one named credential. A project narrowing
 * sorts just after the same grant without one.
 */
export function authGrantRank(selector: AgentPolicySelector): number {
  const narrowedToProject = isAgentPolicyConstraint(selector.projectId)
    ? 0.5
    : 0;
  return authGrantKindRank(selector) + narrowedToProject;
}

function authGrantKindRank(selector: AgentPolicySelector): number {
  if (isAgentPolicyConstraint(selector.resourceId)) return 7;
  const owner = isAgentPolicyConstraint(selector.connectionOwner)
    ? selector.connectionOwner
    : undefined;
  const upstream = isAgentPolicyConstraint(selector.upstreamClient);
  if (owner === REPRESENTED_USER) return upstream ? 4 : 3;
  if (owner) return upstream ? 6 : 5;
  if (upstream) return 2;
  if (isAgentPolicyConstraint(selector.clientType)) return 1;
  return 0;
}
