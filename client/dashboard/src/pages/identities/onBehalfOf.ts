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
