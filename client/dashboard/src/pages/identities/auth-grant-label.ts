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
  /**
   * Whether the organization's auth clients loaded, so a credential `client`
   * cannot name is really gone rather than unreadable or still loading.
   */
  clientsLoaded: boolean;
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

/**
 * " · on <server>, <server>" after a credential, when it serves any. A grant
 * narrowed to a project covers only the servers in that project.
 */
function onServers(
  servers: AuthGrantServer[],
  selector: AgentPolicySelector,
): AuthGrantPart[] {
  const projectId = selector.projectId;
  const covered = isAgentPolicyConstraint(projectId)
    ? servers.filter((server) => server.projectId === projectId)
    : servers;
  return covered.length > 0
    ? [text(" · on "), { kind: "servers", servers: covered }]
    : [];
}

/** A person to link; one who is not a member any more keeps their id. */
function personPart(userId: string, names: AuthGrantNames): AuthGrantPart {
  return {
    kind: "person",
    userId,
    name: names.member(userId) ?? `User ${shortId(userId)}`,
  };
}

/**
 * A grant on one auth client, still narrowed by the connection owner and the
 * upstream OAuth client it may also name.
 */
function credentialParts(
  resourceId: string,
  selector: AgentPolicySelector,
  names: AuthGrantNames,
  owner: string | undefined,
  upstreamPart: AuthGrantPart | undefined,
): AuthGrantPart[] {
  const client = names.client(resourceId);
  const missing = `credential ${shortId(resourceId)}${names.clientsLoaded ? ", no longer found" : ""}`;
  const credential = client
    ? `${client.name} · ${clientTypeLabel(client.clientType)}`
    : `upstream ${missing}`;
  const rest: AuthGrantPart[] = [
    ...(upstreamPart
      ? [text(" · through upstream client "), upstreamPart]
      : []),
    ...(client ? onServers(client.servers, selector) : []),
  ];
  if (owner === REPRESENTED_USER) {
    return [
      text(`The connection of the person the agent acts for to ${credential}`),
      ...rest,
    ];
  }
  if (owner) {
    return [
      personPart(owner, names),
      text(`'s connection to ${credential}`),
      ...rest,
    ];
  }
  return [text(client ? credential : `Upstream ${missing}`), ...rest];
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
  const owner = isAgentPolicyConstraint(selector.connectionOwner)
    ? selector.connectionOwner
    : undefined;
  const upstreamId = isAgentPolicyConstraint(selector.upstreamClient)
    ? selector.upstreamClient
    : undefined;
  const upstream = upstreamId ? names.upstreamClient(upstreamId) : undefined;
  // An upstream client that cannot be found any more keeps its short id.
  const upstreamPart: AuthGrantPart | undefined = upstreamId
    ? {
        kind: "upstream",
        id: upstreamId,
        name: upstream?.name ?? shortId(upstreamId),
        issuerId: upstream?.issuerId,
      }
    : undefined;
  if (isAgentPolicyConstraint(selector.resourceId)) {
    return credentialParts(
      selector.resourceId,
      selector,
      names,
      owner,
      upstreamPart,
    );
  }
  const servers = upstream ? onServers(upstream.servers, selector) : [];

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
    const person = personPart(owner, names);
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
