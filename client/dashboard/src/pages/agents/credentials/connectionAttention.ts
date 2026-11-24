import { missingSignInNames, type ServerConnection } from "./ownerConnections";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

/**
 * The connection each server shows. For an agent that exists, only the one
 * assigned to it: the editor's own connection reaches the agent only when
 * they assign it (`assignableOwnConnection`), so showing it as the agent's
 * would claim a grant nobody made. While an agent is being created there is
 * nothing assigned yet, and the creator's connections are what creation
 * assigns.
 */
export function displayedConnections(
  connections: OwnerConnections,
): Map<string, ServerConnection> {
  const displayed = new Map(connections.byServer);
  for (const [serverId, assigned] of connections.assignedByServer ?? [])
    displayed.set(serverId, assigned);
  return displayed;
}

/**
 * Whether the editor could give an existing agent their own working
 * connection to a server, because the agent has none that works there.
 */
export function assignableOwnConnection(
  connections: OwnerConnections,
  serverId: string,
): boolean {
  const assigned = connections.assignedByServer?.get(serverId)?.status;
  if (assigned !== "missing" && assigned !== "ended") return false;
  return connections.byServer.get(serverId)?.status === "ready";
}

/**
 * The servers someone has to sign in to before the agent can call them: one
 * that signs each caller in, with no connection, an expired one, or one that
 * could not be checked. One whose connection the viewer may not see is not
 * counted: nothing on it is theirs to fix.
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

/**
 * What a re-read of a server's connection found, told once it lands. An
 * expired connection reads the same before and after, so without this the
 * refresh looks like it did nothing.
 */
export function refreshOutcome(
  serverName: string,
  connection: ServerConnection,
): { tone: "success" | "warning" | "info" | "error"; message: string } {
  if (connection.status === "ready")
    return {
      tone: "success",
      message: `The connection to ${serverName} is working.`,
    };
  if (connection.status === "ended")
    return {
      tone: "warning",
      message: `The connection to ${serverName} is still expired. Reauthenticate to sign in again.`,
    };
  if (connection.status === "missing")
    return {
      tone: "info",
      message: `No connection to ${serverName} yet. Connect an account to add one.`,
    };
  if (connection.status === "error")
    return {
      tone: "error",
      message: `Could not check the connection to ${serverName}. Try again.`,
    };
  return {
    tone: "info",
    message: `Checked the connection to ${serverName}.`,
  };
}
