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

/**
 * Whom an agent acts for on a server, from the server's listing and the
 * connection assigned to the agent there. A server with no user session
 * issuer asks no one to sign in, so whatever credential it has is shared.
 */
export function serverOnBehalfOf(
  server: { issuerId?: string } | undefined,
  connection: ServerConnection | undefined,
  inventoryLoading: boolean,
  now: number = Date.now(),
): ServerOnBehalfOf {
  if (!server) {
    return inventoryLoading ? { kind: "checking" } : { kind: "unknown" };
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
