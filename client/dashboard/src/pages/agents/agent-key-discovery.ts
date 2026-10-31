import type { Agents } from "@gram/client/sdk/agents.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { ANY_RESOURCE, delegableGrantKey } from "./agent-api-key-grants";
import {
  narrowGrantsToServers,
  type CredentialServerResource,
} from "./agent-key-server-grants";

/** Matches the server's `toolset_ids` MaxLength. */
export const DISCOVERY_BATCH_SIZE = 100;

/**
 * Scoped discovery for every inventory resource in as few GETs as possible;
 * never fall back to broad discovery. Each request locks the agent, so
 * batches run one at a time rather than contending for that lock.
 */
export async function discoverKeyServerGrants(
  agents: Pick<Agents, "listDelegableGrants">,
  agentId: string,
  inventory: CredentialServerResource[],
  signal: AbortSignal,
): Promise<AgentPolicyGrantForm[]> {
  const resources = [
    ...new Map(
      inventory
        .filter(
          (server) =>
            server.resourceId &&
            server.projectId &&
            server.kind !== "Unproxied",
        )
        .map((server) => [
          JSON.stringify([server.projectId, server.resourceId]),
          server,
        ]),
    ).values(),
  ];
  // Any failed batch throws, so no partial permission inventory is published.
  const results: AgentPolicyGrantForm[] = [];
  for (let i = 0; i < resources.length; i += DISCOVERY_BATCH_SIZE) {
    const batch = resources.slice(i, i + DISCOVERY_BATCH_SIZE);
    const grants = await agents.listDelegableGrants(
      { agentId, toolsetIds: batch.map((server) => server.resourceId) },
      undefined,
      { signal },
    );
    if (!Array.isArray(grants))
      throw new Error("Invalid delegable permission response");
    // Scoped candidates are pinned to one resource. Drop any wildcard so a
    // candidate returned for one server can never authorize another.
    const pinned = grants.filter(
      (grant) => grant.selector.resourceId !== ANY_RESOURCE,
    );
    results.push(...narrowGrantsToServers(pinned, batch));
  }
  return [
    ...new Map(
      results.map((grant) => [delegableGrantKey(grant), grant]),
    ).values(),
  ];
}

/**
 * What a key for "All servers" asks for: the agent's own `mcp:connect`
 * wildcard for each project in reach, plus every exclusion the owner, the
 * agent or the caller holds against it. The key then reaches servers added
 * after it is issued, where a list of today's servers never would.
 *
 * Empty when a project in reach has no such wildcard to delegate, for example
 * when the owner is granted servers one by one. The caller then falls back to
 * naming servers.
 */
export async function discoverWildcardKeyGrants(
  agents: Pick<Agents, "listDelegableGrants">,
  agentId: string,
  projectIds: string[],
  signal: AbortSignal,
): Promise<AgentPolicyGrantForm[]> {
  const grants = await agents.listDelegableGrants(
    { agentId, carryExclusions: true },
    undefined,
    { signal },
  );
  if (!Array.isArray(grants))
    throw new Error("Invalid delegable permission response");
  const wildcards = grants.filter(
    (grant) =>
      grant.effect === "allow" &&
      grant.scope === "mcp:connect" &&
      grant.selector.resourceKind === "mcp" &&
      grant.selector.resourceId === ANY_RESOURCE &&
      (projectIds.length === 0
        ? grant.selector.projectId === undefined
        : grant.selector.projectId !== undefined &&
          projectIds.includes(grant.selector.projectId)),
  );
  // Every project in reach needs its wildcard, and an empty reach (every
  // project) needs the organization-wide one. A partial set would issue a key
  // that silently misses the other projects, so name servers instead.
  const complete =
    projectIds.length === 0
      ? wildcards.length > 0
      : projectIds.every((projectId) =>
          wildcards.some((grant) => grant.selector.projectId === projectId),
        );
  if (!complete) return [];
  // Exclusions only narrow, so carrying one the kept wildcards do not overlap
  // costs nothing, and leaving one out would fail issuance.
  const exclusions = grants.filter(
    (grant) =>
      grant.effect === "allow" && grant.scope === "mcp:blocked_connect",
  );
  return [
    ...new Map(
      [...wildcards, ...exclusions].map((grant) => [
        delegableGrantKey(grant),
        grant,
      ]),
    ).values(),
  ];
}
