import type { Agents } from "@gram/client/sdk/agents.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { delegableGrantKey } from "./agent-api-key-grants";
import {
  narrowGrantsToServers,
  type CredentialServerResource,
} from "./agent-key-server-grants";

/** One scoped GET per inventory resource; never fall back to broad discovery. */
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
  // Promise.all publishes no partial permission inventory if any lookup fails.
  const results = await Promise.all(
    resources.map(async (server) => {
      const grants = await agents.listDelegableGrants(
        { agentId, toolsetId: server.resourceId },
        undefined,
        { signal },
      );
      if (!Array.isArray(grants))
        throw new Error("Invalid delegable permission response");
      // A candidate returned for one lookup must not authorize another server.
      return narrowGrantsToServers(grants, [server]);
    }),
  );
  return [
    ...new Map(
      results.flat().map((grant) => [delegableGrantKey(grant), grant]),
    ).values(),
  ];
}
