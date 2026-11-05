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
   * An upstream OAuth client by its id: its name, the issuer it is registered
   * with (for its page) and the servers whose auth clients sign in through it.
   */
  upstreamClient: (
    id: string,
  ) =>
    | { name: string; issuerId?: string; servers: AuthGrantServer[] }
    | undefined;
  /** A member's display name, by user id. */
  member: (userId: string) => string | undefined;
};

/**
 * One piece of a label: words, or a person, upstream OAuth client or servers
 * to link. An upstream client without an issuer has no page to link to.
 */
export type AuthGrantPart =
  | { kind: "text"; text: string }
  | { kind: "person"; userId: string; name: string }
  | { kind: "upstream"; id: string; name: string; issuerId?: string }
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
  // An upstream client that cannot be found any more keeps its short id.
  const upstreamPart: AuthGrantPart | undefined = upstreamId
    ? {
        kind: "upstream",
        id: upstreamId,
        name: upstream?.name ?? shortId(upstreamId),
        issuerId: upstream?.issuerId,
      }
    : undefined;

  if (owner === REPRESENTED_USER) {
    if (upstream && upstreamPart) {
      return [
        text("The "),
        upstreamPart,
        text(" connection of the person the agent acts for"),
        ...servers,
      ];
    }
    if (upstreamPart) {
      return [
        text(
          "The connection of the person the agent acts for through upstream client ",
        ),
        upstreamPart,
      ];
    }
    return [text("Personal connections of the person the agent acts for")];
  }
  if (owner) {
    // A person who is not a member any more is still linked by their id.
    const person: AuthGrantPart = {
      kind: "person",
      userId: owner,
      name: names.member(owner) ?? `User ${shortId(owner)}`,
    };
    if (upstream && upstreamPart) {
      return [
        person,
        text("'s "),
        upstreamPart,
        text(" connection"),
        ...servers,
      ];
    }
    if (upstreamPart) {
      return [
        person,
        text("'s connection through upstream client "),
        upstreamPart,
      ];
    }
    return [person, text("'s personal connections")];
  }
  if (upstream && upstreamPart) {
    return [text("Any personal connection to "), upstreamPart, ...servers];
  }
  if (upstreamPart) {
    return [
      text("Any personal connection through upstream client "),
      upstreamPart,
    ];
  }
  if (selector.clientType === "remote_session") {
    return [text("Any personal connection in the organization")];
  }
  if (isAgentPolicyConstraint(selector.clientType)) {
    return [
      text(
        `Any ${clientTypeLabel(selector.clientType).toLowerCase()} in the organization`,
      ),
    ];
  }
  return [text("Any upstream credential in the organization")];
}

/** The label as plain text, with each linked name in its place. */
export function authGrantText(parts: AuthGrantPart[]): string {
  return parts
    .map((part) => {
      if (part.kind === "text") return part.text;
      if (part.kind === "person" || part.kind === "upstream") {
        return part.name;
      }
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
