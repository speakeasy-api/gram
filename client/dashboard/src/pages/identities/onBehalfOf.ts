import type { Key } from "@gram/client/models/components/key.js";
import type { McpServerAuthentication } from "@gram/client/models/components/mcpserverauthentication.js";
import { connectionLifetime } from "@/components/sessions/connection-lifetime";
import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";

/** A person whose own connection an agent signs in with on a server. */
export type OnBehalfOfPerson = {
  userId: string;
  name: string;
  /** One of this person's assigned sign-ins no longer works. */
  expired: boolean;
};

const USER_PREFIX = "user:";

/**
 * Whom an agent acts for on a server, from the connections assigned to it
 * there: the owners of people's own connections, once each. A server can ask
 * for several sign-ins, and each can belong to someone else, so every owner
 * is named rather than whoever came first. Whether a person's connection has
 * expired is read from their own sign-ins only: another person's lapsed one
 * says nothing about theirs.
 *
 * A shared credential, a server that needs no sign-in, and a connection not
 * yet read act for no one.
 *
 * Lives beside `GrantOnBehalfOf` rather than inside it so that file exports
 * only components and keeps fast refresh.
 */
export function onBehalfOfPeople(
  connection: ServerConnection | undefined,
  now: number = Date.now(),
): OnBehalfOfPerson[] {
  if (connection?.status !== "ready" && connection?.status !== "ended") {
    return [];
  }
  const people = new Map<string, OnBehalfOfPerson>();
  for (const session of connection.sessions) {
    if (!session.subjectUrn.startsWith(USER_PREFIX)) continue;
    const userId = session.subjectUrn.slice(USER_PREFIX.length);
    const ended = connectionLifetime(session, now).kind === "ended";
    const known = people.get(userId);
    if (known) {
      known.expired ||= ended;
      continue;
    }
    people.set(userId, {
      userId,
      name: session.subjectDisplayName || session.subjectEmail || userId,
      expired: ended,
    });
  }
  return [...people.values()];
}

/**
 * How an agent signs in to one MCP server, as its Permissions tab tells it:
 * with named people's own connections, or with a shared credential that acts
 * for no one.
 */
export type ServerOnBehalfOf =
  /** The server or its connection is still being read. */
  | { kind: "checking" }
  /** No person's connection: a shared credential, or no sign-in at all. */
  | { kind: "shared" }
  | { kind: "people"; people: OnBehalfOfPerson[] }
  /** The server asks for a person's connection and none is assigned. */
  | { kind: "missing" }
  | { kind: "error" }
  /** No server the dashboard can list goes by this id. */
  | { kind: "unknown" };

/** Auth client types an agent key can be set up to use: shared ones the agent gateway can send. */
const AGENT_KEY_SHARED_CLIENT_TYPES = new Set([
  "service_account",
  "static_credential",
]);

/**
 * Whether one of an agent's live keys was set up to use a shared credential
 * on a server, which the MCP runtime then signs the agent in with instead of
 * anyone's connection. The runtime honors a key's choice only under upstream
 * auth enforcement, and refuses one whose credential the server no longer
 * offers enabled, so neither counts.
 */
export function keysUseSharedCredential(
  keys: Pick<Key, "expiresAt" | "authClientChoices">[],
  serverId: string,
  auth: Pick<McpServerAuthentication, "clients">,
  now: number = Date.now(),
): boolean {
  const offered = new Set(
    auth.clients
      .filter(
        (client) =>
          client.enabled &&
          AGENT_KEY_SHARED_CLIENT_TYPES.has(client.clientType),
      )
      .map((client) => client.id),
  );
  return keys.some(
    (key) =>
      (!key.expiresAt || key.expiresAt.getTime() > now) &&
      (key.authClientChoices ?? []).some(
        (choice) =>
          choice.mcpServerId === serverId &&
          !!choice.authClientId &&
          offered.has(choice.authClientId),
      ),
  );
}

/**
 * Whom an agent acts for on a server, from the server's listing, whether the
 * agent's keys use a shared credential chosen for it, and the connection
 * assigned to the agent there. A server with no user session issuer asks no
 * one to sign in, so whatever credential it has is shared.
 */
export function serverOnBehalfOf(
  server: { issuerId?: string } | undefined,
  connection: ServerConnection | undefined,
  inventoryLoading: boolean,
  now: number = Date.now(),
  /** Whether the agent's keys use a shared credential chosen for the server, or "checking" while that is read. */
  keysShared: boolean | "checking" = false,
): ServerOnBehalfOf {
  if (!server) {
    return inventoryLoading ? { kind: "checking" } : { kind: "unknown" };
  }
  if (keysShared === true) return { kind: "shared" };
  if (keysShared === "checking" && server.issuerId) {
    return { kind: "checking" };
  }
  if (!connection) {
    return server.issuerId ? { kind: "checking" } : { kind: "shared" };
  }
  if (connection.status === "checking") return { kind: "checking" };
  if (connection.status === "error") return { kind: "error" };
  if (connection.status === "missing") return { kind: "missing" };
  const people = onBehalfOfPeople(connection, now);
  if (people.length === 0) return { kind: "shared" };
  return { kind: "people", people };
}
