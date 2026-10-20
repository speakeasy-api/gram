import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";

/** The scope an agent's MCP reach is granted under. */
export const CONNECT_SCOPE = "mcp:connect";

/** One server an agent may reach, as its grants record it. */
export type GrantedServer = {
  /**
   * Every grant row that reaches this server, so a removal can name all of
   * them. A policy can hold more than one — the same server granted twice, or
   * once per project — and removing only the first would leave the agent still
   * connected through the rest.
   */
  grantIds: string[];
  resourceId: string;
  projectId: string;
};

/**
 * The servers an agent's policy grants it, by the resource id the server
 * list uses. A wildcard grant is left out: it is not a server anyone picked,
 * and rewriting it as one would narrow the agent on the next save. Ask
 * `wildcardConnectGrant` whether one is there, because a selection that
 * silently omits it is a narrower list than the policy enforces.
 */
export function grantedServers(
  grants: AgentPolicyGrant[],
): Map<string, GrantedServer> {
  const servers = new Map<string, GrantedServer>();
  for (const grant of grants) {
    if (grant.scope !== CONNECT_SCOPE) continue;
    if (grant.selector.resourceKind !== "mcp") continue;
    if (grant.selector.resourceId === "*") continue;
    const seen = servers.get(grant.selector.resourceId);
    if (seen !== undefined) {
      seen.grantIds.push(grant.id);
      continue;
    }
    servers.set(grant.selector.resourceId, {
      grantIds: [grant.id],
      resourceId: grant.selector.resourceId,
      projectId: grant.selector.projectId ?? "",
    });
  }
  return servers;
}

/** A wildcard MCP grant, and how far its selector reaches. */
export type WildcardConnectGrant = {
  /** The project it is scoped to, or `""` where it reaches every project. */
  projectId: string;
};

/**
 * The wildcard MCP grant the policy holds, if it holds one.
 *
 * Such an agent has no server selection to show for the reach that grant
 * covers: the picker would come up empty, saving it would look like a
 * narrowing, and the wildcard would stay in place and keep granting
 * everything. The caller has to say so rather than present a list it cannot
 * enforce — and the project comes back with it, because a grant scoped to one
 * project reaches that project's servers, which is not every server the
 * organization has.
 */
export function wildcardConnectGrant(
  grants: AgentPolicyGrant[],
): WildcardConnectGrant | undefined {
  const wildcards = grants.filter(
    (grant) =>
      grant.scope === CONNECT_SCOPE &&
      grant.selector.resourceKind === "mcp" &&
      grant.selector.resourceId === "*",
  );
  if (wildcards.length === 0) return undefined;
  // An unscoped wildcard swallows every scoped one, so it is the reach to
  // report: naming one project beside it would understate what the agent has.
  if (wildcards.some((grant) => !grant.selector.projectId)) {
    return { projectId: "" };
  }
  return { projectId: wildcards[0]?.selector.projectId ?? "" };
}

/**
 * What saving an edited server list has to write: the servers added since it
 * was loaded, and the grants that have to go.
 *
 * Expressed as a diff rather than a replace because the policy is a set of
 * rows with their own ids: rewriting all of them would churn every grant's
 * audit trail on a change to one.
 */
export function grantChanges(
  granted: Map<string, GrantedServer>,
  chosen: { resourceId: string; projectId: string }[],
): {
  add: { resourceId: string; projectId: string }[];
  remove: string[];
} {
  const wanted = new Set(chosen.map((server) => server.resourceId));
  return {
    add: chosen.filter((server) => !granted.has(server.resourceId)),
    remove: [...granted.values()]
      .filter((server) => !wanted.has(server.resourceId))
      .flatMap((server) => server.grantIds),
  };
}
