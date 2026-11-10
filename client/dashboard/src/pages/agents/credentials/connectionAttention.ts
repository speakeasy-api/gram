import type { ServerConnection } from "./ownerConnections";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

/**
 * The connection each server shows on the MCP sign-in tab: the agent's
 * assigned one, unless it no longer works and the editor has a usable one,
 * which a save assigns in its place. Until the editor's own connection is
 * known, its checking or error state stands rather than a premature verdict.
 */
export function displayedConnections(
  connections: OwnerConnections,
): Map<string, ServerConnection> {
  const displayed = new Map(connections.byServer);
  for (const [serverId, assigned] of connections.assignedByServer ?? []) {
    const own = connections.byServer.get(serverId)?.status;
    const keepOwn =
      assigned.status !== "ready" &&
      assigned.status !== "not-required" &&
      (own === "ready" || own === "checking" || own === "error");
    if (!keepOwn) displayed.set(serverId, assigned);
  }
  return displayed;
}

/**
 * The servers someone has to sign in to before the agent can call them: one
 * that signs each caller in, with no connection, an expired one, or one that
 * could not be checked.
 */
export function serversNeedingConnection(
  servers: InventoryServer[],
  displayed: Map<string, ServerConnection>,
): InventoryServer[] {
  return servers.filter(
    (server) =>
      server.issuerId &&
      ["missing", "ended", "error"].includes(
        displayed.get(server.id)?.status ?? "checking",
      ),
  );
}
