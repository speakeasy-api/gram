import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";

import {
  grantedServers,
  reachCoversProject,
  wildcardConnectGrant,
} from "@/pages/agents/agentPolicyEdit";
import {
  displayedConnections,
  serversNeedingConnection,
} from "@/pages/agents/credentials/connectionAttention";
import { useOwnerConnections } from "@/pages/agents/credentials/useOwnerConnections";
import { useServerInventory } from "@/pages/agents/credentials/useServerInventory";

/**
 * How many of the agent's MCP servers someone has to sign in to, counted as
 * its MCP sign-in tab counts them, for the badge that draws people there.
 * Only someone who can change the agent can read its servers, and fix them,
 * so for anyone else it is 0.
 */
export function useAgentMcpSignInAttention(agent: ManagedAgent): number {
  const canWrite = agent.permissions.write;
  const grants = useAgentsListPolicyGrants({ agentId: agent.id }, undefined, {
    throwOnError: false,
    enabled: canWrite,
  });
  // Only what finding a server needs: no activity counts or upstream URLs.
  const inventory = useServerInventory({ lookupOnly: true });
  const wildcard =
    grants.data === undefined ? undefined : wildcardConnectGrant(grants.data);
  const granted =
    grants.data === undefined ? new Map() : grantedServers(grants.data);
  const servers = inventory.servers.filter((server) =>
    wildcard
      ? reachCoversProject(wildcard.projectIds, server.projectId)
      : granted.has(server.resourceId),
  );
  const connections = useOwnerConnections(
    servers,
    canWrite && grants.data !== undefined,
    agent.id,
  );
  if (!canWrite || grants.data === undefined) return 0;
  return serversNeedingConnection(servers, displayedConnections(connections))
    .length;
}
