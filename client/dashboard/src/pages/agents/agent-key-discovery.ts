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
