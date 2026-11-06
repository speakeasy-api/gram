import { useOwnerConnections } from "@/pages/agents/credentials/useOwnerConnections";
import { useServerInventory } from "@/pages/agents/credentials/useServerInventory";
import { serverOnBehalfOf, type ServerOnBehalfOf } from "./onBehalfOf";

/**
 * Whom an agent acts for on one MCP server: the people whose own connections
 * are assigned to it there, or no one when a shared credential signs it in.
 * The Permissions tab's MCP servers section and the Overview's "Acting on
 * behalf of" both read it, so they cannot disagree.
 */
export function useServerOnBehalfOf(
  agentId: string,
  /** The server id a grant's selector names, or a server id from a call. */
  resourceId: string | undefined,
): ServerOnBehalfOf {
  const inventory = useServerInventory({ lookupOnly: true });
  const server = resourceId
    ? inventory.servers.find(
        (candidate) =>
          candidate.resourceId === resourceId || candidate.id === resourceId,
      )
    : undefined;
  const connections = useOwnerConnections(
    server ? [server] : [],
    !!server,
    agentId,
  );
  if (!resourceId) return { kind: "unknown" };
  // A failed server list says nothing about this server: it could not be
  // checked, which is not the same as no server going by this id.
  if (!server && inventory.isError) return { kind: "error" };
  return serverOnBehalfOf(
    server,
    server ? connections.assignedByServer?.get(server.id) : undefined,
    inventory.isLoading,
  );
}
