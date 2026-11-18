import { missingSignInNames, type ServerConnection } from "./ownerConnections";
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

const andList = new Intl.ListFormat("en", {
  style: "long",
  type: "conjunction",
});

/**
 * What a server still needs before it is ready, in words: "This server has 2
 * Linear sign-ins; 1 is connected." Null when there
 * is nothing to add to the row's own "Requires connection": no sign-in is
 * known to be missing, or nothing is connected yet and the one missing is the
 * server's own, which the row already names.
 *
 * Without this the row read "Requires connection" right after a successful
 * sign-in, when the server also needed a sign-in to another service on its
 * authorization server, and nothing on the page said which.
 */
export function missingSignInsSentence(
  server: Pick<InventoryServer, "name">,
  connection: ServerConnection | undefined,
): string | null {
  if (connection?.status !== "missing" || !connection.signIns?.length)
    return null;
  const { names, unnamed } = missingSignInNames(connection);
  const missing = connection.signIns.length;
  const connected = connection.connected ?? 0;
  // A sign-in that just succeeded is the case that needs explaining most,
  // even when what is left shares the server's name.
  if (
    connected === 0 &&
    unnamed === 0 &&
    names.length === 1 &&
    names[0] === server.name
  )
    return null;
  const total = connected + missing;
  const verb = connected === 1 ? "is" : "are";
  // Every sign-in, connected or left, shares the server's name, as when one
  // service is registered twice: say so plainly rather than naming the server
  // back to itself. Connected sign-ins of another name get the general wording.
  if (
    connected > 0 &&
    unnamed === 0 &&
    names.length === 1 &&
    names[0] === server.name &&
    connection.connectedNames?.length === connected &&
    connection.connectedNames.every((name) => name === server.name)
  )
    return `This server has ${total} ${server.name} sign-ins; ${connected} ${verb} connected.`;
  const progress =
    connected > 0
      ? `This server has ${total} sign-ins; ${connected} ${verb} connected. `
      : "";
  if (names.length === 0)
    return `${progress}${
      unnamed === 1
        ? "Still needs one more sign-in."
        : `Still needs ${unnamed} more sign-ins.`
    }`;
  const parts =
    unnamed === 0
      ? names
      : [
          ...names,
          unnamed === 1 ? "1 other sign-in" : `${unnamed} other sign-ins`,
        ];
  return `${progress}Still needs a connection to ${andList.format(parts)}.`;
}
